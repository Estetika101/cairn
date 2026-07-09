package model

import (
	"context"
	"errors"
)

// CheckMeta is the registry metadata for a check. Built-ins and plugins expose
// the same shape — there is no privileged built-in path in the registry.
type CheckMeta struct {
	ID           string   // stable, e.g. "security-headers", "broken-links"
	Module       string   // "security", "links", ...
	Tier         int      // 1 in the slice (no Tier 2 yet)
	Scope        Scope    // ScopePage | ScopeSite
	Severity     Severity // DEFAULT severity for this check's fails (the fallback; v0.4 §6b)
	Title        string
	WCAGCriteria []string // optional; empty for the slice's two checks
}

// Check is implemented identically by built-ins and (via the host adapter) by
// WASM plugins.
type Check interface {
	Meta() CheckMeta
	Run(cc CheckContext) ([]Finding, error)
}

// CheckContext is what both a built-in and a WASM guest see. Fetch is the ONLY
// network path a check is given.
type CheckContext interface {
	Scope() Scope
	Page() PageData                                          // valid when Scope() == ScopePage
	Corpus() []PageData                                      // valid when Scope() == ScopeSite
	Fetch(ctx context.Context, url string) (PageData, error) // engine-owned
	Config() CheckConfig                                     // read-only view
	Logf(format string, args ...any)
}

// Typed Fetch errors so a check can map them to the right skipped Reason rather
// than a generic failure. Transport/timeout errors surface as ordinary errors
// and the check decides (usually a fail).
var (
	ErrRobotsDisallowed = errors.New("disallowed by robots.txt")       // -> skipped
	ErrBudgetExhausted  = errors.New("fetch budget exhausted")         // -> skipped
	ErrBlocked          = errors.New("bot-protection / WAF challenge") // -> skipped (v0.4 §5c)
)
