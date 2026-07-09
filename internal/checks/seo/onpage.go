// Package seo implements the SEO check module (v0.4 §5d): title/meta/canonical/
// heading/alt/robots/viewport/social checks (page-scoped) and hreflang
// reciprocity, sitemap validity/coverage, and duplicate-content checks
// (site-scoped). All SEO checks are advisory by default (fail -> warn; none
// gate as error) per the spec's own table.
package seo

import (
	"fmt"
	"strings"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// Recommended length bounds. The spec pins meta-description at ~150-160 chars
// explicitly; title bounds follow the same widely-cited SEO convention (Google
// truncates well past 60 chars in most SERPs) since the spec only says "within
// length bounds" without a number.
const (
	titleMinLen    = 10
	titleMaxLen    = 60
	metaDescMinLen = 120
	metaDescMaxLen = 160
)

func pageURL(p model.PageData) string {
	if p.FinalURL != "" {
		return p.FinalURL
	}
	return p.RequestedURL
}

// pageCheck is the common shape for the page-scoped SEO checks: pure functions
// of the page's parsed DOM, one Finding per page.
type pageCheck struct {
	id    string
	title string
	run   func(doc *goquery.Document, loc model.Location) model.Finding
}

func (c pageCheck) Meta() model.CheckMeta {
	return model.CheckMeta{
		ID: c.id, Module: "seo", Tier: 1, Scope: model.ScopePage,
		Severity: model.SeverityWarn, Title: c.title,
	}
}

func (c pageCheck) Run(cc model.CheckContext) ([]model.Finding, error) {
	page := cc.Page()
	loc := model.Location{URL: pageURL(page)}
	if page.Doc == nil {
		return nil, nil // no parsed HTML (non-HTML response); nothing to check
	}
	f := c.run(page.Doc, loc)
	if f.Status == "" {
		return nil, nil // check declined to opine (e.g. precondition not met)
	}
	f.Module, f.Scope, f.Criterion = "seo", model.ScopePage, c.id
	if f.Location.URL == "" {
		f.Location = loc
	}
	return []model.Finding{f}, nil
}

func pass() model.Finding { return model.Finding{Status: model.StatusPass} }

func fail(observed, required, fix string) model.Finding {
	return model.Finding{
		Status: model.StatusFail, Severity: model.SeverityWarn,
		Observed: observed, Required: required, SuggestedFix: fix,
	}
}

// Title returns the seo-title check: <title> present and within length bounds.
func Title() model.Check {
	return pageCheck{id: "seo-title", title: "Title tag present and sized", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		text := strings.TrimSpace(doc.Find("title").First().Text())
		if text == "" {
			return fail("<title> missing or empty", "a non-empty <title>",
				"Add a descriptive <title> tag, roughly 10-60 characters.")
		}
		if n := len(text); n < titleMinLen || n > titleMaxLen {
			return fail(fmt.Sprintf("title is %d characters: %q", n, text),
				fmt.Sprintf("%d-%d characters", titleMinLen, titleMaxLen),
				"Rewrite the title to fall within the recommended length so it isn't truncated or too thin.")
		}
		return pass()
	}}
}

// MetaDescription returns the seo-meta-desc check: meta description present.
func MetaDescription() model.Check {
	return pageCheck{id: "seo-meta-desc", title: "Meta description present", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		content := metaContent(doc, "description")
		if strings.TrimSpace(content) == "" {
			return fail("meta description missing or empty", "a non-empty meta description",
				fmt.Sprintf("Add a %d-%d character meta description summarizing the page.", metaDescMinLen, metaDescMaxLen))
		}
		return pass()
	}}
}

// MetaDescriptionLength returns the seo-meta-desc-length check: ~150-160 chars.
// It declines to opine when the description is absent (seo-meta-desc owns that).
func MetaDescriptionLength() model.Check {
	return pageCheck{id: "seo-meta-desc-length", title: "Meta description length", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		content := strings.TrimSpace(metaContent(doc, "description"))
		if content == "" {
			return model.Finding{} // absence is seo-meta-desc's concern, not this check's
		}
		if n := len(content); n < metaDescMinLen || n > metaDescMaxLen {
			return fail(fmt.Sprintf("meta description is %d characters", n),
				fmt.Sprintf("~%d-%d characters", metaDescMinLen, metaDescMaxLen),
				"Rewrite the meta description to land in the recommended length so search engines don't truncate or pad it.")
		}
		return pass()
	}}
}

// Canonical returns the seo-canonical check: canonical link present and resolvable.
func Canonical() model.Check {
	return pageCheck{id: "seo-canonical", title: "Canonical link present", run: func(doc *goquery.Document, loc model.Location) model.Finding {
		href, ok := doc.Find(`link[rel="canonical"]`).First().Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return fail("no <link rel=\"canonical\"> present", "a canonical link",
				"Add <link rel=\"canonical\" href=\"...\"> pointing at the preferred URL for this content.")
		}
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") && !strings.HasPrefix(href, "/") {
			return fail("canonical href is not an absolute or root-relative URL: "+href,
				"an absolute (or root-relative) URL", "Use an absolute canonical URL, not a bare relative path.")
		}
		return pass()
	}}
}

// SingleH1 returns the seo-single-h1 check: exactly one <h1>.
func SingleH1() model.Check {
	return pageCheck{id: "seo-single-h1", title: "Exactly one H1", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		n := doc.Find("h1").Length()
		switch {
		case n == 0:
			return fail("no <h1> found", "exactly one <h1>", "Add a single <h1> describing the page's main content.")
		case n > 1:
			return fail(fmt.Sprintf("%d <h1> elements found", n), "exactly one <h1>",
				"Demote extra <h1> elements to <h2> or lower so there is exactly one page title heading.")
		default:
			return pass()
		}
	}}
}

// HeadingOrder returns the seo-heading-order check: heading levels don't skip.
func HeadingOrder() model.Check {
	return pageCheck{id: "seo-heading-order", title: "Heading levels don't skip", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		var skips []string
		prev := 0
		doc.Find("h1, h2, h3, h4, h5, h6").Each(func(_ int, s *goquery.Selection) {
			lvl := int(s.Get(0).Data[1] - '0')
			if prev != 0 && lvl > prev+1 {
				skips = append(skips, fmt.Sprintf("h%d -> h%d", prev, lvl))
			}
			prev = lvl
		})
		if len(skips) > 0 {
			return fail("heading level skip(s): "+strings.Join(skips, ", "),
				"each heading level steps down by at most one",
				"Insert the missing intermediate heading level(s), or renumber so levels don't skip.")
		}
		return pass()
	}}
}

// ImgAltCoverage returns the seo-img-alt-coverage check: images carry alt.
// This is the SEO view (presence only); WCAG 1.1.1 (alt text quality) belongs
// to the accessibility module.
func ImgAltCoverage() model.Check {
	return pageCheck{id: "seo-img-alt-coverage", title: "Images carry alt attributes", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		imgs := doc.Find("img")
		total := imgs.Length()
		if total == 0 {
			return model.Finding{}
		}
		missing := 0
		imgs.Each(func(_ int, s *goquery.Selection) {
			if _, ok := s.Attr("alt"); !ok {
				missing++
			}
		})
		if missing > 0 {
			return fail(fmt.Sprintf("%d of %d images missing an alt attribute", missing, total),
				"every <img> carries an alt attribute",
				"Add alt=\"...\" to every image (empty alt=\"\" is fine for purely decorative images).")
		}
		return pass()
	}}
}

// MetaRobots returns the seo-meta-robots check: no accidental noindex/nofollow.
func MetaRobots() model.Check {
	return pageCheck{id: "seo-meta-robots", title: "No accidental noindex/nofollow", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		content := strings.ToLower(metaContent(doc, "robots"))
		if strings.Contains(content, "noindex") || strings.Contains(content, "nofollow") {
			return fail("meta robots: "+content, "no noindex/nofollow (unless intentional)",
				"Remove noindex/nofollow from the robots meta tag if this page should be indexed and followed.")
		}
		return pass()
	}}
}

// Viewport returns the seo-viewport check: responsive viewport meta present.
func Viewport() model.Check {
	return pageCheck{id: "seo-viewport", title: "Responsive viewport meta present", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		if strings.TrimSpace(metaContent(doc, "viewport")) == "" {
			return fail("no <meta name=\"viewport\"> present", "a responsive viewport meta tag",
				"Add <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">.")
		}
		return pass()
	}}
}

func metaContent(doc *goquery.Document, name string) string {
	v, _ := doc.Find(fmt.Sprintf(`meta[name="%s"]`, name)).First().Attr("content")
	return v
}
