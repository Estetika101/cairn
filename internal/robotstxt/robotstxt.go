// Package robotstxt parses robots.txt group/rule syntax. It is deliberately a
// pure function of bytes in, rules out — no network, no caching — so it can
// be shared by two very different callers without creating a layering
// inversion: the engine's own politeness gate (one call, for the configured
// crawl User-Agent) and the GEO module's bot-posture check (many calls, one
// per AI-crawler product token, against the same fetched robots.txt body).
// Duplicating this logic per caller was considered and rejected — unlike a
// few lines of URL normalization, group-parsing with longest-match-wins and
// Allow-beats-Disallow-on-tie is real logic a copy could drift out of sync
// with.
package robotstxt

import "strings"

// Rules holds the Disallow/Allow rules for the group that matched a given
// user-agent (or the "*" fallback). Decision is longest-match-wins, with
// Allow beating Disallow on an equal-length tie — the standard resolution.
type Rules struct {
	rules []rule
}

type rule struct {
	allow bool
	path  string
}

// Allowed reports whether path may be fetched. An empty rule set allows all.
func (r *Rules) Allowed(path string) bool {
	if path == "" {
		path = "/"
	}
	bestLen := -1
	bestAllow := true
	for _, ru := range r.rules {
		if ru.path == "" || !strings.HasPrefix(path, ru.path) {
			continue
		}
		switch {
		case len(ru.path) > bestLen:
			bestLen = len(ru.path)
			bestAllow = ru.allow
		case len(ru.path) == bestLen && ru.allow:
			bestAllow = true // Allow wins ties
		}
	}
	if bestLen == -1 {
		return true
	}
	return bestAllow
}

// Parse extracts the rule group applicable to uaToken (a product token like
// "verdict" or "GPTBot") from a fetched robots.txt body. A group headed by one
// or more User-agent lines applies to those agents; the most specific
// matching group wins, falling back to "*".
func Parse(body []byte, uaToken string) *Rules {
	uaToken = strings.ToLower(uaToken)

	type group struct {
		agents []string
		rules  []rule
	}
	var groups []group
	var cur *group
	sawRuleSinceAgent := false

	for _, raw := range strings.Split(string(body), "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "user-agent":
			// A User-agent after rules starts a fresh group.
			if cur == nil || sawRuleSinceAgent {
				groups = append(groups, group{})
				cur = &groups[len(groups)-1]
				sawRuleSinceAgent = false
			}
			cur.agents = append(cur.agents, strings.ToLower(val))
		case "disallow", "allow":
			if cur == nil {
				continue
			}
			sawRuleSinceAgent = true
			cur.rules = append(cur.rules, rule{allow: key == "allow", path: val})
		}
	}

	var star, specific *Rules
	for i := range groups {
		g := &groups[i]
		rr := &Rules{rules: g.rules}
		for _, a := range g.agents {
			if a == "*" && star == nil {
				star = rr
			}
			if a != "*" && strings.Contains(uaToken, a) {
				specific = rr
			}
		}
	}
	if specific != nil {
		return specific
	}
	if star != nil {
		return star
	}
	return &Rules{}
}

// ProductToken extracts the product token from a full User-Agent string, e.g.
// "verdict/0.1 (+https://…)" -> "verdict".
func ProductToken(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "*"
	}
	if i := strings.IndexAny(ua, "/ "); i >= 0 {
		return ua[:i]
	}
	return ua
}
