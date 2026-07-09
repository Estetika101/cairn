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
	"github.com/Estetika101/cairn/internal/dashboard"
	"github.com/Estetika101/cairn/internal/engine"
	"github.com/Estetika101/cairn/internal/model"
	"github.com/Estetika101/cairn/internal/plugin"
	"github.com/Estetika101/cairn/internal/report"
)

// version is the tool's own release version, distinct from the report schema.
const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches on the command grammar (v0.4 §4b): `audit` is the default
// verb, so a bare `cairn --config x.yaml` is an alias for `cairn audit
// --config x.yaml`. `serve` is the only other verb implemented so far.
func run(args []string, stdout, stderr *os.File) int {
	if len(args) > 0 && args[0] == "serve" {
		return runServe(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "audit" {
		args = args[1:]
	}
	return runAudit(args, stdout, stderr)
}

func runAudit(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("cairn audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "cairn.yaml", "path to the config file")
	outDir := fs.String("out", "", "override output.outDir")
	serve := fs.Bool("serve", false, "after auditing, start the local dashboard on the result")
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

	ctx := context.Background()

	// Filter built-in checks by config (module or check-ID enable).
	var enabled []model.Check
	for _, c := range checks.Builtins() {
		m := c.Meta()
		if cfg.CheckEnabled(m.Module, m.ID) {
			enabled = append(enabled, c)
		}
	}

	// Load WASM plugins named in config; they register as ordinary checks.
	for _, path := range cfg.Plugins {
		p, perr := plugin.Load(ctx, path)
		if perr != nil {
			fmt.Fprintf(stderr, "cairn: %v\n", perr)
			return 2
		}
		defer p.Close(ctx)
		m := p.Meta()
		if cfg.CheckEnabled(m.Module, m.ID) {
			enabled = append(enabled, p)
		}
	}

	rep := model.Report{
		SchemaVersion: "1.0.0-draft",
		Tool:          model.ToolInfo{Name: "cairn", Version: version},
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Sites run sequentially in the slice (siteConcurrency > 1 is future work).
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

	formats := cfg.Output.Formats
	if *serve && !contains(formats, "json") {
		// The dashboard reads report.json; --serve without a json format
		// configured would otherwise start a server with nothing to show.
		formats = append(append([]string{}, formats...), "json")
	}
	if err := report.Emit(rep, formats, cfg.Output.OutDir, stdout, isTerminal(stdout)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	code := report.ExitCode(rep, cfg.FailOn)
	if !*serve {
		return code
	}

	addr := fmt.Sprintf("%s:%d", cfg.Serve.Host, cfg.Serve.Port)
	fmt.Fprintf(stdout, "cairn: exit code would be %d; serving results at http://%s (Ctrl+C to stop)\n", code, addr)
	if err := dashboard.ListenAndServe(addr, cfg.Output.OutDir); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// runServe starts the dashboard against an already-written report directory
// (no audit runs here — `cairn audit --serve` is for that). --report wins if
// both --config and --report are given; otherwise the directory is derived
// from --config's output.outDir, defaulting to ./cairn-report.
func runServe(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("cairn serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "config file to derive the report directory and bind address from")
	reportDir := fs.String("report", "", "directory containing report.json (default: ./cairn-report, or output.outDir from --config)")
	host := fs.String("host", "", "bind address (default 127.0.0.1, or serve.host from --config)")
	port := fs.Int("port", 0, "port (default 8787, or serve.port from --config)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dir := *reportDir
	bindHost, bindPort := "127.0.0.1", 8787

	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if dir == "" {
			dir = cfg.Output.OutDir
		}
		bindHost, bindPort = cfg.Serve.Host, cfg.Serve.Port
	}
	if dir == "" {
		dir = "./cairn-report"
	}
	if *host != "" {
		bindHost = *host
	}
	if *port != 0 {
		bindPort = *port
	}

	addr := fmt.Sprintf("%s:%d", bindHost, bindPort)
	fmt.Fprintf(stdout, "cairn: serving %s at http://%s (Ctrl+C to stop)\n", dir, addr)
	if err := dashboard.ListenAndServe(addr, dir); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
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
