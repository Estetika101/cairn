package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/Estetika101/cairn/internal/model"
)

// ansi color codes, used only when color is enabled (a TTY, NO_COLOR unset).
type palette struct{ red, yellow, green, dim, bold, reset string }

func newPalette(color bool) palette {
	if !color {
		return palette{}
	}
	return palette{
		red:    "\033[31m",
		yellow: "\033[33m",
		green:  "\033[32m",
		dim:    "\033[2m",
		bold:   "\033[1m",
		reset:  "\033[0m",
	}
}

// WriteConsole prints a human summary grouped by module, with a non-buried
// "N checks skipped" banner at the top when anything was skipped (v0.4 §7c).
func WriteConsole(w io.Writer, rep model.Report, color bool) {
	p := newPalette(color)

	totalSkipped := 0
	for _, s := range rep.Sites {
		totalSkipped += s.Summary.Skipped
	}
	if totalSkipped > 0 {
		fmt.Fprintf(w, "%s⚠ %d check(s) skipped — see report%s\n\n", p.yellow, totalSkipped, p.reset)
	}

	for _, site := range rep.Sites {
		fmt.Fprintf(w, "%s▸ %s%s %s(%s)%s\n", p.bold, site.Name, p.reset, p.dim, site.URL, p.reset)

		// Per-module tally.
		for _, m := range modulesOf(site.Findings) {
			var fail, warn, pass, skip, info int
			for _, f := range site.Findings {
				if f.Module != m {
					continue
				}
				switch f.Status {
				case model.StatusFail:
					if f.Severity == model.SeverityError {
						fail++
					} else {
						warn++
					}
				case model.StatusPass:
					pass++
				case model.StatusSkipped:
					skip++
				case model.StatusInfo:
					info++
				}
			}
			fmt.Fprintf(w, "  %-14s %serror %d%s  %swarn %d%s  %spass %d%s  skip %d  info %d\n",
				m, p.red, fail, p.reset, p.yellow, warn, p.reset, p.green, pass, p.reset, skip, info)
		}

		// List actionable findings (fails), errors first.
		for _, f := range site.Findings {
			if f.Status != model.StatusFail {
				continue
			}
			col := p.yellow
			if f.Severity == model.SeverityError {
				col = p.red
			}
			fmt.Fprintf(w, "    %s%-5s%s %s %s — %s\n",
				col, f.Severity, p.reset, f.Module, f.Criterion, f.Observed)
		}

		s := site.Summary
		fmt.Fprintf(w, "  %sSummary: %d fail, %d pass, %d skipped, %d info%s\n\n",
			p.dim, s.Fail, s.Pass, s.Skipped, s.Info, p.reset)
	}
}

func modulesOf(fs []model.Finding) []string {
	seen := map[string]bool{}
	var mods []string
	for _, f := range fs {
		if !seen[f.Module] {
			seen[f.Module] = true
			mods = append(mods, f.Module)
		}
	}
	sort.Strings(mods)
	return mods
}
