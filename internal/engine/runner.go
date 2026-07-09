package engine

import (
	"context"
	"sort"

	"github.com/Estetika101/cairn/internal/model"
)

// SiteTarget is the per-site input to a run.
type SiteTarget struct {
	Name       string
	URL        string
	CrawlLimit int
}

// RunSite crawls one site and runs the given checks over it, returning a
// SiteReport. Page-scoped checks run once per crawled page; site-scoped checks
// run once over the whole corpus (site path lands in M4). Findings get stable
// content-hash IDs and a deterministic sort so output is reproducible.
func RunSite(ctx context.Context, crawlCfg model.CrawlConfig, target SiteTarget, checks []model.Check, checkCfg model.CheckConfig) (model.SiteReport, error) {
	f := NewFetcher(crawlCfg)

	corpus, err := crawl(ctx, f, target.URL, target.CrawlLimit)
	if err != nil {
		return model.SiteReport{}, err
	}

	var pageChecks, siteChecks []model.Check
	for _, c := range checks {
		if c.Meta().Scope == model.ScopeSite {
			siteChecks = append(siteChecks, c)
		} else {
			pageChecks = append(pageChecks, c)
		}
	}

	var findings []model.Finding

	for _, page := range corpus {
		for _, c := range pageChecks {
			cc := &checkCtx{scope: model.ScopePage, page: page, fetcher: f, checkCfg: checkCfg}
			fs, cerr := c.Run(cc)
			if cerr != nil {
				continue // a broken check is not a broken run; M5 formalizes skipped-on-error
			}
			findings = append(findings, fs...)
		}
	}

	if len(siteChecks) > 0 {
		cc := &checkCtx{scope: model.ScopeSite, corpus: corpus, fetcher: f, checkCfg: checkCfg}
		for _, c := range siteChecks {
			fs, cerr := c.Run(cc)
			if cerr != nil {
				continue
			}
			findings = append(findings, fs...)
		}
	}

	AssignIDs(findings)
	SortFindings(findings)

	return model.SiteReport{
		Name:     target.Name,
		URL:      target.URL,
		Summary:  model.Summarize(findings),
		Findings: findings,
	}, nil
}

// SortFindings gives a deterministic order independent of crawl order: by
// severity rank (errors first), then module, then ID.
func SortFindings(fs []model.Finding) {
	rank := func(f model.Finding) int {
		switch f.Severity {
		case model.SeverityError:
			return 0
		case model.SeverityWarn:
			return 1
		default:
			return 2 // pass/info/skipped carry no severity
		}
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if ri, rj := rank(fs[i]), rank(fs[j]); ri != rj {
			return ri < rj
		}
		if fs[i].Module != fs[j].Module {
			return fs[i].Module < fs[j].Module
		}
		return fs[i].ID < fs[j].ID
	})
}
