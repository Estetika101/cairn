package config

import (
	"strings"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	// A minimal config exercises the defaulting path: only sites is given.
	cfg, err := Parse([]byte("sites:\n  - url: https://example.com\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Crawl.PerHost.DelayMs; got != 250 {
		t.Errorf("perHost.delayMs default = %d, want 250", got)
	}
	if got := cfg.Crawl.MaxExtraFetches; got != 500 {
		t.Errorf("maxExtraFetches default = %d, want 500", got)
	}
	if !cfg.Crawl.RespectRobots {
		t.Errorf("respectRobots default = false, want true")
	}
	if got := cfg.FailOn; got != "error" {
		t.Errorf("failOn default = %q, want error", got)
	}
	if got := cfg.Accessibility.WCAGLevel; got != "AA" {
		t.Errorf("wcagLevel default = %q, want AA", got)
	}
	// Name defaults to the URL host when omitted.
	if got := cfg.Sites[0].Name; got != "example.com" {
		t.Errorf("site name default = %q, want example.com", got)
	}
}

func TestParseOverrides(t *testing.T) {
	src := `
sites:
  - name: Site
    url: https://example.com
    crawlLimit: 3
failOn: warn
crawl:
  maxExtraFetches: 1
  respectRobots: false
  perHost:
    delayMs: 500
`
	cfg, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.FailOn != "warn" {
		t.Errorf("failOn = %q, want warn", cfg.FailOn)
	}
	if cfg.Crawl.MaxExtraFetches != 1 {
		t.Errorf("maxExtraFetches = %d, want 1", cfg.Crawl.MaxExtraFetches)
	}
	if cfg.Crawl.RespectRobots {
		t.Errorf("respectRobots = true, want false (explicit override)")
	}
	if cfg.Crawl.PerHost.DelayMs != 500 {
		t.Errorf("perHost.delayMs = %d, want 500", cfg.Crawl.PerHost.DelayMs)
	}
	// An unset nested default survives a partial perHost override.
	if cfg.Crawl.PerHost.Concurrency != 2 {
		t.Errorf("perHost.concurrency = %d, want 2 (default kept)", cfg.Crawl.PerHost.Concurrency)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no sites", "failOn: error\n", "at least one site"},
		{"bad url", "sites:\n  - url: not-a-url\n", "absolute http(s) URL"},
		{"bad failOn", "sites:\n  - url: https://x.com\nfailOn: sometimes\n", "failOn"},
		{"bad wcag level", "sites:\n  - url: https://x.com\naccessibility:\n  wcagLevel: AAAA\n", "wcagLevel"},
		{"bad tier2 mode", "sites:\n  - url: https://x.com\ntier2:\n  mode: maybe\n", "tier2.mode"},
		{"bad format", "sites:\n  - url: https://x.com\noutput:\n  formats: [console, pdf]\n", "unknown format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCheckEnabled(t *testing.T) {
	src := `
sites:
  - url: https://example.com
checks:
  security: true
  seo: false
  seo-title: true
`
	cfg, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.CheckEnabled("security", "security-headers") {
		t.Errorf("security module should be enabled")
	}
	if cfg.CheckEnabled("seo", "seo-meta-desc") {
		t.Errorf("seo module disabled -> seo-meta-desc should be disabled")
	}
	// Check-ID override beats the module toggle (most-specific wins).
	if !cfg.CheckEnabled("seo", "seo-title") {
		t.Errorf("seo-title explicitly true should override module=false")
	}
	// Absent module/id -> enabled by default.
	if !cfg.CheckEnabled("links", "broken-links") {
		t.Errorf("absent -> should default enabled")
	}
}
