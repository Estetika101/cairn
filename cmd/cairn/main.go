// Command cairn is an open-source, self-hostable web QA auditor.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Estetika101/cairn/internal/checks"
	"github.com/Estetika101/cairn/internal/config"
	"github.com/Estetika101/cairn/internal/engine"
	"github.com/Estetika101/cairn/internal/model"
	"github.com/Estetika101/cairn/internal/report"
)

// version is the tool's own release version, distinct from the report schema.
const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("cairn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "cairn.yaml", "path to the config file")
	outDir := fs.String("out", "", "override output.outDir")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *outDir != "" {
		cfg.Output.OutDir = *outDir
	}

	// Filter built-in checks by config (module or check-ID enable).
	var enabled []model.Check
	for _, c := range checks.Builtins() {
		m := c.Meta()
		if cfg.CheckEnabled(m.Module, m.ID) {
			enabled = append(enabled, c)
		}
	}

	rep := model.Report{
		SchemaVersion: "1.0.0-draft",
		Tool:          model.ToolInfo{Name: "cairn", Version: version},
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Sites run sequentially in the slice (siteConcurrency > 1 is future work).
	ctx := context.Background()
	for _, site := range cfg.Sites {
		sr, rerr := engine.RunSite(ctx, cfg.Crawl, engine.SiteTarget{
			Name:       site.Name,
			URL:        site.URL,
			CrawlLimit: site.CrawlLimit,
		}, enabled, cfg.CheckConfig())
		if rerr != nil {
			fmt.Fprintf(stderr, "cairn: %s: %v\n", site.URL, rerr)
			return 2
		}
		rep.Sites = append(rep.Sites, sr)
	}

	if err := report.Emit(rep, cfg.Output.Formats, cfg.Output.OutDir, stdout, isTerminal(stdout)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	return report.ExitCode(rep, cfg.FailOn)
}

// isTerminal reports whether w is an interactive terminal and color is allowed.
func isTerminal(w *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := w.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
