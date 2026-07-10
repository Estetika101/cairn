package report

import (
	"fmt"
	"io"

	"github.com/Estetika101/verdict/internal/model"
)

// WriteMarkdown emits a human-readable report (report.md) — the same findings as
// the console view, in a form suitable for committing or pasting into a PR.
func WriteMarkdown(w io.Writer, rep model.Report) {
	fmt.Fprintf(w, "# verdict report\n\n")
	for _, site := range rep.Sites {
		s := site.Summary
		fmt.Fprintf(w, "## %s\n\n%s\n\n", site.Name, site.URL)
		fmt.Fprintf(w, "**%d fail · %d pass · %d skipped · %d info**\n\n", s.Fail, s.Pass, s.Skipped, s.Info)

		fmt.Fprintf(w, "| status | severity | module | criterion | observed |\n")
		fmt.Fprintf(w, "|---|---|---|---|---|\n")
		for _, f := range site.Findings {
			detail := f.Observed
			if f.Status == model.StatusSkipped {
				detail = f.Reason
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
				f.Status, dashIfEmpty(string(f.Severity)), f.Module, f.Criterion, dashIfEmpty(detail))
		}
		fmt.Fprintln(w)
	}
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
