package seo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Estetika101/cairn/internal/checks/seo"
	"github.com/Estetika101/cairn/internal/model"
)

func runSite(t *testing.T, c model.Check, cc *stubCtx) []model.Finding {
	t.Helper()
	cc.scope = model.ScopeSite
	fs, err := c.Run(cc)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestHreflangReciprocity(t *testing.T) {
	a := pageFromHTML(t, "https://x/en", `<head><link rel="alternate" hreflang="fr" href="https://x/fr"></head>`)
	// /fr does NOT link back to /en -> should fail.
	frNoLinkBack := pageFromHTML(t, "https://x/fr", `<head></head>`)

	cc := &stubCtx{
		corpus: []model.PageData{a},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			if url == "https://x/fr" {
				return frNoLinkBack, nil
			}
			return model.PageData{}, errors.New("unexpected fetch")
		},
	}
	fs := runSite(t, seo.HreflangReciprocity(), cc)
	f := onlyFinding(t, fs)
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail (fr does not link back to en)", f.Status)
	}
	if f.Location.URL != "https://x/en" || len(f.Location.AffectedURLs) != 1 || f.Location.AffectedURLs[0] != "https://x/fr" {
		t.Errorf("location wrong: %+v", f.Location)
	}
}

func TestHreflangReciprocity_Reciprocal(t *testing.T) {
	a := pageFromHTML(t, "https://x/en", `<head><link rel="alternate" hreflang="fr" href="https://x/fr"></head>`)
	frLinksBack := pageFromHTML(t, "https://x/fr", `<head><link rel="alternate" hreflang="en" href="https://x/en"></head>`)

	cc := &stubCtx{
		corpus: []model.PageData{a},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			return frLinksBack, nil
		},
	}
	fs := runSite(t, seo.HreflangReciprocity(), cc)
	if len(fs) != 0 {
		t.Errorf("reciprocal alternates should emit no finding, got %+v", fs)
	}
}

func TestHreflangReciprocity_RobotsDisallowed(t *testing.T) {
	a := pageFromHTML(t, "https://x/en", `<head><link rel="alternate" hreflang="fr" href="https://x/fr"></head>`)
	cc := &stubCtx{
		corpus: []model.PageData{a},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			return model.PageData{}, model.ErrRobotsDisallowed
		},
	}
	fs := runSite(t, seo.HreflangReciprocity(), cc)
	f := onlyFinding(t, fs)
	if f.Status != model.StatusSkipped || f.Reason != "disallowed by robots.txt" {
		t.Errorf("f = %+v, want skipped/disallowed by robots.txt", f)
	}
}

func TestSitemapValid(t *testing.T) {
	seed := pageFromHTML(t, "https://x/", `<body></body>`)

	t.Run("valid urlset", func(t *testing.T) {
		cc := &stubCtx{
			corpus: []model.PageData{seed},
			fetch: func(_ context.Context, url string) (model.PageData, error) {
				return model.PageData{Status: 200, Body: []byte(`<urlset><url><loc>https://x/</loc></url></urlset>`)}, nil
			},
		}
		f := onlyFinding(t, runSite(t, seo.SitemapValid(), cc))
		if f.Status != model.StatusPass {
			t.Errorf("status = %s, want pass; observed=%s", f.Status, f.Observed)
		}
	})

	t.Run("404", func(t *testing.T) {
		cc := &stubCtx{
			corpus: []model.PageData{seed},
			fetch: func(_ context.Context, url string) (model.PageData, error) {
				return model.PageData{Status: 404}, nil
			},
		}
		f := onlyFinding(t, runSite(t, seo.SitemapValid(), cc))
		if f.Status != model.StatusFail {
			t.Errorf("status = %s, want fail (404)", f.Status)
		}
	})

	t.Run("invalid xml", func(t *testing.T) {
		cc := &stubCtx{
			corpus: []model.PageData{seed},
			fetch: func(_ context.Context, url string) (model.PageData, error) {
				return model.PageData{Status: 200, Body: []byte(`not xml at all <<<`)}, nil
			},
		}
		f := onlyFinding(t, runSite(t, seo.SitemapValid(), cc))
		if f.Status != model.StatusFail {
			t.Errorf("status = %s, want fail (invalid xml)", f.Status)
		}
	})
}

func TestSitemapCoverage(t *testing.T) {
	crawled := pageFromHTML(t, "https://x/a", `<body></body>`)
	cc := &stubCtx{
		corpus: []model.PageData{crawled},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			return model.PageData{Status: 200, Body: []byte(
				`<urlset><url><loc>https://x/a</loc></url><url><loc>https://x/b</loc></url></urlset>`)}, nil
		},
	}
	f := onlyFinding(t, runSite(t, seo.SitemapCoverage(), cc))
	if f.Status != model.StatusInfo {
		t.Errorf("status = %s, want info", f.Status)
	}
}

// Regression: when the sitemap fetch fails, coverage must report skipped (info
// track), never re-emit seo-sitemap-valid's fail under its own check ID.
func TestSitemapCoverage_SitemapUnavailable(t *testing.T) {
	crawled := pageFromHTML(t, "https://x/a", `<body></body>`)
	cc := &stubCtx{
		corpus: []model.PageData{crawled},
		fetch: func(_ context.Context, url string) (model.PageData, error) {
			return model.PageData{Status: 404}, nil
		},
	}
	f := onlyFinding(t, runSite(t, seo.SitemapCoverage(), cc))
	if f.Status != model.StatusSkipped {
		t.Errorf("status = %s, want skipped (coverage is never a scored fail)", f.Status)
	}
	if f.Severity != "" {
		t.Errorf("severity = %q, want empty on a skipped finding", f.Severity)
	}
}

func TestDuplicateTitle(t *testing.T) {
	p1 := pageFromHTML(t, "https://x/a", `<title>Same Title</title>`)
	p2 := pageFromHTML(t, "https://x/b", `<title>Same Title</title>`)
	p3 := pageFromHTML(t, "https://x/c", `<title>Different</title>`)

	cc := &stubCtx{corpus: []model.PageData{p1, p2, p3}}
	fs := runSite(t, seo.DuplicateTitle(), cc)
	f := onlyFinding(t, fs)
	if f.Status != model.StatusFail {
		t.Errorf("status = %s, want fail", f.Status)
	}
	if len(f.Location.AffectedURLs) != 2 {
		t.Errorf("affectedUrls = %v, want both duplicate pages", f.Location.AffectedURLs)
	}
}
