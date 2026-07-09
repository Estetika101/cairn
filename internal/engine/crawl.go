package engine

import (
	"context"
	"net/url"
	"strings"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/PuerkitoBio/goquery"
)

// crawl does a breadth-first walk from seed, following only internal (same-host)
// links, until it has collected up to `limit` pages. A crawlLimit of 0 means the
// single seed page. Non-HTML and non-2xx pages are still included in the corpus
// (checks decide what to do with them) but are not expanded for further links.
func crawl(ctx context.Context, f *Fetcher, seed string, limit int) ([]model.PageData, error) {
	if limit < 1 {
		limit = 1
	}
	seedURL, err := url.Parse(seed)
	if err != nil {
		return nil, err
	}
	seedHost := strings.ToLower(seedURL.Host)

	var corpus []model.PageData
	seen := map[string]bool{}
	queue := []string{seed}

	for len(queue) > 0 && len(corpus) < limit {
		raw := queue[0]
		queue = queue[1:]

		canon, _, cerr := canonicalize(raw)
		if cerr != nil || seen[canon] {
			continue
		}
		seen[canon] = true

		pd, ferr := f.Fetch(ctx, raw, CrawlFetch)
		if ferr != nil {
			// A page that can't be fetched (robots, blocked, transport) is not
			// added to the corpus; the crawl simply doesn't reach it. Link
			// liveness of such targets is the broken-links check's job, not the
			// crawl's.
			continue
		}
		corpus = append(corpus, pd)

		if pd.Doc == nil {
			continue
		}
		pd.Doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, ok := s.Attr("href")
			if !ok {
				return
			}
			next := resolveInternal(seedURL, seedHost, href)
			if next != "" {
				queue = append(queue, next)
			}
		})
	}
	return corpus, nil
}

// resolveInternal resolves href against base and returns an absolute URL only if
// it is an http(s) link on the same host; otherwise "".
func resolveInternal(base *url.URL, host, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := base.Parse(href)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if strings.ToLower(u.Host) != host {
		return ""
	}
	u.Fragment = ""
	return u.String()
}
