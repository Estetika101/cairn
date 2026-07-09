// Package engine is cairn's fetch/crawl core. The Fetcher is the single network
// path shared by the crawl and by every check's Fetch: politeness, robots, the
// per-run cache, and the fetch budget are enforced here, once, for every caller
// (v0.4 §3b/§7c). No check — built-in or plugin — opens its own socket.
package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// FetchKind separates the two budgets so a big crawl can't starve link-checking.
type FetchKind int

const (
	CrawlFetch FetchKind = iota // budget-exempt; bounded by crawlLimit (page count)
	ExtraFetch                  // check-initiated; bounded by maxExtraFetches
)

const maxRedirectHops = 10

type cacheEntry struct {
	pd  model.PageData
	err error
}

type flight struct {
	done chan struct{}
	pd   model.PageData
	err  error
}

// Fetcher is created once per site run.
type Fetcher struct {
	cfg     model.CrawlConfig
	client  *http.Client
	lim     *limiter
	uaToken string

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]*flight
	robots   map[string]*robotsRules // per-host, nil-safe once resolved
	budget   int

	netReqs atomic.Int64
	logMu   sync.Mutex
	log     []string // canonical URLs actually fetched over the network (content only)
}

type ctxKey int

const chainKey ctxKey = 0

// NewFetcher builds a per-run Fetcher from crawl config.
func NewFetcher(cfg model.CrawlConfig) *Fetcher {
	f := &Fetcher{
		cfg:      cfg,
		lim:      newLimiter(cfg.MaxConcurrentRequests, cfg.PerHost.Concurrency, cfg.PerHost.DelayMs),
		uaToken:  uaToken(cfg.UserAgent),
		cache:    map[string]cacheEntry{},
		inflight: map[string]*flight{},
		robots:   map[string]*robotsRules{},
		budget:   cfg.MaxExtraFetches,
	}
	f.client = &http.Client{
		Timeout: time.Duration(cfg.RequestTimeoutMs) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if c, ok := req.Context().Value(chainKey).(*[]string); ok {
				*c = append(*c, req.URL.String())
			}
			if len(via) >= maxRedirectHops {
				return errors.New("stopped after too many redirects")
			}
			return nil
		},
	}
	return f
}

// NetworkRequests reports how many transport requests the engine has made
// (content + robots.txt). Used by tests to prove the cache and sandbox.
func (f *Fetcher) NetworkRequests() int64 { return f.netReqs.Load() }

// RequestLog returns the canonical URLs actually fetched over the network,
// excluding robots.txt. A cache hit does not appear.
func (f *Fetcher) RequestLog() []string {
	f.logMu.Lock()
	defer f.logMu.Unlock()
	out := make([]string, len(f.log))
	copy(out, f.log)
	return out
}

// Fetch is the only network path a caller gets. It applies, in order: per-run
// cache, robots gate, budget gate (ExtraFetch only), politeness, transport.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, kind FetchKind) (model.PageData, error) {
	canon, u, err := canonicalize(rawURL)
	if err != nil {
		return model.PageData{}, err
	}

	// 1. Per-run cache (memo). A hit consumes no budget and makes no network call.
	f.mu.Lock()
	if e, ok := f.cache[canon]; ok {
		f.mu.Unlock()
		return withFromCache(e.pd), e.err
	}
	// Single-flight: a concurrent identical fetch is deduped, not double-counted.
	if fl, ok := f.inflight[canon]; ok {
		f.mu.Unlock()
		<-fl.done
		return withFromCache(fl.pd), fl.err
	}
	fl := &flight{done: make(chan struct{})}
	f.inflight[canon] = fl
	f.mu.Unlock()

	pd, ferr := f.fetchUncached(ctx, u, canon, kind)

	f.mu.Lock()
	// ErrBudgetExhausted is a run-state error, not a property of the URL, so it
	// is never memoized — a later URL must be free to try while budget remains
	// only for earlier ones. Everything else (success, robots, blocked, 4xx/5xx,
	// transport) is deterministic for the run and is cached.
	if !errors.Is(ferr, model.ErrBudgetExhausted) {
		f.cache[canon] = cacheEntry{pd: pd, err: ferr}
	}
	delete(f.inflight, canon)
	f.mu.Unlock()

	fl.pd, fl.err = pd, ferr
	close(fl.done)
	return pd, ferr
}

func (f *Fetcher) fetchUncached(ctx context.Context, u *url.URL, canon string, kind FetchKind) (model.PageData, error) {
	// 2. robots.txt gate.
	if f.cfg.RespectRobots {
		rules := f.ensureRobots(ctx, u)
		if !rules.allowed(u.EscapedPath()) {
			return model.PageData{}, model.ErrRobotsDisallowed
		}
	}

	// 3. Budget gate (ExtraFetch only). Reserve atomically before the network.
	if kind == ExtraFetch {
		f.mu.Lock()
		if f.budget <= 0 {
			f.mu.Unlock()
			return model.PageData{}, model.ErrBudgetExhausted
		}
		f.budget--
		f.mu.Unlock()
	}

	// 4 + 5. Politeness + transport.
	return f.rawGet(ctx, u.String(), true)
}

// ensureRobots fetches and parses /robots.txt for a host once per run. The
// robots fetch is an internal engine fetch: it skips the robots gate (a
// robots.txt request can't be gated on robots.txt) and consumes no budget
// (v0.4 §3b). On any error it fails open (allow) for the slice.
func (f *Fetcher) ensureRobots(ctx context.Context, u *url.URL) *robotsRules {
	host := strings.ToLower(u.Host)
	f.mu.Lock()
	if r, ok := f.robots[host]; ok {
		f.mu.Unlock()
		return r
	}
	f.mu.Unlock()

	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	var rules *robotsRules
	pd, err := f.rawGet(ctx, robotsURL, false)
	if err != nil || pd.Status < 200 || pd.Status >= 300 {
		rules = &robotsRules{} // fail open
	} else {
		rules = parseRobots(pd.Body, f.uaToken)
	}

	f.mu.Lock()
	f.robots[host] = rules
	f.mu.Unlock()
	return rules
}

// rawGet performs politeness + transport with retry, redirect capture, and
// challenge detection. content=false suppresses request-log entries (robots).
func (f *Fetcher) rawGet(ctx context.Context, rawURL string, content bool) (model.PageData, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return model.PageData{}, err
	}
	host := strings.ToLower(u.Host)

	var (
		resp  *http.Response
		body  []byte
		chain []string
		start time.Time
	)
	attempts := f.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		release := f.lim.acquire(host)
		chain = nil
		reqCtx := context.WithValue(ctx, chainKey, &chain)
		req, rerr := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
		if rerr != nil {
			release()
			return model.PageData{}, rerr
		}
		req.Header.Set("User-Agent", f.cfg.UserAgent)

		start = time.Now()
		f.netReqs.Add(1)
		if content {
			f.logMu.Lock()
			if canon, _, cerr := canonicalize(rawURL); cerr == nil {
				f.log = append(f.log, canon)
			}
			f.logMu.Unlock()
		}
		r, gerr := f.client.Do(req)
		if gerr != nil {
			release()
			return model.PageData{}, gerr
		}
		b, rerr2 := io.ReadAll(r.Body)
		r.Body.Close()
		release()
		if rerr2 != nil {
			return model.PageData{}, rerr2
		}

		// Retry on 429/503 honoring Retry-After (capped), up to maxRetries.
		if (r.StatusCode == http.StatusTooManyRequests || r.StatusCode == http.StatusServiceUnavailable) && attempt < attempts-1 {
			wait := f.retryAfter(r.Header.Get("Retry-After"))
			time.Sleep(wait)
			continue
		}
		resp, body = r, b
		break
	}

	pd := model.PageData{
		RequestedURL:  rawURL,
		FinalURL:      resp.Request.URL.String(),
		Status:        resp.StatusCode,
		Headers:       resp.Header,
		Body:          body,
		RedirectChain: chain,
		TimingMs:      time.Since(start).Milliseconds(),
		FetchedAt:     time.Now(),
	}

	if isChallenge(resp, body) {
		return pd, model.ErrBlocked
	}

	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		if doc, derr := goquery.NewDocumentFromReader(bytes.NewReader(body)); derr == nil {
			pd.Doc = doc
		}
	}
	return pd, nil
}

// retryAfter parses a Retry-After delta-seconds value and clamps it to the cap.
func (f *Fetcher) retryAfter(v string) time.Duration {
	capDur := time.Duration(f.cfg.RetryAfterCapMs) * time.Millisecond
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	d := time.Duration(secs) * time.Second
	if d > capDur {
		return capDur
	}
	return d
}

// isChallenge detects a bot-protection / WAF interstitial so it is reported as
// blocked rather than mislabeled a normal 4xx (v0.4 §5c).
func isChallenge(resp *http.Response, body []byte) bool {
	if resp.Header.Get("cf-mitigated") != "" {
		return true
	}
	server := strings.ToLower(resp.Header.Get("Server"))
	blob := strings.ToLower(string(body))
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusServiceUnavailable:
		if strings.Contains(server, "cloudflare") &&
			(strings.Contains(blob, "attention required") ||
				strings.Contains(blob, "just a moment") ||
				strings.Contains(blob, "cf-challenge") ||
				strings.Contains(blob, "__cf_chl")) {
			return true
		}
		if strings.Contains(blob, "access denied") && strings.Contains(server, "akamai") {
			return true
		}
		if strings.Contains(blob, "hcaptcha.com/captcha") {
			return true
		}
	}
	return false
}

// canonicalize is the fetch-cache key: lowercase scheme+host, drop default
// ports, drop the fragment, KEEP the query. This is deliberately distinct from
// the finding-ID normalize (which keeps the fragment) — v0.4 §6b.
func canonicalize(rawURL string) (string, *url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}
	u.Fragment = ""
	key := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key, u, nil
}

func withFromCache(pd model.PageData) model.PageData {
	pd.FromCache = true
	return pd
}
