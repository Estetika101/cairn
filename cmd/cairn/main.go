// Command cairn is an open-source, self-hostable web QA auditor.
//
// This is the slice-stage entry point: it parses flags and loads config. The
// audit engine, checks, and reporters are wired in over the subsequent
// milestones (see webqa-SLICE-SPEC §S10).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Estetika101/cairn/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("cairn", flag.ContinueOnError)
	configPath := fs.String("config", "cairn.yaml", "path to the config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// Placeholder until the runner lands (milestones 2–6).
	fmt.Printf("cairn: loaded %d site(s) from %s; failOn=%s\n", len(cfg.Sites), *configPath, cfg.FailOn)
	fmt.Println("cairn: audit engine not yet wired — slice milestone 1 (config) complete.")
	return 0
}
