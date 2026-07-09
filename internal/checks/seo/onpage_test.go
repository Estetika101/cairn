package seo_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Estetika101/cairn/internal/checks/seo"
	"github.com/Estetika101/cairn/internal/model"
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
	return model.PageData{}, nil
}

func pageFromHTML(t *testing.T, url, html string) model.PageData {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return model.PageData{FinalURL: url, Doc: doc, Headers: http.Header{}}
}

func onlyFinding(t *testing.T, fs []model.Finding) model.Finding {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(fs), fs)
	}
	return fs[0]
}

func runOne(t *testing.T, c model.Check, page model.PageData) []model.Finding {
	t.Helper()
	fs, err := c.Run(&stubCtx{scope: model.ScopePage, page: page})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestTitle(t *testing.T) {
	cases := []struct {
		name   string
		html   string
		status model.Status
	}{
		{"missing", `<html><head></head><body></body></html>`, model.StatusFail},
		{"too short", `<html><head><title>Hi</title></head></html>`, model.StatusFail},
		{"good", `<html><head><title>A perfectly reasonable page title</title></head></html>`, model.StatusPass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := runOne(t, seo.Title(), pageFromHTML(t, "https://x/", tc.html))
			f := onlyFinding(t, fs)
			if f.Status != tc.status {
				t.Errorf("status = %s, want %s (observed: %s)", f.Status, tc.status, f.Observed)
			}
			if f.Criterion != "seo-title" || f.Location.URL != "https://x/" {
				t.Errorf("criterion/location wrong: %+v", f)
			}
		})
	}
}

func TestSingleH1(t *testing.T) {
	cases := []struct {
		name   string
		html   string
		status model.Status
	}{
		{"none", `<body><h2>x</h2></body>`, model.StatusFail},
		{"two", `<body><h1>a</h1><h1>b</h1></body>`, model.StatusFail},
		{"one", `<body><h1>a</h1></body>`, model.StatusPass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := onlyFinding(t, runOne(t, seo.SingleH1(), pageFromHTML(t, "https://x/", tc.html)))
			if f.Status != tc.status {
				t.Errorf("status = %s, want %s", f.Status, tc.status)
			}
		})
	}
}

func TestHeadingOrder(t *testing.T) {
	skip := pageFromHTML(t, "https://x/", `<body><h1>a</h1><h3>skipped to h3</h3></body>`)
	f := onlyFinding(t, runOne(t, seo.HeadingOrder(), skip))
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail (h1->h3 skip)", f.Status)
	}

	ok := pageFromHTML(t, "https://x/", `<body><h1>a</h1><h2>b</h2><h3>c</h3><h2>d</h2></body>`)
	f2 := onlyFinding(t, runOne(t, seo.HeadingOrder(), ok))
	if f2.Status != model.StatusPass {
		t.Errorf("status = %s, want pass (no skips, stepping back up is fine)", f2.Status)
	}
}

func TestImgAltCoverage(t *testing.T) {
	fs := runOne(t, seo.ImgAltCoverage(), pageFromHTML(t, "https://x/", `<body><img src="a.png"></body>`))
	f := onlyFinding(t, fs)
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail (missing alt)", f.Status)
	}

	none := runOne(t, seo.ImgAltCoverage(), pageFromHTML(t, "https://x/", `<body>no images</body>`))
	if len(none) != 0 {
		t.Errorf("no images present -> should emit no finding, got %+v", none)
	}
}

func TestMetaRobots(t *testing.T) {
	fs := runOne(t, seo.MetaRobots(), pageFromHTML(t, "https://x/", `<head><meta name="robots" content="noindex,nofollow"></head>`))
	f := onlyFinding(t, fs)
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail (noindex present)", f.Status)
	}

	ok := runOne(t, seo.MetaRobots(), pageFromHTML(t, "https://x/", `<head></head>`))
	f2 := onlyFinding(t, ok)
	if f2.Status != model.StatusPass {
		t.Errorf("status = %s, want pass (no robots meta = default)", f2.Status)
	}
}

func TestOpenGraphCompleteness(t *testing.T) {
	incomplete := runOne(t, seo.OpenGraph(), pageFromHTML(t, "https://x/",
		`<head><meta property="og:title" content="T"></head>`))
	f := onlyFinding(t, incomplete)
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail (missing og:description/image/url)", f.Status)
	}

	complete := runOne(t, seo.OpenGraph(), pageFromHTML(t, "https://x/", `<head>
		<meta property="og:title" content="T">
		<meta property="og:description" content="D">
		<meta property="og:image" content="https://x/i.png">
		<meta property="og:url" content="https://x/">
	</head>`))
	f2 := onlyFinding(t, complete)
	if f2.Status != model.StatusPass {
		t.Errorf("status = %s, want pass", f2.Status)
	}
}
