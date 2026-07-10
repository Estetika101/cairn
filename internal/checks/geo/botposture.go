// Package geo implements the GEO (generative-engine optimization) check
// module: AI-crawler posture, llms.txt, and date-metadata/freshness signals.
// Per v0.4 §5, these are strategic/informational rather than scored — allow
// or block of an AI crawler is a business decision the tool has no basis to
// grade, and llms.txt adoption is too new and inconsistent to treat absence
// as a defect. The one exception is date metadata: its *presence* is a clean,
// scoreable binary (v0.4 §5's "scored normally" item); its *age* is reported
// as a measured fact, not judged against an invented freshness threshold —
// see botposture.go's package doc and freshness.go for why that split matters.
package geo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
	"github.com/Estetika101/verdict/internal/robotstxt"
)

// botDef is one AI-crawler product token and what allowing/blocking it means.
// Current as of the 2026 crawler landscape researched for this module: each
// major vendor now runs 2-3 *distinct* bots with different purposes, not one
// bot each — the older "GPTBot/ClaudeBot/CCBot" flat model doesn't capture
// that a site can allow Claude-User (so Claude can fetch a page a user
// explicitly asks about) while blocking ClaudeBot (opt out of training).
type botDef struct {
	ua      string
	purpose string
}

type vendor struct {
	name string
	bots []botDef
}

// aiCrawlers is the reported vendor/bot table. Google-Extended and
// Applebot-Extended are deliberately labeled as opt-out tokens, not crawlers:
// there is no separate bot behind them, so "disallowed" doesn't stop Googlebot
// or Applebot from crawling for search — it only opts the already-crawled
// content out of that vendor's AI-training use. Getting this distinction wrong
// in the report would mislead a site owner into thinking they'd blocked a
// crawl that never stopped.
var aiCrawlers = []vendor{
	{"OpenAI", []botDef{
		{"GPTBot", "training"},
		{"OAI-SearchBot", "search citations"},
		{"ChatGPT-User", "user-triggered fetch"},
	}},
	{"Anthropic", []botDef{
		{"ClaudeBot", "training"},
		{"Claude-SearchBot", "search quality"},
		{"Claude-User", "user-triggered fetch"},
	}},
	{"Perplexity", []botDef{
		{"PerplexityBot", "crawl/index"},
		{"Perplexity-User", "user-triggered fetch"},
	}},
	{"Google", []botDef{
		{"Google-Extended", "training opt-out token — not a separate crawler; Googlebot's own crawl is unaffected"},
	}},
	{"Apple", []botDef{
		{"Applebot-Extended", "training opt-out token — not a separate crawler; Applebot's own crawl is unaffected"},
	}},
	{"Common Crawl", []botDef{
		{"CCBot", "training dataset many LLMs are built from"},
	}},
}

// BotPosture returns the geo-bot-posture check.
func BotPosture() model.Check {
	return siteCheck{id: "geo-bot-posture", title: "AI crawler access posture (robots.txt)", run: func(cc model.CheckContext) []model.Finding {
		root := siteRoot(cc)
		if root == "" {
			return nil
		}
		loc := model.Location{URL: root + "/robots.txt"}

		pd, err := cc.Fetch(context.Background(), loc.URL)
		switch {
		case errors.Is(err, model.ErrBudgetExhausted):
			return []model.Finding{{Status: model.StatusSkipped, Reason: "fetch budget exhausted", Location: loc}}
		case errors.Is(err, model.ErrBlocked):
			return []model.Finding{{Status: model.StatusSkipped, Reason: "blocked", Location: loc}}
		case errors.Is(err, model.ErrRobotsDisallowed):
			// A site disallowing its own robots.txt to Verdict's own UA is a
			// contradiction in practice, but handled the same honest way as
			// anywhere else: report it, don't guess.
			return []model.Finding{{Status: model.StatusSkipped, Reason: "disallowed by robots.txt", Location: loc}}
		case err != nil || pd.Status >= 400:
			return []model.Finding{{
				Status: model.StatusInfo, Location: loc,
				Observed: "no robots.txt found — all AI crawlers are implicitly allowed by default",
			}}
		}

		var parts []string
		for _, v := range aiCrawlers {
			var botParts []string
			for _, b := range v.bots {
				allowed := robotstxt.Parse(pd.Body, b.ua).Allowed("/")
				verdict := "disallowed"
				if allowed {
					verdict = "allowed"
				}
				botParts = append(botParts, fmt.Sprintf("%s (%s): %s", b.ua, b.purpose, verdict))
			}
			parts = append(parts, v.name+" — "+strings.Join(botParts, ", "))
		}

		return []model.Finding{{
			Status: model.StatusInfo, Location: loc,
			Observed: strings.Join(parts, " | "),
		}}
	}}
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

func pageURL(p model.PageData) string {
	if p.FinalURL != "" {
		return p.FinalURL
	}
	return p.RequestedURL
}

// siteCheck is the common shape for the site-scoped GEO checks.
type siteCheck struct {
	id    string
	title string
	run   func(cc model.CheckContext) []model.Finding
}

func (c siteCheck) Meta() model.CheckMeta {
	return model.CheckMeta{ID: c.id, Module: "geo", Tier: 1, Scope: model.ScopeSite, Title: c.title}
}

func (c siteCheck) Run(cc model.CheckContext) ([]model.Finding, error) {
	fs := c.run(cc)
	for i := range fs {
		fs[i].Module, fs[i].Scope, fs[i].Criterion = "geo", model.ScopeSite, c.id
	}
	return fs, nil
}
