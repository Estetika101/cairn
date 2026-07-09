// Package model holds the shared value types for cairn. It has no logic and
// imports nothing from other internal packages, so engine, checks, plugin, and
// report can all depend on it without cycles.
package model

// Status records what happened when a check ran. It is a separate axis from
// Severity: a finding only carries a Severity when Status == StatusFail.
type Status string

const (
	StatusFail    Status = "fail"    // ran, target violated the criterion
	StatusPass    Status = "pass"    // ran, target satisfied it (proves "checked, clean")
	StatusSkipped Status = "skipped" // could not run; carries a Reason
	StatusInfo    Status = "info"    // ran, result not scoreable (never a defect)
	StatusManual  Status = "manual"  // reserved, unused in the slice
)

// Severity ranks how bad a fail is. Only meaningful when Status == StatusFail.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Scope declares when a check runs: once per crawled page, or once per site.
type Scope string

const (
	ScopePage Scope = "page"
	ScopeSite Scope = "site"
)

// Location points a finding at what's wrong. For a site-scoped finding the
// identity is the target URL; AffectedURLs lists the referrer pages and is
// deliberately excluded from the content-hash ID (see engine/id.go).
type Location struct {
	URL          string   `json:"url"`
	Selector     string   `json:"selector,omitempty"`
	AffectedURLs []string `json:"affectedUrls,omitempty"`
	File         *string  `json:"file,omitempty"` // null in the slice; live-URL audits only
	Line         *int     `json:"line,omitempty"`
}

// Finding is the single source of truth every output format derives from.
type Finding struct {
	ID           string   `json:"id"` // content hash, assigned by engine/id.go
	Module       string   `json:"module"`
	Scope        Scope    `json:"scope"`
	Criterion    string   `json:"criterion,omitempty"`
	Status       Status   `json:"status"`
	Severity     Severity `json:"severity,omitempty"` // omitted unless Status == StatusFail
	Reason       string   `json:"reason,omitempty"`   // required when Status == StatusSkipped
	Location     Location `json:"location"`
	Observed     string   `json:"observed,omitempty"`
	Required     string   `json:"required,omitempty"`
	SuggestedFix string   `json:"suggestedFix,omitempty"`
	// autoFixable / effort are RESERVED and intentionally not emitted (v0.4 §6b).
}
