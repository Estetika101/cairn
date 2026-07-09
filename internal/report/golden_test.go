package report_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Estetika101/cairn/internal/checks/links"
	"github.com/Estetika101/cairn/internal/checks/security"
	"github.com/Estetika101/cairn/internal/engine"
	"github.com/Estetika101/cairn/internal/model"
	"github.com/Estetika101/cairn/internal/report"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the golden files")

// canonHost is the fixed origin the volatile httptest URL is rewritten to, so
// the report — including content-hash IDs — is byte-for-byte reproducible.
const canonHost = "http://fixture.test"

func goldenCfg() model.CrawlConfig {
	return model.CrawlConfig{
		RequestTimeoutMs:      5000,
		UserAgent:             "cairn/0.1 (+test)",
		MaxConcurrentRequests: 8,
		MaxExtraFetches:       500,
		SiteConcurrency:       1,
		RespectRobots:         false,
		PerHost:               model.PerHostConfig{Concurrency: 4, DelayMs: 0},
	}
}

// Row 12: report.json and cairn-tasks.md byte-match the goldens, with stable IDs
// and a fixed generatedAt.
func TestGoldenReport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			// CSP and X-Frame-Options intentionally absent.
			fmt.Fprint(w, `<html><head><title>Home</title></head><body>`+
				`<a href="/a">a</a><a href="/dead">dead</a></body></html>`)
		case "/a":
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			fmt.Fprint(w, `<html><head><title>A</title></head><body><a href="/dead">dead</a></body></html>`)
		case "/dead":
			w.WriteHeader(http.StatusNotFound)
		default:
			fmt.Fprint(w, "<html><body>x</body></html>")
		}
	}))
	defer srv.Close()

	sr, err := engine.RunSite(context.Background(), goldenCfg(),
		engine.SiteTarget{Name: "Fixture", URL: srv.URL, CrawlLimit: 2},
		[]model.Check{security.New(), links.New()}, model.CheckConfig{})
	if err != nil {
		t.Fatalf("RunSite: %v", err)
	}

	// Rewrite the volatile origin to a fixed one, then recompute IDs and re-sort
	// so the output is fully deterministic.
	normalize(&sr, srv.URL, canonHost)

	rep := model.Report{
		SchemaVersion: "1.0.0-draft",
		Tool:          model.ToolInfo{Name: "cairn", Version: "0.1.0-dev"},
		GeneratedAt:   "2026-01-01T00:00:00Z",
		Sites:         []model.SiteReport{sr},
	}

	var jsonBuf, tasksBuf bytes.Buffer
	if err := report.WriteJSON(&jsonBuf, rep); err != nil {
		t.Fatal(err)
	}
	report.WriteTasks(&tasksBuf, rep)

	checkGolden(t, "report.json", jsonBuf.Bytes())
	checkGolden(t, "cairn-tasks.md", tasksBuf.Bytes())
}

func normalize(sr *model.SiteReport, from, to string) {
	sr.URL = strings.ReplaceAll(sr.URL, from, to)
	for i := range sr.Findings {
		f := &sr.Findings[i]
		f.Location.URL = strings.ReplaceAll(f.Location.URL, from, to)
		f.Observed = strings.ReplaceAll(f.Observed, from, to)
		for j := range f.Location.AffectedURLs {
			f.Location.AffectedURLs[j] = strings.ReplaceAll(f.Location.AffectedURLs[j], from, to)
		}
	}
	engine.AssignIDs(sr.Findings)
	engine.SortFindings(sr.Findings)
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-golden to create)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match golden.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
