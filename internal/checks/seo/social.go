package seo

import (
	"fmt"
	"strings"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// openGraphCore / twitterCardCore are the tags treated as "complete" per v0.4
// §5d ("Open Graph tags complete" / "Twitter Card tags complete").
var openGraphCore = []string{"og:title", "og:description", "og:image", "og:url"}
var twitterCardCore = []string{"twitter:card", "twitter:title", "twitter:description", "twitter:image"}

// OpenGraph returns the seo-open-graph check.
func OpenGraph() model.Check {
	return pageCheck{id: "seo-open-graph", title: "Open Graph tags complete", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		return propertyCompleteness(doc, "property", openGraphCore, "Open Graph")
	}}
}

// TwitterCard returns the seo-twitter-card check.
func TwitterCard() model.Check {
	return pageCheck{id: "seo-twitter-card", title: "Twitter Card tags complete", run: func(doc *goquery.Document, _ model.Location) model.Finding {
		return propertyCompleteness(doc, "name", twitterCardCore, "Twitter Card")
	}}
}

// propertyCompleteness checks a set of <meta attr="key" content="..."> tags for
// completeness. attr is "property" for Open Graph, "name" for Twitter Card.
func propertyCompleteness(doc *goquery.Document, attr string, core []string, label string) model.Finding {
	var missing []string
	for _, key := range core {
		v, _ := doc.Find(fmt.Sprintf(`meta[%s="%s"]`, attr, key)).First().Attr("content")
		if strings.TrimSpace(v) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fail(
			fmt.Sprintf("%s tag(s) missing: %s", label, strings.Join(missing, ", ")),
			fmt.Sprintf("all of: %s", strings.Join(core, ", ")),
			fmt.Sprintf("Add the missing %s meta tag(s) so shared links render a rich preview.", label),
		)
	}
	return pass()
}
