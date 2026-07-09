package model

// CrawlConfig is the engine-facing crawl/politeness configuration. It lives in
// model (not config) so the engine imports only model. YAML tags let the config
// loader unmarshal straight into it; the tags are inert strings, no dependency.
type CrawlConfig struct {
	RequestTimeoutMs      int           `yaml:"requestTimeoutMs"`
	UserAgent             string        `yaml:"userAgent"`
	MaxRetries            int           `yaml:"maxRetries"`
	RetryAfterCapMs       int           `yaml:"retryAfterCapMs"`
	MaxConcurrentRequests int           `yaml:"maxConcurrentRequests"` // GLOBAL in-flight cap
	MaxExtraFetches       int           `yaml:"maxExtraFetches"`       // PER-SITE Fetch budget
	SiteConcurrency       int           `yaml:"siteConcurrency"`       // sequential when 1
	RespectRobots         bool          `yaml:"respectRobots"`
	PerHost               PerHostConfig `yaml:"perHost"`
}

// PerHostConfig governs how hard any single host is hit — the number that
// matters for a typical single-site audit.
type PerHostConfig struct {
	Concurrency int `yaml:"concurrency"` // simultaneous in-flight requests to one host
	DelayMs     int `yaml:"delayMs"`     // minimum gap between requests to the same host
}

// CheckConfig is the read-only view a check gets via CheckContext.Config().
// Minimal for the slice; grows as modules land.
type CheckConfig struct {
	WCAGVersion   string // "2.0" | "2.1" | "2.2"
	WCAGLevel     string // "A" | "AA" | "AAA"
	CheckExternal bool   // links.checkExternal
}
