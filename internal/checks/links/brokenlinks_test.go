package links_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Estetika101/verdict/internal/checks/links"
	"github.com/Estetika101/verdict/internal/engine"
	"github.com/Estetika101/verdict/internal/model"
)

func cfg() model.CrawlConfig {
	return model.CrawlConfig{
		RequestTimeoutMs:      5000,
		UserAgent:             "verdict/0.1 (+test)",
		MaxRetries:            0,
		RetryAfterCapMs:       1000,
		MaxConcurrentRequests: 8,
		MaxExtraFetches:       500,
		SiteConcurrency:       1,
		RespectRobots:         false,
		PerHost:               model.PerHostConfig{Concurrency: 4, DelayMs: 0},
	}
}

func runLinks(t *testing.T, c model.CrawlConfig, seed string, checkExternal bool) []model.Finding {
	t.Helper()
	sr, err := engine.RunSite(context.Background(), c,
		engine.SiteTarget{Name: "fixture", URL: seed, CrawlLimit: 10},
		[]model.Check{links.New()}, model.CheckConfig{CheckExternal: checkExternal})
	if err != nil {
		t.Fatalf("RunSite: %v", err)
	}
	return sr.Findings
}

func find(t *testing.T, fs []model.Finding, url string) model.Finding {
	t.Helper()
	for _, f := range fs {
		if f.Location.URL == url {
			return f
		}
	}
	t.Fatalf("no finding for %s (have %d findings)", url, len(fs))
	return model.Finding{}
}

func html(body string) string {
	return "<html><head><title>t</title></head><body>" + body + "</body></html>"
}

// Rows 4 + 5: a dead link, and two pages linking it produce ONE finding whose
// affectedUrls lists both referrers.
func TestBrokenLinks_DeadAndDedup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, html(`<a href="/a">a</a><a href="/b">b</a>`))
		case "/a", "/b":
			fmt.Fprint(w, html(`<a href="/dead">dead</a>`))
		case "/dead":
			w.WriteHeader(http.StatusNotFound)
		default:
			fmt.Fprint(w, html(""))
		}
	}))
	defer srv.Close()

	fs := runLinks(t, cfg(), srv.URL, false)
	dead := find(t, fs, srv.URL+"/dead")
	if dead.Status != model.StatusFail || dead.Severity != model.SeverityError {
		t.Errorf("/dead = %s/%s, want fail/error", dead.Status, dead.Severity)
	}
	if len(dead.Location.AffectedURLs) != 2 {
		t.Errorf("affectedUrls = %v, want both /a and /b", dead.Location.AffectedURLs)
	}
	var deadCount int
	for _, f := range fs {
		if f.Location.URL == srv.URL+"/dead" {
			deadCount++
		}
	}
	if deadCount != 1 {
		t.Errorf("got %d findings for /dead, want exactly 1 (deduped)", deadCount)
	}
}

// Rows 6 + 7: a >1-hop redirect chain warns; a missing fragment warns.
func TestBrokenLinks_RedirectAndFragment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, html(`<a href="/moved">m</a> <a href="/#missing">frag</a>`))
		case "/moved":
			http.Redirect(w, r, "/mid", http.StatusMovedPermanently)
		case "/mid":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			fmt.Fprint(w, html("ok"))
		default:
			fmt.Fprint(w, html(""))
		}
	}))
	defer srv.Close()

	fs := runLinks(t, cfg(), srv.URL, false)

	redirect := find(t, fs, srv.URL+"/moved")
	if redirect.Status != model.StatusFail || redirect.Criterion != "links: redirect-chain" {
		t.Errorf("/moved = %s (%s), want fail links: redirect-chain", redirect.Status, redirect.Criterion)
	}

	var frag model.Finding
	for _, f := range fs {
		if f.Criterion == "links: fragment" {
			frag = f
		}
	}
	if frag.Status != model.StatusFail || !strings.Contains(frag.Observed, "fragment #missing") {
		t.Errorf("fragment finding = %+v, want fail with 'fragment #missing'", frag)
	}
}

// Row 8: a robots-disallowed target is skipped, not passed or failed.
func TestBrokenLinks_RobotsDisallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
		case "/":
			fmt.Fprint(w, html(`<a href="/private">p</a>`))
		default:
			fmt.Fprint(w, html("secret"))
		}
	}))
	defer srv.Close()

	c := cfg()
	c.RespectRobots = true
	fs := runLinks(t, c, srv.URL, false)

	p := find(t, fs, srv.URL+"/private")
	if p.Status != model.StatusSkipped || p.Reason != "disallowed by robots.txt" {
		t.Errorf("/private = %s (%q), want skipped 'disallowed by robots.txt'", p.Status, p.Reason)
	}
}

// Row 9: with maxExtraFetches:1 and three external links, one is checked and the
// rest are skipped with the budget reason.
func TestBrokenLinks_BudgetExhausted(t *testing.T) {
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, html("ok"))
	}))
	defer ext.Close()

	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, html(fmt.Sprintf(`<a href="%s/e1">1</a><a href="%s/e2">2</a><a href="%s/e3">3</a>`,
			ext.URL, ext.URL, ext.URL)))
	}))
	defer main.Close()

	c := cfg()
	c.MaxExtraFetches = 1
	fs := runLinks(t, c, main.URL, true) // checkExternal = true

	var checked, budgetSkips int
	for _, f := range fs {
		if !strings.HasPrefix(f.Location.URL, ext.URL) {
			continue
		}
		switch {
		case f.Status == model.StatusPass:
			checked++
		case f.Status == model.StatusSkipped && f.Reason == "fetch budget exhausted":
			budgetSkips++
		}
	}
	if checked != 1 || budgetSkips != 2 {
		t.Errorf("checked=%d budgetSkips=%d, want 1 and 2", checked, budgetSkips)
	}
}

// Row 11: a WAF/challenge response is skipped as "blocked", not failed as a 4xx.
func TestBrokenLinks_Blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, html(`<a href="/cf">cf</a>`))
		case "/cf":
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "<html><body>Attention Required! | Cloudflare</body></html>")
		default:
			fmt.Fprint(w, html(""))
		}
	}))
	defer srv.Close()

	fs := runLinks(t, cfg(), srv.URL, false)
	cf := find(t, fs, srv.URL+"/cf")
	if cf.Status != model.StatusSkipped || cf.Reason != "blocked" {
		t.Errorf("/cf = %s (%q), want skipped 'blocked'", cf.Status, cf.Reason)
	}
}
