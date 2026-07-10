package geo

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Estetika101/verdict/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// dateLayouts covers the date/time formats actually seen in the wild across
// JSON-LD, Open Graph, and <time> elements: full RFC3339, RFC3339 without a
// timezone offset, and a bare date.
var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// Freshness returns the geo-date-freshness check. Presence of publish/modified
// date metadata is a clean binary and is scored (fail/warn if entirely
// absent, per v0.4 §5's "scored normally" framing for this item). Its AGE, if
// present, is reported as a measured fact — "last updated N days ago" — not
// judged against an invented staleness threshold: whether 400 days is "stale"
// depends on the content (a recipe ages very differently than a crawler
// bot-list), and imposing a fixed cadence requirement is exactly the
// unverifiable content-strategy territory this tool's checks don't score.
func Freshness() model.Check {
	return pageCheck{id: "geo-date-freshness", title: "Publish/modified date metadata", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		published, modified := extractDates(doc)

		best := modified
		label := "modified"
		if best == "" {
			best = published
			label = "published"
		}
		if best == "" {
			return model.Finding{
				Status: model.StatusFail, Severity: model.SeverityWarn,
				Observed: "no datePublished/dateModified (JSON-LD), article:published_time/modified_time (OpenGraph), or <time datetime> found",
				Required: "publish or last-modified date metadata, in JSON-LD Article/BlogPosting, OpenGraph, or a <time> element",
				SuggestedFix: "Add dateModified/datePublished to your Article schema, or article:published_time / " +
					"article:modified_time OpenGraph meta tags — AI answer engines weigh recency when selecting sources.",
			}
		}

		t, ok := parseDate(best)
		if !ok {
			return model.Finding{
				Status: model.StatusFail, Severity: model.SeverityWarn,
				Observed: fmt.Sprintf("date metadata present but not a recognizable date: %q", best),
				Required: "an ISO 8601 date (e.g. 2026-07-10 or 2026-07-10T15:04:05Z)",
			}
		}

		days := int(time.Since(t).Hours() / 24)
		observed := fmt.Sprintf("%s %s (%s), %d day(s) ago", label, t.Format("2006-01-02"), best, days)
		if days < 0 {
			observed = fmt.Sprintf("%s date is in the future: %s (%s)", label, t.Format("2006-01-02"), best)
		}
		return model.Finding{Status: model.StatusInfo, Observed: observed}
	}}
}

// extractDates looks for datePublished/dateModified in JSON-LD first (the
// signal Google's own AI Overviews guidance says it actually reads), falling
// back to OpenGraph article: tags, then a <time datetime> element.
func extractDates(doc *goquery.Document) (published, modified string) {
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var v any
		if err := json.Unmarshal([]byte(s.Text()), &v); err != nil {
			return true
		}
		p, m := findDateFields(v)
		if p != "" {
			published = p
		}
		if m != "" {
			modified = m
		}
		return published == "" || modified == "" // stop once we have both
	})
	if published != "" || modified != "" {
		return published, modified
	}

	if v, ok := doc.Find(`meta[property="article:published_time"]`).First().Attr("content"); ok {
		published = v
	}
	if v, ok := doc.Find(`meta[property="article:modified_time"]`).First().Attr("content"); ok {
		modified = v
	}
	if published != "" || modified != "" {
		return published, modified
	}

	if v, ok := doc.Find("time[datetime]").First().Attr("datetime"); ok {
		published = v
	}
	return published, modified
}

// findDateFields recursively walks a decoded JSON-LD value (which may be a
// single object, an array, or an "@graph"-wrapped object) looking for
// datePublished/dateModified string fields.
func findDateFields(v any) (published, modified string) {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["datePublished"].(string); ok && published == "" {
			published = s
		}
		if s, ok := t["dateModified"].(string); ok && modified == "" {
			modified = s
		}
		for _, key := range []string{"@graph"} {
			if child, ok := t[key]; ok {
				p, m := findDateFields(child)
				if published == "" {
					published = p
				}
				if modified == "" {
					modified = m
				}
			}
		}
	case []any:
		for _, item := range t {
			p, m := findDateFields(item)
			if published == "" {
				published = p
			}
			if modified == "" {
				modified = m
			}
			if published != "" && modified != "" {
				break
			}
		}
	}
	return published, modified
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
