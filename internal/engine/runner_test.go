package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Estetika101/cairn/internal/checks/security"
	"github.com/Estetika101/cairn/internal/engine"
	"github.com/Estetika101/cairn/internal/model"
)

func runnerCfg() model.CrawlConfig {
	return model.CrawlConfig{
		RequestTimeoutMs:      5000,
		UserAgent:             "cairn/0.1 (+test)",
		MaxRetries:            0,
		MaxConcurrentRequests: 8,
		MaxExtraFetches:       500,
		SiteConcurrency:       1,
		RespectRobots:         false,
		PerHost:               model.PerHostConfig{Concurrency: 2, DelayMs: 0},
	}
}

// End-to-end wiring: crawl -> page check -> findings with stable IDs. Uses a
// plain-HTTP fixture, so HSTS is not applicable and the four non-HSTS header
// criteria are evaluated.
func TestRunSite_PagePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		// nosniff, X-Frame-Options, Referrer-Policy intentionally absent.
		w.Write([]byte(`<html><head><title>Home</title></head><body>hi</body></html>`))
	}))
	defer srv.Close()

	sr, err := engine.RunSite(context.Background(), runnerCfg(),
		engine.SiteTarget{Name: "Fixture", URL: srv.URL, CrawlLimit: 0},
		[]model.Check{security.New()}, model.CheckConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(sr.Findings) != 4 {
		t.Fatalf("got %d findings, want 4 (HSTS N/A on http)", len(sr.Findings))
	}
	ids := map[string]bool{}
	for _, f := range sr.Findings {
		if len(f.ID) != 10 {
			t.Errorf("finding %q has ID %q, want a 10-char content hash", f.Criterion, f.ID)
		}
		if ids[f.ID] {
			t.Errorf("duplicate finding ID %q — uniqueness invariant broken", f.ID)
		}
		ids[f.ID] = true
	}
	if sr.Summary.Pass != 1 || sr.Summary.Fail != 3 {
		t.Errorf("summary = %+v, want 1 pass / 3 fail", sr.Summary)
	}
}
