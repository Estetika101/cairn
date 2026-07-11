package demo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Estetika101/verdict/internal/checks/geo"
	"github.com/Estetika101/verdict/internal/checks/security"
	"github.com/Estetika101/verdict/internal/checks/seo"
	"github.com/Estetika101/verdict/internal/engine"
	"github.com/Estetika101/verdict/internal/model"
)

//go:embed assets/*
var assetsFS embed.FS

const (
	scansPerHour = 5
	scanTimeout  = 15 * time.Second
)

// Options configures a Server. Store, TurnstileSiteKey, and
// TurnstileSecretKey are all optional together: leave them zero to run
// without logging and without human verification (e.g. local dev). Turnstile
// is on only when BOTH the site key (public, sent to the browser) and the
// secret key (private, used server-side) are set — a site key alone would
// render a widget nothing actually checks.
type Options struct {
	Store              *Store
	TurnstileSiteKey   string
	TurnstileSecretKey string
}

// Server is the public demo endpoint: one visitor-submitted URL in, one
// hardened fetch, a fixed set of page-scoped checks, a logged result out.
type Server struct {
	limiter   *rateLimiter
	store     *Store // nil disables logging (e.g. local dev with no DATABASE_URL)
	checks    []model.Check
	mux       *http.ServeMux
	siteKey   string
	secretKey string // "" disables Turnstile verification entirely
}

// NewServer builds the demo server.
func NewServer(opts Options) (*Server, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("demo: %w", err)
	}

	// Only page-scoped checks run here — the demo has no Corpus and Fetch is
	// deliberately unavailable (demoCheckContext.Fetch always errors), so any
	// site-scoped check (broken-links, sitemap validity, hreflang
	// reciprocity, bot posture, llms.txt) is filtered out automatically by
	// scope rather than hand-maintained as an ID allowlist — it stays correct
	// as new checks are added upstream without anyone remembering to update
	// a list here.
	var pageChecks []model.Check
	for _, c := range allCandidateChecks() {
		if c.Meta().Scope == model.ScopePage {
			pageChecks = append(pageChecks, c)
		}
	}

	s := &Server{
		limiter: newRateLimiter(scansPerHour, time.Hour),
		store:   opts.Store,
		checks:  pageChecks,
	}
	// Both keys required, or neither counts — see the Options doc comment.
	if opts.TurnstileSiteKey != "" && opts.TurnstileSecretKey != "" {
		s.siteKey = opts.TurnstileSiteKey
		s.secretKey = opts.TurnstileSecretKey
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/scan", s.handleScan)
	mux.HandleFunc("/api/turnstile-sitekey", s.handleSiteKey)
	s.mux = mux

	go s.sweepLoop()
	return s, nil
}

// handleSiteKey exposes the PUBLIC site key only — never the secret — so the
// frontend can render the Turnstile widget dynamically. Returns an empty
// string when Turnstile isn't configured, and the frontend treats that as
// "skip the widget entirely," matching the backend's own skip-when-unset
// behavior in handleScan.
func (s *Server) handleSiteKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"sitekey": s.siteKey})
}

func allCandidateChecks() []model.Check {
	all := []model.Check{security.New()}
	all = append(all, seo.All()...)
	all = append(all, geo.All()...)
	return all
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex") // the scan tool itself isn't a page worth indexing
	s.mux.ServeHTTP(w, r)
}

func (s *Server) sweepLoop() {
	t := time.NewTicker(5 * time.Minute)
	for range t.C {
		s.limiter.sweep()
	}
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := clientIP(r)
	if !s.limiter.Allow(ip) {
		http.Error(w, fmt.Sprintf("rate limit exceeded — max %d scans/hour, try again later", scansPerHour), http.StatusTooManyRequests)
		return
	}

	var body struct {
		URL            string `json:"url"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	target, ok := normalizeSubmittedURL(body.URL)
	if !ok {
		http.Error(w, "please provide a valid http(s) URL to scan", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), scanTimeout)
	defer cancel()

	// Turnstile verification only runs when a secret key is configured
	// (see Options' doc comment) — local/dev runs without one work exactly
	// as before. A verification failure is reported to the visitor as
	// exactly that, not folded into the generic scan-failure path.
	if s.secretKey != "" {
		human, verr := verifyTurnstile(ctx, s.secretKey, body.TurnstileToken, ip)
		if verr != nil {
			http.Error(w, "verification check failed — please try again", http.StatusBadGateway)
			return
		}
		if !human {
			http.Error(w, "please complete the human verification and try again", http.StatusForbidden)
			return
		}
	}

	pd, err := safeFetch(ctx, target)
	if err != nil {
		s.logAsync(ScanLogEntry{Hostname: hostnameOf(target), RemoteIP: ip, Err: err.Error()})
		http.Error(w, "could not scan that URL: "+userFacingError(err), http.StatusBadRequest)
		return
	}

	cc := &demoCheckContext{page: pd}
	var findings []model.Finding
	for _, c := range s.checks {
		fs, cerr := c.Run(cc)
		if cerr != nil {
			continue
		}
		findings = append(findings, fs...)
	}
	engine.AssignIDs(findings)
	engine.SortFindings(findings)
	summary := model.Summarize(findings)

	s.logAsync(ScanLogEntry{
		Hostname: hostnameOf(target), RemoteIP: ip,
		ErrorCount: countBySeverity(findings, model.SeverityError),
		WarnCount:  countBySeverity(findings, model.SeverityWarn),
		PassCount:  summary.Pass, SkipCount: summary.Skipped, InfoCount: summary.Info,
	})

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"url":      pd.FinalURL,
		"summary":  summary,
		"findings": findings,
	})
}

// logAsync never blocks or fails the actual scan response on a logging
// hiccup — the log is for operator visibility, not something a visitor's
// request should ever depend on.
func (s *Server) logAsync(e ScanLogEntry) {
	if s.store == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.LogScan(ctx, e)
	}()
}

// demoCheckContext wraps exactly one already-fetched page. Fetch is
// deliberately unavailable — no check running in the public demo gets a
// second network call, which is what keeps the whole endpoint down to one
// hardened fetch path.
type demoCheckContext struct{ page model.PageData }

func (c *demoCheckContext) Scope() model.Scope       { return model.ScopePage }
func (c *demoCheckContext) Page() model.PageData     { return c.page }
func (c *demoCheckContext) Corpus() []model.PageData { return nil }
func (c *demoCheckContext) Config() model.CheckConfig {
	return model.CheckConfig{WCAGVersion: "2.2", WCAGLevel: "AA"}
}
func (c *demoCheckContext) Logf(string, ...any) {}
func (c *demoCheckContext) Fetch(context.Context, string) (model.PageData, error) {
	return model.PageData{}, errors.New("fetch is not available in the public demo — only the submitted page itself is checked")
}

// clientIP prefers Fly.io's client-IP header (set by their edge proxy, so
// RemoteAddr would otherwise just be the internal proxy hop) and falls back
// to RemoteAddr for local/direct runs. RemoteAddr is host:port — the port is
// a fresh ephemeral value on every connection, so it MUST be stripped or the
// rate limiter silently never sees a repeat key (caught live: curling the
// same "attacker" six times in a row from one machine all landed as distinct
// keys and every request sailed through).
func clientIP(r *http.Request) string {
	if v := r.Header.Get("Fly-Client-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// normalizeSubmittedURL applies the one UX nicety a "paste a URL" box needs:
// default to https:// when the visitor typed a bare domain.
func normalizeSubmittedURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return u.String(), true
}

func countBySeverity(findings []model.Finding, sev model.Severity) int {
	n := 0
	for _, f := range findings {
		if f.Status == model.StatusFail && f.Severity == sev {
			n++
		}
	}
	return n
}

func hostnameOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}

// userFacingError avoids leaking raw internal error text (which can include
// resolved IPs, internal timeouts, etc.) to an anonymous caller — map the
// known safety-guard errors to a clear message, generic-ify the rest.
func userFacingError(err error) string {
	switch {
	case errors.Is(err, ErrPrivateIP):
		return "that address isn't a public site Verdict can reach"
	case errors.Is(err, ErrBadScheme):
		return "only http and https URLs are supported"
	case errors.Is(err, ErrTooManyRedirects):
		return "too many redirects"
	default:
		return "the request timed out or the site couldn't be reached"
	}
}
