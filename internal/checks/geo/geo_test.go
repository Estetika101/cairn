package geo_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Estetika101/verdict/internal/checks/geo"
	"github.com/Estetika101/verdict/internal/model"
	"github.com/PuerkitoBio/goquery"
)

type stubCtx struct {
	scope  model.Scope
	page   model.PageData
	corpus []model.PageData
	fetch  func(context.Context, string) (model.PageData, error)
}

func (s *stubCtx) Scope() model.Scope        { return s.scope }
func (s *stubCtx) Page() model.PageData      { return s.page }
func (s *stubCtx) Corpus() []model.PageData  { return s.corpus }
func (s *stubCtx) Config() model.CheckConfig { return model.CheckConfig{} }
func (s *stubCtx) Logf(string, ...any)       {}
func (s *stubCtx) Fetch(ctx context.Context, url string) (model.PageData, error) {
	if s.fetch != nil {
		return s.fetch(ctx, url)
	}
	return model.PageData{Status: 404}, nil
}

func runSite(t *testing.T, c model.Check, cc *stubCtx) []model.Finding {
	t.Helper()
	cc.scope = model.ScopeSite
	fs, err := c.Run(cc)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func onlyFinding(t *testing.T, fs []model.Finding) model.Finding {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(fs), fs)
	}
	return fs[0]
}

func pageFromHTML(t *testing.T, url, html string) model.PageData {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return model.PageData{FinalURL: url, Doc: doc, Headers: http.Header{}}
}

// --- BotPosture ---

func TestBotPosture_NoRobotsTxt(t *testing.T) {
	seed := pageFromHTML(t, "https://x.example/", `<body></body>`)
	cc := &stubCtx{corpus: []model.PageData{seed}}
	f := onlyFinding(t, runSite(t, geo.BotPosture(), cc))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info", f.Status)
	}
	if !strings.Contains(f.Observed, "no robots.txt found") {
		t.Errorf("observed = %q, want it to note no robots.txt", f.Observed)
	}
}

func TestBotPosture_PerBotAllowDisallow(t *testing.T) {
	seed := pageFromHTML(t, "https://x.example/", `<body></body>`)
	robotsBody := "User-agent: GPTBot\nDisallow: /\n\nUser-agent: ClaudeBot\nAllow: /\n"
	cc := &stubCtx{
		corpus: []model.PageData{seed},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			return model.PageData{Status: 200, Body: []byte(robotsBody)}, nil
		},
	}
	f := onlyFinding(t, runSite(t, geo.BotPosture(), cc))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info (bot posture is never scored)", f.Status)
	}
	if !strings.Contains(f.Observed, "GPTBot (training): disallowed") {
		t.Errorf("observed missing GPTBot disallowed: %s", f.Observed)
	}
	if !strings.Contains(f.Observed, "ClaudeBot (training): allowed") {
		t.Errorf("observed missing ClaudeBot allowed: %s", f.Observed)
	}
	// Google-Extended must be labeled as a training opt-out token, not a crawler.
	if !strings.Contains(f.Observed, "not a separate crawler") {
		t.Errorf("observed should explain Google-Extended/Applebot-Extended are opt-out tokens: %s", f.Observed)
	}
}

// --- LLMsTxt ---

func TestLLMsTxt_Absent(t *testing.T) {
	seed := pageFromHTML(t, "https://x.example/", `<body></body>`)
	cc := &stubCtx{corpus: []model.PageData{seed}}
	f := onlyFinding(t, runSite(t, geo.LLMsTxt(), cc))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info", f.Status)
	}
	if !strings.Contains(f.Observed, "no llms.txt found") {
		t.Errorf("observed = %q", f.Observed)
	}
}

func TestLLMsTxt_ValidH1(t *testing.T) {
	seed := pageFromHTML(t, "https://x.example/", `<body></body>`)
	cc := &stubCtx{
		corpus: []model.PageData{seed},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			if strings.HasSuffix(url, "/llms.txt") {
				return model.PageData{Status: 200, Body: []byte("# Example Docs\n\n> A summary.\n")}, nil
			}
			return model.PageData{Status: 404}, nil
		},
	}
	f := onlyFinding(t, runSite(t, geo.LLMsTxt(), cc))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info (never scored)", f.Status)
	}
	if !strings.Contains(f.Observed, "required H1 title: Example Docs") {
		t.Errorf("observed = %q, want it to report the parsed H1 title", f.Observed)
	}
	if !strings.Contains(f.Observed, "llms-full.txt not found") {
		t.Errorf("observed = %q, want the llms-full.txt bonus note", f.Observed)
	}
}

func TestLLMsTxt_MissingRequiredH1(t *testing.T) {
	seed := pageFromHTML(t, "https://x.example/", `<body></body>`)
	cc := &stubCtx{
		corpus: []model.PageData{seed},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			if strings.HasSuffix(url, "/llms.txt") {
				return model.PageData{Status: 200, Body: []byte("Just some text, no heading.\n")}, nil
			}
			return model.PageData{Status: 404}, nil
		},
	}
	f := onlyFinding(t, runSite(t, geo.LLMsTxt(), cc))
	// Even malformed, this stays info per the module's philosophy — never a
	// scored fail for something this new/inconsistently adopted.
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info even when malformed", f.Status)
	}
	if !strings.Contains(f.Observed, "missing the one required element") {
		t.Errorf("observed = %q, want it to flag the missing H1", f.Observed)
	}
}

// --- Freshness ---

func runPage(t *testing.T, c model.Check, page model.PageData) []model.Finding {
	t.Helper()
	fs, err := c.Run(&stubCtx{scope: model.ScopePage, page: page})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestFreshness_Absent(t *testing.T) {
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", `<body>no dates here</body>`)))
	if f.Status != model.StatusFail || f.Severity != model.SeverityWarn {
		t.Errorf("f = %+v, want fail/warn when no date metadata exists at all", f)
	}
}

func TestFreshness_JSONLD(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Article","datePublished":"2025-01-01","dateModified":"2026-01-01T00:00:00Z"}</script>`
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", html)))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info — age is reported, not judged", f.Status)
	}
	if !strings.Contains(f.Observed, "modified 2026-01-01") {
		t.Errorf("observed = %q, want it to prefer dateModified and report the parsed date", f.Observed)
	}
	if !strings.Contains(f.Observed, "day(s) ago") {
		t.Errorf("observed = %q, want a measured age, not a judgment", f.Observed)
	}
}

func TestFreshness_OpenGraphFallback(t *testing.T) {
	html := `<meta property="article:modified_time" content="2026-01-01T00:00:00Z">`
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", html)))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info", f.Status)
	}
	if !strings.Contains(f.Observed, "modified 2026-01-01") {
		t.Errorf("observed = %q, want the OpenGraph date used as a fallback", f.Observed)
	}
}

func TestFreshness_TimeElementFallback(t *testing.T) {
	html := `<time datetime="2026-01-01">Jan 1</time>`
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", html)))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info", f.Status)
	}
}

func TestFreshness_MalformedDate(t *testing.T) {
	html := `<meta property="article:published_time" content="not-a-date">`
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", html)))
	if f.Status != model.StatusFail || f.Severity != model.SeverityWarn {
		t.Errorf("f = %+v, want fail/warn for an unparseable date", f)
	}
}

func TestFreshness_NoImposedThreshold(t *testing.T) {
	// A date far in the past must still be `info`, never a fail — the check
	// must not invent a staleness cutoff.
	html := fmt.Sprintf(`<meta property="article:modified_time" content="%s">`, "2015-01-01T00:00:00Z")
	f := onlyFinding(t, runPage(t, geo.Freshness(), pageFromHTML(t, "https://x/", html)))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info even for a very old date — no staleness threshold should be imposed", f.Status)
	}
}
