package dashboard_test

import (
	"bytes"
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

	srv, err := dashboard.New(dashboard.Options{ReportDir: dir})
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
	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir()})
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
	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir()})
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
	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir()})
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

const validConfigYAML = `
sites:
  - name: Example
    url: https://example.com
    crawlLimit: 3
checks:
  security: true
failOn: error
`

func writeConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cairn.yaml")
	if err := os.WriteFile(path, []byte(validConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Without --config, /api/config is disabled outright (view-only serve mode).
func TestConfigEndpoint_DisabledWithoutConfigPath(t *testing.T) {
	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 when no --config was given", resp.StatusCode)
	}
}

// GET returns the full parsed config; POST merges an edit onto a fresh load
// and writes it back, leaving untouched sections (here: failOn) intact.
func TestConfigEndpoint_GetAndPost(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)

	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir(), ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	getResp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}

	// Post an edit that only mentions sites — failOn and checks are absent
	// from this JSON and must survive unchanged (merge-onto-loaded-copy).
	edit := `{"sites":[{"name":"Changed","url":"https://changed.example.com","crawlLimit":9}]}`
	postResp, err := http.Post(ts.URL+"/api/config", "application/json", strings.NewReader(edit))
	if err != nil {
		t.Fatal(err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST status = %d, want 204", postResp.StatusCode)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	written := string(raw)
	if !strings.Contains(written, "changed.example.com") {
		t.Errorf("config file wasn't updated with the new site: %s", written)
	}
	if !strings.Contains(strings.ToLower(written), "failon: error") {
		t.Errorf("failOn should survive untouched (absent from the POST body): %s", written)
	}
}

// A POST that fails validation (empty sites list) is rejected with 400 and
// the file on disk is left untouched.
func TestConfigEndpoint_PostInvalid(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	before, _ := os.ReadFile(configPath)

	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir(), ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/config", "application/json", strings.NewReader(`{"sites":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an empty sites list", resp.StatusCode)
	}
	after, _ := os.ReadFile(configPath)
	if string(before) != string(after) {
		t.Errorf("config file was modified despite failing validation")
	}
}

// Without a RunAudit callback, /api/audit is disabled.
func TestAuditEndpoint_DisabledWithoutCallback(t *testing.T) {
	srv, err := dashboard.New(dashboard.Options{ReportDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/audit", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 when no RunAudit callback was given", resp.StatusCode)
	}
}

func TestAuditEndpoint_Triggers(t *testing.T) {
	called := false
	srv, err := dashboard.New(dashboard.Options{
		ReportDir: t.TempDir(),
		RunAudit:  func() error { called = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/audit", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if !called {
		t.Error("RunAudit callback was not invoked")
	}
}

// Write-capable endpoints (config POST, audit POST) reject a non-loopback
// remote address unless AllowRemoteConfig is set — the httptest server only
// ever sees 127.0.0.1 as its real peer, so a request is built directly and
// dispatched via ServeHTTP with a spoofed RemoteAddr to exercise the guard.
func TestRequireLocal_RejectsRemote(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srv, err := dashboard.New(dashboard.Options{
		ReportDir:  t.TempDir(),
		ConfigPath: configPath,
		RunAudit:   func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/config", "/api/audit"} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		req.RemoteAddr = "203.0.113.5:54321" // TEST-NET-3, definitely not loopback
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 for a non-loopback remote addr", path, rec.Code)
		}
	}
}

func TestRequireLocal_AllowsRemoteWhenConfigured(t *testing.T) {
	called := false
	srv, err := dashboard.New(dashboard.Options{
		ReportDir:         t.TempDir(),
		AllowRemoteConfig: true,
		RunAudit:          func() error { called = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/audit", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 when AllowRemoteConfig is true", rec.Code)
	}
	if !called {
		t.Error("RunAudit callback was not invoked despite AllowRemoteConfig")
	}
}
