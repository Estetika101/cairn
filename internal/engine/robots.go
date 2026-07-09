package engine

import (
	"strings"
)

// robotsRules holds the Disallow/Allow rules for the group that matched our
// user-agent (or the "*" fallback). Decision is longest-match-wins, with Allow
// beating Disallow on an equal-length tie — the standard resolution.
type robotsRules struct {
	rules []robotRule
}

type robotRule struct {
	allow bool
	path  string
}

// allowed reports whether path may be fetched. Empty rule set => allow all.
func (r *robotsRules) allowed(path string) bool {
	if path == "" {
		path = "/"
	}
	bestLen := -1
	bestAllow := true
	for _, rule := range r.rules {
		if rule.path == "" || !strings.HasPrefix(path, rule.path) {
			continue
		}
		switch {
		case len(rule.path) > bestLen:
			bestLen = len(rule.path)
			bestAllow = rule.allow
		case len(rule.path) == bestLen && rule.allow:
			bestAllow = true // Allow wins ties
		}
	}
	if bestLen == -1 {
		return true
	}
	return bestAllow
}

// parseRobots extracts the rule group applicable to uaToken (a product token
// like "cairn"). A group headed by one or more User-agent lines applies to those
// agents; the most specific matching group wins, falling back to "*".
func parseRobots(body []byte, uaToken string) *robotsRules {
	uaToken = strings.ToLower(uaToken)

	type group struct {
		agents []string
		rules  []robotRule
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
			cur.rules = append(cur.rules, robotRule{allow: key == "allow", path: val})
		}
	}

	var star, specific *robotsRules
	for i := range groups {
		g := &groups[i]
		rr := &robotsRules{rules: g.rules}
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
	return &robotsRules{}
}

// uaToken extracts the product token from a full User-Agent string, e.g.
// "cairn/0.1 (+https://…)" -> "cairn".
func uaToken(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "*"
	}
	if i := strings.IndexAny(ua, "/ "); i >= 0 {
		return ua[:i]
	}
	return ua
}
