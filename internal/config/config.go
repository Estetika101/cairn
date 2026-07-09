// Package config loads, defaults, and validates cairn's YAML config. Both the
// file and (later) the web setup form write the same YAML; this loader is the
// single validator behind every on-ramp, so a bad hand-edit fails loudly here
// rather than crashing mid-run (v0.4 §6c).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/Estetika101/cairn/internal/model"
	"gopkg.in/yaml.v3"
)

// Config is the parsed cairn.yaml. It is a superset-tolerant view: unknown keys
// are ignored, absent keys take defaults.
type Config struct {
	Sites         []SiteConfig        `yaml:"sites"`
	Checks        map[string]bool     `yaml:"checks"` // keys are module names OR check IDs
	Accessibility AccessibilityConfig `yaml:"accessibility"`
	FailOn        string              `yaml:"failOn"`
	Output        OutputConfig        `yaml:"output"`
	Crawl         model.CrawlConfig   `yaml:"crawl"`
	Links         LinksConfig         `yaml:"links"`
	Tier2         Tier2Config         `yaml:"tier2"`
	Plugins       []string            `yaml:"plugins"`
}

type SiteConfig struct {
	Name       string `yaml:"name"`
	URL        string `yaml:"url"`
	CrawlLimit int    `yaml:"crawlLimit"` // 0 = single page, N = up to N internal pages
}

type AccessibilityConfig struct {
	WCAGVersion string `yaml:"wcagVersion"`
	WCAGLevel   string `yaml:"wcagLevel"`
}

type OutputConfig struct {
	Formats []string `yaml:"formats"`
	OutDir  string   `yaml:"outDir"`
}

type LinksConfig struct {
	CheckExternal bool `yaml:"checkExternal"`
}

type Tier2Config struct {
	Mode       string `yaml:"mode"` // auto | require | off (parsed; unused in the slice)
	ChromePath string `yaml:"chromePath"`
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
			OutDir:  "./cairn-report",
		},
		Crawl: model.CrawlConfig{
			RequestTimeoutMs:      10000,
			UserAgent:             "cairn/0.1 (+https://github.com/Estetika101/cairn)",
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
