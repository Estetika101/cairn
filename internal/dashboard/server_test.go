package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Estetika101/cairn/internal/dashboard"
)

func TestReportEndpoint(t *testing.T) {
	dir := t.TempDir()
	want := `{"schemaVersion":"1.0.0-draft","sites":[]}`
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := dashboard.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/report")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

func TestReportEndpoint_MissingReport(t *testing.T) {
	srv, err := dashboard.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/report")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when report.json is absent", resp.StatusCode)
	}
}

func TestIndexServed(t *testing.T) {
	srv, err := dashboard.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

// The dashboard shows a live audit and must never be indexed if deployed
// beyond localhost — every response carries X-Robots-Tag, plus a disallow-all
// robots.txt for crawlers that ignore headers.
func TestNoIndexEverywhere(t *testing.T) {
	srv, err := dashboard.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	for _, path := range []string{"/", "/api/report", "/robots.txt", "/nonexistent"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("X-Robots-Tag"); !strings.Contains(got, "noindex") || !strings.Contains(got, "nofollow") {
			t.Errorf("%s: X-Robots-Tag = %q, want noindex and nofollow", path, got)
		}
	}

	resp, err := http.Get(ts.URL + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "Disallow: /") {
		t.Errorf("robots.txt = %q, want a disallow-all rule", string(body[:n]))
	}
}
