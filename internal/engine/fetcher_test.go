package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Estetika101/cairn/internal/model"
)

func testCfg() model.CrawlConfig {
	return model.CrawlConfig{
		RequestTimeoutMs:      5000,
		UserAgent:             "cairn/0.1 (+https://github.com/Estetika101/cairn)",
		MaxRetries:            0,
		RetryAfterCapMs:       1000,
		MaxConcurrentRequests: 8,
		MaxExtraFetches:       500,
		SiteConcurrency:       1,
		RespectRobots:         false,
		PerHost:               model.PerHostConfig{Concurrency: 2, DelayMs: 0},
	}
}

// Row 8: robots.txt disallows /private -> Fetch returns ErrRobotsDisallowed
// without a content network hit.
func TestFetch_RobotsDisallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	cfg := testCfg()
	cfg.RespectRobots = true
	f := NewFetcher(cfg)

	_, err := f.Fetch(context.Background(), srv.URL+"/private", ExtraFetch)
	if !errors.Is(err, model.ErrRobotsDisallowed) {
		t.Fatalf("err = %v, want ErrRobotsDisallowed", err)
	}
	for _, u := range f.RequestLog() {
		if u == srv.URL+"/private" {
			t.Errorf("disallowed URL was fetched over the network: %s", u)
		}
	}
}

// Row 9: maxExtraFetches:1 with three distinct ExtraFetch targets -> 1 checked,
// 2 skipped with ErrBudgetExhausted.
func TestFetch_BudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	cfg := testCfg()
	cfg.MaxExtraFetches = 1
	f := NewFetcher(cfg)

	var ok, exhausted int
	for _, p := range []string{"/x1", "/x2", "/x3"} {
		_, err := f.Fetch(context.Background(), srv.URL+p, ExtraFetch)
		switch {
		case err == nil:
			ok++
		case errors.Is(err, model.ErrBudgetExhausted):
			exhausted++
		default:
			t.Fatalf("unexpected err for %s: %v", p, err)
		}
	}
	if ok != 1 || exhausted != 2 {
		t.Fatalf("ok=%d exhausted=%d, want 1 and 2", ok, exhausted)
	}
}

// Row 10: two Fetches of the same URL -> second served from cache; one network
// hit; budget decremented at most once.
func TestFetch_CacheDedup(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	f := NewFetcher(testCfg())
	target := srv.URL + "/page"

	pd1, err1 := f.Fetch(context.Background(), target, ExtraFetch)
	pd2, err2 := f.Fetch(context.Background(), target, ExtraFetch)
	if err1 != nil || err2 != nil {
		t.Fatalf("errs: %v %v", err1, err2)
	}
	if pd1.FromCache {
		t.Errorf("first fetch should not be from cache")
	}
	if !pd2.FromCache {
		t.Errorf("second fetch should be from cache")
	}
	mu.Lock()
	gotHits := hits
	mu.Unlock()
	if gotHits != 1 {
		t.Errorf("server hits = %d, want 1", gotHits)
	}
	var logged int
	for _, u := range f.RequestLog() {
		if u == target {
			logged++
		}
	}
	if logged != 1 {
		t.Errorf("request log has target %d times, want 1", logged)
	}
}

// Row 11: 403 + Cloudflare challenge markers -> ErrBlocked, not a normal 4xx.
func TestFetch_BlockedChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<html><body>Attention Required! | Cloudflare</body></html>")
	}))
	defer srv.Close()

	f := NewFetcher(testCfg())
	pd, err := f.Fetch(context.Background(), srv.URL+"/cf", ExtraFetch)
	if !errors.Is(err, model.ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if pd.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (preserved on the PageData)", pd.Status)
	}
}

// Row 14: with delayMs:250, three same-host requests start >= ~250ms apart even
// when issued concurrently.
func TestFetch_PerHostDelay(t *testing.T) {
	var mu sync.Mutex
	var stamps []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stamps = append(stamps, time.Now())
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	cfg := testCfg()
	cfg.PerHost = model.PerHostConfig{Concurrency: 1, DelayMs: 250}
	f := NewFetcher(cfg)

	var wg sync.WaitGroup
	for _, p := range []string{"/d1", "/d2", "/d3"} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			f.Fetch(context.Background(), srv.URL+path, ExtraFetch)
		}(p)
	}
	wg.Wait()

	if len(stamps) != 3 {
		t.Fatalf("got %d requests, want 3", len(stamps))
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].Before(stamps[j]) })
	for i := 1; i < len(stamps); i++ {
		gap := stamps[i].Sub(stamps[i-1])
		if gap < 240*time.Millisecond {
			t.Errorf("gap %d = %v, want >= 250ms", i, gap)
		}
	}
}
