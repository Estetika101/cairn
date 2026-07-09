package seo

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// siteCheck is the common shape for the site-scoped SEO checks: they run once
// per site over the full corpus, and may use Fetch (sitemap, hreflang targets).
type siteCheck struct {
	id       string
	title    string
	severity model.Severity
	run      func(cc model.CheckContext) []model.Finding
}

func (c siteCheck) Meta() model.CheckMeta {
	return model.CheckMeta{
		ID: c.id, Module: "seo", Tier: 1, Scope: model.ScopeSite,
		Severity: c.severity, Title: c.title,
	}
}

func (c siteCheck) Run(cc model.CheckContext) ([]model.Finding, error) {
	fs := c.run(cc)
	for i := range fs {
		fs[i].Module, fs[i].Scope, fs[i].Criterion = "seo", model.ScopeSite, c.id
	}
	return fs, nil
}

// --- sitemap ---

type sitemapXML struct {
	XMLName  xml.Name   `xml:""`
	URLEntry []locEntry `xml:"url"`
	Sitemaps []locEntry `xml:"sitemap"`
}
type locEntry struct {
	Loc string `xml:"loc"`
}

func siteRoot(cc model.CheckContext) string {
	corpus := cc.Corpus()
	if len(corpus) == 0 {
		return ""
	}
	u, err := url.Parse(pageURL(corpus[0]))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// fetchSitemap fetches and parses <root>/sitemap.xml, mapping engine errors to
// the skipped reasons v0.4 §5c defines. ok=false with a non-nil skipped finding
// means "don't proceed"; the caller returns that finding as-is.
func fetchSitemap(cc model.CheckContext, root string) (sitemapXML, string, *model.Finding) {
	loc := model.Location{URL: root + "/sitemap.xml"}
	pd, err := cc.Fetch(context.Background(), loc.URL)
	switch {
	case errors.Is(err, model.ErrRobotsDisallowed):
		return sitemapXML{}, "", &model.Finding{Status: model.StatusSkipped, Reason: "disallowed by robots.txt", Location: loc}
	case errors.Is(err, model.ErrBudgetExhausted):
		return sitemapXML{}, "", &model.Finding{Status: model.StatusSkipped, Reason: "fetch budget exhausted", Location: loc}
	case errors.Is(err, model.ErrBlocked):
		return sitemapXML{}, "", &model.Finding{Status: model.StatusSkipped, Reason: "blocked", Location: loc}
	case err != nil:
		return sitemapXML{}, "", &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed: "request failed: " + err.Error(), Required: "a reachable sitemap.xml",
		}
	}
	if pd.Status >= 400 {
		return sitemapXML{}, "", &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed: fmt.Sprintf("HTTP %d", pd.Status), Required: "a reachable sitemap.xml",
			SuggestedFix: "Publish a sitemap.xml at the site root and link it from robots.txt.",
		}
	}
	var sm sitemapXML
	if err := xml.Unmarshal(pd.Body, &sm); err != nil {
		return sitemapXML{}, "", &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed: "sitemap.xml is not valid XML: " + err.Error(), Required: "well-formed sitemap XML",
			SuggestedFix: "Fix the sitemap XML syntax or regenerate it.",
		}
	}
	return sm, loc.URL, nil
}

// SitemapValid returns the seo-sitemap-valid check.
func SitemapValid() model.Check {
	return siteCheck{id: "seo-sitemap-valid", title: "sitemap.xml present, valid, reachable", severity: model.SeverityWarn, run: func(cc model.CheckContext) []model.Finding {
		root := siteRoot(cc)
		if root == "" {
			return nil
		}
		sm, sitemapURL, skip := fetchSitemap(cc, root)
		if skip != nil {
			return []model.Finding{*skip}
		}
		loc := model.Location{URL: sitemapURL}
		if sm.XMLName.Local == "sitemapindex" {
			return []model.Finding{{
				Status: model.StatusPass, Location: loc,
				Observed: fmt.Sprintf("sitemap index with %d child sitemap(s) (not recursed)", len(sm.Sitemaps)),
			}}
		}
		if sm.XMLName.Local != "urlset" {
			return []model.Finding{{
				Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
				Observed: "sitemap.xml root element is not <urlset> or <sitemapindex>",
				Required: "a valid sitemap <urlset> or <sitemapindex>",
			}}
		}
		return []model.Finding{{Status: model.StatusPass, Location: loc}}
	}}
}

// SitemapCoverage returns the seo-sitemap-coverage check (always status:info —
// it is a heuristic cross-check, not a pass/fail per v0.4 §5d).
func SitemapCoverage() model.Check {
	return siteCheck{id: "seo-sitemap-coverage", title: "sitemap vs. crawled coverage", severity: "", run: func(cc model.CheckContext) []model.Finding {
		root := siteRoot(cc)
		if root == "" {
			return nil
		}
		sm, sitemapURL, skip := fetchSitemap(cc, root)
		if skip != nil {
			// Coverage is always informational (never a scored fail), so a sitemap
			// fetch problem here is reported as skipped, not as a duplicate of
			// seo-sitemap-valid's fail under a second check ID.
			reason := skip.Reason
			if reason == "" {
				reason = skip.Observed
			}
			return []model.Finding{{
				Status: model.StatusSkipped, Reason: "sitemap unavailable: " + reason,
				Location: skip.Location,
			}}
		}
		if sm.XMLName.Local != "urlset" {
			return nil // an index or invalid sitemap has nothing to compare
		}

		sitemapSet := map[string]bool{}
		for _, e := range sm.URLEntry {
			sitemapSet[normalizeURL(e.Loc)] = true
		}
		crawledSet := map[string]bool{}
		for _, p := range cc.Corpus() {
			crawledSet[normalizeURL(pageURL(p))] = true
		}

		var onlyInSitemap, onlyCrawled int
		for u := range sitemapSet {
			if !crawledSet[u] {
				onlyInSitemap++
			}
		}
		for u := range crawledSet {
			if !sitemapSet[u] {
				onlyCrawled++
			}
		}

		return []model.Finding{{
			Status:   model.StatusInfo,
			Location: model.Location{URL: sitemapURL},
			Observed: fmt.Sprintf("%d sitemap URL(s), %d crawled page(s); %d sitemap URL(s) not crawled, %d crawled page(s) not in sitemap",
				len(sitemapSet), len(crawledSet), onlyInSitemap, onlyCrawled),
		}}
	}}
}

// --- hreflang reciprocity ---

// HreflangReciprocity returns the seo-hreflang-reciprocity check: does page A's
// claimed alternate B point back to A. Only runs against pages that declare
// hreflang alternates; silent (no findings) when none exist in the corpus.
func HreflangReciprocity() model.Check {
	return siteCheck{id: "seo-hreflang-reciprocity", title: "hreflang alternates are reciprocal", severity: model.SeverityWarn, run: func(cc model.CheckContext) []model.Finding {
		var findings []model.Finding
		checked := map[string]bool{} // dedup identical (A,B) pairs across the corpus

		for _, page := range cc.Corpus() {
			if page.Doc == nil {
				continue
			}
			a := pageURL(page)
			page.Doc.Find(`link[rel="alternate"][hreflang]`).Each(func(_ int, s *goquery.Selection) {
				href, ok := s.Attr("href")
				if !ok || strings.TrimSpace(href) == "" {
					return
				}
				b := resolveAgainst(a, href)
				if b == "" || normalizeURL(b) == normalizeURL(a) {
					return
				}
				pairKey := a + " -> " + b
				if checked[pairKey] {
					return
				}
				checked[pairKey] = true

				if f := checkReciprocal(cc, a, b); f != nil {
					findings = append(findings, *f)
				}
			})
		}
		return findings
	}}
}

func checkReciprocal(cc model.CheckContext, a, b string) *model.Finding {
	loc := model.Location{URL: a, Selector: "link[rel=alternate][hreflang]", AffectedURLs: []string{b}}
	pd, err := cc.Fetch(context.Background(), b)
	switch {
	case errors.Is(err, model.ErrRobotsDisallowed):
		return &model.Finding{Status: model.StatusSkipped, Reason: "disallowed by robots.txt", Location: loc}
	case errors.Is(err, model.ErrBudgetExhausted):
		return &model.Finding{Status: model.StatusSkipped, Reason: "fetch budget exhausted", Location: loc}
	case errors.Is(err, model.ErrBlocked):
		return &model.Finding{Status: model.StatusSkipped, Reason: "blocked", Location: loc}
	case err != nil:
		return &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed: fmt.Sprintf("alternate %s: request failed: %s", b, err.Error()),
			Required: "the claimed alternate must be reachable",
		}
	}
	if pd.Doc == nil {
		return &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed: fmt.Sprintf("alternate %s returned no parseable HTML", b),
			Required: "the alternate must declare a reciprocal hreflang link back",
		}
	}
	reciprocal := false
	pd.Doc.Find(`link[rel="alternate"][hreflang]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, ok := s.Attr("href")
		if !ok {
			return true
		}
		if normalizeURL(resolveAgainst(b, href)) == normalizeURL(a) {
			reciprocal = true
			return false
		}
		return true
	})
	if !reciprocal {
		return &model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn, Location: loc,
			Observed:     fmt.Sprintf("%s claims alternate %s, but %s does not link back", a, b, b),
			Required:     "the alternate declares a reciprocal hreflang link back to the source page",
			SuggestedFix: fmt.Sprintf("Add a hreflang alternate on %s pointing back to %s, or remove the one-sided link.", b, a),
		}
	}
	return nil // reciprocal is correct — no pass finding per pair to avoid duplicate-pair noise
}

// --- duplicate content across the corpus ---

// DuplicateTitle returns the seo-duplicate-title check.
func DuplicateTitle() model.Check {
	return siteCheck{id: "seo-duplicate-title", title: "No duplicate titles across pages", severity: model.SeverityWarn, run: func(cc model.CheckContext) []model.Finding {
		return duplicateGroups(cc, "titles", func(p model.PageData) string {
			if p.Doc == nil {
				return ""
			}
			return strings.TrimSpace(p.Doc.Find("title").First().Text())
		})
	}}
}

// DuplicateMetaDescription returns the seo-duplicate-meta-desc check.
func DuplicateMetaDescription() model.Check {
	return siteCheck{id: "seo-duplicate-meta-desc", title: "No duplicate meta descriptions across pages", severity: model.SeverityWarn, run: func(cc model.CheckContext) []model.Finding {
		return duplicateGroups(cc, "meta descriptions", func(p model.PageData) string {
			if p.Doc == nil {
				return ""
			}
			return strings.TrimSpace(metaContent(p.Doc, "description"))
		})
	}}
}

func duplicateGroups(cc model.CheckContext, label string, extract func(model.PageData) string) []model.Finding {
	groups := map[string][]string{}
	for _, p := range cc.Corpus() {
		v := extract(p)
		if v == "" {
			continue
		}
		groups[v] = append(groups[v], pageURL(p))
	}

	var findings []model.Finding
	var keys []string
	for v := range groups {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	for _, v := range keys {
		urls := groups[v]
		if len(urls) < 2 {
			continue
		}
		sort.Strings(urls)
		findings = append(findings, model.Finding{
			Status: model.StatusFail, Severity: model.SeverityWarn,
			Location:     model.Location{URL: urls[0], AffectedURLs: urls},
			Observed:     fmt.Sprintf("%d pages share the same %s: %q", len(urls), strings.TrimSuffix(label, "s"), truncate(v, 80)),
			Required:     "unique content per page",
			SuggestedFix: fmt.Sprintf("Give each page its own %s.", strings.TrimSuffix(label, "s")),
		})
	}
	return findings
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- URL helpers (local to seo; deliberately not shared with engine's
// unexported canonicalize, which is cache-key-specific per v0.4 §6b) ---

func resolveAgainst(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := b.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}

func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	out := u.Scheme + "://" + u.Host + path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
