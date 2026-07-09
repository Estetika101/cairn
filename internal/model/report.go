package model

// Report is the top-level JSON envelope. It is the source of truth; console,
// tasks, and any other format are derived from the same findings, never
// recomputed (v0.4 §6b).
type Report struct {
	SchemaVersion string       `json:"schemaVersion"`
	Tool          ToolInfo     `json:"tool"`
	GeneratedAt   string       `json:"generatedAt"` // RFC3339; injectable for golden tests
	Sites         []SiteReport `json:"sites"`
}

type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SiteReport is one entry per configured site — one report, one array, whether
// you configured one site or twenty.
type SiteReport struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Summary  Summary   `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Summary counts findings by Status, computed from the findings array so it
// can never drift from a parallel bookkeeping path.
type Summary struct {
	Fail    int `json:"fail"`
	Pass    int `json:"pass"`
	Skipped int `json:"skipped"`
	Info    int `json:"info"`
}

// Summarize tallies findings by status.
func Summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Status {
		case StatusFail:
			s.Fail++
		case StatusPass:
			s.Pass++
		case StatusSkipped:
			s.Skipped++
		case StatusInfo:
			s.Info++
		}
	}
	return s
}
