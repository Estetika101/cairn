// Package config loads, defaults, and validates verdict's YAML config. Both the
// file and (later) the web setup form write the same YAML; this loader is the
// single validator behind every on-ramp, so a bad hand-edit fails loudly here
// rather than crashing mid-run (v0.4 §6c).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
	"gopkg.in/yaml.v3"
)

// Config is the parsed verdict.yaml. It is a superset-tolerant view: unknown keys
// are ignored, absent keys take defaults. JSON tags (matching, lowercase
// camelCase) let the dashboard's config-editing API round-trip this same
// struct — GET returns it, a form edits a subset, POST merges back onto a
// freshly loaded copy so untouched sections (crawl/tier2/plugins/serve) are
// never blanked out (v0.4 §6c).
type Config struct {
	Sites         []SiteConfig        `yaml:"sites" json:"sites"`
	Checks        map[string]bool     `yaml:"checks" json:"checks"` // keys are module names OR check IDs
	Accessibility AccessibilityConfig `yaml:"accessibility" json:"accessibility"`
	FailOn        string              `yaml:"failOn" json:"failOn"`
	Output        OutputConfig        `yaml:"output" json:"output"`
	Crawl         model.CrawlConfig   `yaml:"crawl" json:"crawl"`
	Links         LinksConfig         `yaml:"links" json:"links"`
	Tier2         Tier2Config         `yaml:"tier2" json:"tier2"`
	Plugins       []string            `yaml:"plugins" json:"plugins"`
	Serve         ServeConfig         `yaml:"serve" json:"serve"`
}

type SiteConfig struct {
	Name       string `yaml:"name" json:"name"`
	URL        string `yaml:"url" json:"url"`
	CrawlLimit int    `yaml:"crawlLimit" json:"crawlLimit"` // 0 = single page, N = up to N internal pages
}

type AccessibilityConfig struct {
	WCAGVersion string `yaml:"wcagVersion" json:"wcagVersion"`
	WCAGLevel   string `yaml:"wcagLevel" json:"wcagLevel"`
}

type OutputConfig struct {
	Formats []string `yaml:"formats" json:"formats"`
	OutDir  string   `yaml:"outDir" json:"outDir"`
}

type LinksConfig struct {
	CheckExternal bool `yaml:"checkExternal" json:"checkExternal"`
}

type Tier2Config struct {
	Mode       string `yaml:"mode" json:"mode"` // auto | require | off (parsed; unused in the slice)
	ChromePath string `yaml:"chromePath" json:"chromePath"`
}

// ServeConfig governs `verdict serve` / `verdict audit --serve` (v0.4 §6c). Only
// Host/Port are consumed today; Interval and AllowRemoteConfig are reserved for
// `verdict watch`, not yet built — schema surface for a capability the tool
// doesn't have yet, same pattern as autoFixable/effort. AllowRemoteConfig DOES
// have real teeth now: it gates the dashboard's config-write/audit-trigger
// endpoints, which stay localhost-only unless this is explicitly true.
type ServeConfig struct {
	Host              string `yaml:"host" json:"host"`
	Port              int    `yaml:"port" json:"port"`
	Interval          string `yaml:"interval" json:"interval"`                   // reserved: verdict watch re-audit cadence
	AllowRemoteConfig bool   `yaml:"allowRemoteConfig" json:"allowRemoteConfig"` // see doc comment above
}

// Defaults returns a Config populated with v0.4 §4 defaults. Load unmarshals the
// file over this, so absent keys keep these values.
func Defaults() Config {
	return Config{
		Checks:        nil, // nil => everything enabled unless explicitly disabled (CheckEnabled)
		Accessibility: AccessibilityConfig{WCAGVersion: "2.2", WCAGLevel: "AA"},
		FailOn:        "error",
		Output: OutputConfig{
			Formats: []string{"console", "markdown", "json", "tasks"},
			OutDir:  "./verdict-report",
		},
		Crawl: model.CrawlConfig{
			RequestTimeoutMs:      10000,
			UserAgent:             "verdict/0.1 (+https://github.com/Estetika101/verdict)",
			MaxRetries:            2,
			RetryAfterCapMs:       30000,
			MaxConcurrentRequests: 8,
			MaxExtraFetches:       500,
			SiteConcurrency:       1,
			RespectRobots:         true,
			PerHost:               model.PerHostConfig{Concurrency: 2, DelayMs: 250},
		},
		Links: LinksConfig{CheckExternal: false},
		Tier2: Tier2Config{Mode: "auto"},
		Serve: ServeConfig{Host: "127.0.0.1", Port: 8787, Interval: "15m"},
	}
}

// Load reads, defaults, and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return Parse(data)
}

// Parse defaults, unmarshals, and validates raw YAML bytes.
func Parse(data []byte) (*Config, error) {
	cfg := Defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: invalid YAML: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate re-runs the same checks Parse applies. It's exported so the
// dashboard's config-editing endpoint (v0.4 §6c) can validate an in-memory
// struct assembled from a base config merged with form edits, without a
// round-trip through YAML text — one validator behind both on-ramps.
func (c *Config) Validate() error { return c.validate() }

func (c *Config) validate() error {
	if len(c.Sites) == 0 {
		return fmt.Errorf("config: sites: at least one site is required")
	}
	for i := range c.Sites {
		s := &c.Sites[i]
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("config: sites[%d].url: required", i)
		}
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("config: sites[%d].url %q: must be an absolute http(s) URL", i, s.URL)
		}
		if s.Name == "" {
			s.Name = u.Host
		}
		if s.CrawlLimit < 0 {
			return fmt.Errorf("config: sites[%d].crawlLimit %d: must be >= 0", i, s.CrawlLimit)
		}
	}

	switch c.FailOn {
	case "off", "warn", "error":
	default:
		return fmt.Errorf("config: failOn %q: must be off, warn, or error", c.FailOn)
	}

	switch c.Accessibility.WCAGVersion {
	case "2.0", "2.1", "2.2":
	default:
		return fmt.Errorf("config: accessibility.wcagVersion %q: must be 2.0, 2.1, or 2.2", c.Accessibility.WCAGVersion)
	}
	switch c.Accessibility.WCAGLevel {
	case "A", "AA", "AAA":
	default:
		return fmt.Errorf("config: accessibility.wcagLevel %q: must be A, AA, or AAA", c.Accessibility.WCAGLevel)
	}

	switch c.Tier2.Mode {
	case "auto", "require", "off":
	default:
		return fmt.Errorf("config: tier2.mode %q: must be auto, require, or off", c.Tier2.Mode)
	}

	if c.Serve.Host == "" {
		return fmt.Errorf("config: serve.host: required")
	}
	if c.Serve.Port < 1 || c.Serve.Port > 65535 {
		return fmt.Errorf("config: serve.port %d: must be 1-65535", c.Serve.Port)
	}

	allowedFormats := map[string]bool{"console": true, "markdown": true, "json": true, "tasks": true}
	for _, f := range c.Output.Formats {
		if !allowedFormats[f] {
			return fmt.Errorf("config: output.formats: unknown format %q (allowed: console, markdown, json, tasks)", f)
		}
	}
	if c.Output.OutDir == "" {
		return fmt.Errorf("config: output.outDir: required")
	}

	if c.Crawl.MaxExtraFetches < 0 {
		return fmt.Errorf("config: crawl.maxExtraFetches %d: must be >= 0", c.Crawl.MaxExtraFetches)
	}
	if c.Crawl.MaxConcurrentRequests < 1 {
		return fmt.Errorf("config: crawl.maxConcurrentRequests %d: must be >= 1", c.Crawl.MaxConcurrentRequests)
	}
	if c.Crawl.SiteConcurrency < 1 {
		return fmt.Errorf("config: crawl.siteConcurrency %d: must be >= 1", c.Crawl.SiteConcurrency)
	}
	if c.Crawl.PerHost.Concurrency < 1 {
		return fmt.Errorf("config: crawl.perHost.concurrency %d: must be >= 1", c.Crawl.PerHost.Concurrency)
	}
	if c.Crawl.PerHost.DelayMs < 0 {
		return fmt.Errorf("config: crawl.perHost.delayMs %d: must be >= 0", c.Crawl.PerHost.DelayMs)
	}
	if c.Crawl.RequestTimeoutMs < 1 {
		return fmt.Errorf("config: crawl.requestTimeoutMs %d: must be >= 1", c.Crawl.RequestTimeoutMs)
	}
	return nil
}

// CheckEnabled reports whether a check should run. Most-specific wins: an entry
// keyed on the check ID overrides one keyed on the module; absent => enabled.
func (c *Config) CheckEnabled(module, id string) bool {
	if c.Checks == nil {
		return true
	}
	if v, ok := c.Checks[id]; ok {
		return v
	}
	if v, ok := c.Checks[module]; ok {
		return v
	}
	return true
}

// CheckConfig builds the read-only view handed to checks via CheckContext.
func (c *Config) CheckConfig() model.CheckConfig {
	return model.CheckConfig{
		WCAGVersion:   c.Accessibility.WCAGVersion,
		WCAGLevel:     c.Accessibility.WCAGLevel,
		CheckExternal: c.Links.CheckExternal,
	}
}
