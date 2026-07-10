package geo

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
)

// LLMsTxt returns the geo-llms-txt check. Per the actual spec at llmstxt.org
// (informal, community-governed — no IETF RFC, authored by Jeremy Howard,
// discussed via GitHub + Discord), the ENTIRE required structure is an H1
// heading naming the site/project as the first significant content; an
// optional leading byte-order mark is allowed before it. Everything else
// (the summary blockquote, detail sections, H2-delimited link lists) is
// optional. This check verifies exactly that one requirement — nothing more
// invented, nothing less.
//
// Status is always "info", never a scored fail, for both absence AND
// malformation: adoption sits under 0.4% of sites in the one dataset checked
// (mid-2026), so treating it as a defect — even a badly-formed one — would
// misrepresent a genuinely optional, still-forming convention as a broken
// requirement.
func LLMsTxt() model.Check {
	return siteCheck{id: "geo-llms-txt", title: "llms.txt presence and structure", run: func(cc model.CheckContext) []model.Finding {
		root := siteRoot(cc)
		if root == "" {
			return nil
		}
		loc := model.Location{URL: root + "/llms.txt"}

		pd, err := cc.Fetch(context.Background(), loc.URL)
		switch {
		case errors.Is(err, model.ErrBudgetExhausted):
			return []model.Finding{{Status: model.StatusSkipped, Reason: "fetch budget exhausted", Location: loc}}
		case errors.Is(err, model.ErrBlocked):
			return []model.Finding{{Status: model.StatusSkipped, Reason: "blocked", Location: loc}}
		case errors.Is(err, model.ErrRobotsDisallowed):
			return []model.Finding{{Status: model.StatusSkipped, Reason: "disallowed by robots.txt", Location: loc}}
		case err != nil || pd.Status >= 400:
			return []model.Finding{{
				Status: model.StatusInfo, Location: loc,
				Observed: "no llms.txt found — adoption is still early and inconsistent industry-wide (see llmstxt.org); not a defect",
			}}
		}

		observed := "llms.txt present"
		if title, ok := firstH1(pd.Body); ok {
			observed += ", with the required H1 title: " + truncate(title, 80)
		} else {
			observed += ", but missing the one required element: an H1 title as the first significant line"
		}
		observed += "; " + llmsFullNote(cc, root)

		return []model.Finding{{Status: model.StatusInfo, Location: loc, Observed: observed}}
	}}
}

// firstH1 finds the first significant (non-blank) line and reports whether it
// is a markdown H1 ("# Title"), stripping a leading UTF-8 BOM if present, per
// the spec's own grammar.
func firstH1(body []byte) (string, bool) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	body = bytes.TrimPrefix(body, bom)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if title, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(title), true
		}
		return "", false // first significant line exists but isn't an H1
	}
	return "", false // file is empty/whitespace-only
}

// llmsFullNote checks for the widely-observed (if not formally spec'd)
// llms-full.txt companion file, reporting its presence as a bonus note rather
// than a second finding.
func llmsFullNote(cc model.CheckContext, root string) string {
	pd, err := cc.Fetch(context.Background(), root+"/llms-full.txt")
	if err == nil && pd.Status >= 200 && pd.Status < 300 {
		return "llms-full.txt also present"
	}
	return "llms-full.txt not found (a widely-observed convention, not part of the formal spec)"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
