// Package links holds the broken-links check: the site-scoped check whose entire
// job is fetching, and thus the proof that CheckContext.Fetch, the budget, robots
// handling, and every skipped reason work end to end (v0.4 §5 / slice §S5b).
package links

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
	"github.com/PuerkitoBio/goquery"
)

type brokenLinks struct{}

// New returns the broken-links check.
func New() model.Check { return brokenLinks{} }

func (brokenLinks) Meta() model.CheckMeta {
	return model.CheckMeta{
		ID:       "broken-links",
		Module:   "links",
		Tier:     1,
		Scope:    model.ScopeSite,
		Severity: model.SeverityError,
		Title:    "Broken links and unreachable resources",
	}
}

// linkTarget accumulates the referrer pages for one unique target reference.
type linkTarget struct {
	abs       string // absolute URL, including any fragment (identity of the finding)
	fragment  string
	referrers map[string]bool
}

func (brokenLinks) Run(cc model.CheckContext) ([]model.Finding, error) {
	checkExternal := cc.Config().CheckExternal

	// 1. Collect every unique in-scope link across the corpus.
	targets := map[string]*linkTarget{}
	addRef := func(abs, frag, referrer string) {
		t := targets[abs]
		if t == nil {
			t = &linkTarget{abs: abs, fragment: frag, referrers: map[string]bool{}}
			targets[abs] = t
		}
		t.referrers[referrer] = true
	}

	for _, pageData := range cc.Corpus() {
		if pageData.Doc == nil {
			continue
		}
		referrer := pageURL(pageData)
		base, err := url.Parse(referrer)
		if err != nil {
			continue
		}
		host := strings.ToLower(base.Host)
		collect(pageData.Doc, base, host, checkExternal, addRef)
	}

	// 2. Fetch each unique target in a deterministic order and classify.
	keys := make([]string, 0, len(targets))
	for k := range targets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var findings []model.Finding
	for _, k := range keys {
		findings = append(findings, classify(cc, targets[k]))
	}
	return findings, nil
}

// collect pulls a[href] link targets plus img/script/link resource targets from
// one page, resolving each against base and filtering by scope.
func collect(doc *goquery.Document, base *url.URL, host string, checkExternal bool, add func(abs, frag, referrer string)) {
	referrer := base.String()
	consider := func(raw string, keepFragment bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "tel:") ||
			strings.HasPrefix(raw, "javascript:") || strings.HasPrefix(raw, "data:") {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		internal := strings.ToLower(u.Host) == host
		if !internal && !checkExternal {
			return
		}
		frag := u.Fragment
		if !keepFragment {
			u.Fragment = ""
		}
		add(u.String(), frag, referrer)
	}

	for _, sel := range []struct {
		q, attr  string
		fragment bool
	}{
		{"a[href]", "href", true},
		{"img[src]", "src", false},
		{"script[src]", "src", false},
		{"link[href]", "href", false},
	} {
		doc.Find(sel.q).Each(func(_ int, s *goquery.Selection) {
			if v, ok := s.Attr(sel.attr); ok {
				consider(v, sel.fragment)
			}
		})
	}
}

// classify fetches one target and turns the outcome into a finding. The finding
// identity is the target (location.url); referrers go in affectedUrls, which is
// excluded from the content-hash ID so the ID is stable as referrers change.
func classify(cc model.CheckContext, t *linkTarget) model.Finding {
	loc := model.Location{URL: t.abs, AffectedURLs: sortedKeys(t.referrers)}
	f := model.Finding{Module: "links", Scope: model.ScopeSite, Location: loc}

	pd, err := cc.Fetch(context.Background(), t.abs)
	switch {
	case errors.Is(err, model.ErrRobotsDisallowed):
		f.Status, f.Criterion, f.Reason = model.StatusSkipped, "links: reachable", "disallowed by robots.txt"
		return f
	case errors.Is(err, model.ErrBudgetExhausted):
		f.Status, f.Criterion, f.Reason = model.StatusSkipped, "links: reachable", "fetch budget exhausted"
		return f
	case errors.Is(err, model.ErrBlocked):
		f.Status, f.Criterion, f.Reason = model.StatusSkipped, "links: reachable", "blocked"
		return f
	case err != nil:
		f.Status, f.Severity, f.Criterion = model.StatusFail, model.SeverityError, "links: reachable"
		f.Observed = "request failed: " + err.Error()
		f.Required = "a reachable target returning 2xx"
		return f
	}

	switch {
	case pd.Status >= 400:
		f.Status, f.Severity, f.Criterion = model.StatusFail, model.SeverityError, "links: reachable"
		f.Observed = fmt.Sprintf("HTTP %d", pd.Status)
		f.Required = "a reachable target returning 2xx"
		f.SuggestedFix = "Fix or remove the link; the target is dead."
	case len(pd.RedirectChain) > 1:
		f.Status, f.Severity, f.Criterion = model.StatusFail, model.SeverityWarn, "links: redirect-chain"
		f.Observed = "redirect chain: " + strings.Join(pd.RedirectChain, " -> ")
		f.Required = "at most one redirect hop"
		f.SuggestedFix = "Point the link at the final URL to avoid the redirect chain."
	case t.fragment != "" && !fragmentExists(pd.Doc, t.fragment):
		f.Status, f.Severity, f.Criterion = model.StatusFail, model.SeverityWarn, "links: fragment"
		f.Observed = fmt.Sprintf("fragment #%s not found in target", t.fragment)
		f.Required = "an element with a matching id or name"
		f.SuggestedFix = "Fix the #fragment or add the missing anchor to the target page."
	default:
		f.Status, f.Criterion = model.StatusPass, "links: reachable"
	}
	return f
}

func fragmentExists(doc *goquery.Document, frag string) bool {
	if doc == nil {
		return false
	}
	frag = strings.ReplaceAll(frag, `"`, "")
	sel := fmt.Sprintf(`[id="%s"], [name="%s"]`, frag, frag)
	return doc.Find(sel).Length() > 0
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pageURL(p model.PageData) string {
	if p.FinalURL != "" {
		return p.FinalURL
	}
	return p.RequestedURL
}
