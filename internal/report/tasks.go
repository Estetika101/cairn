package report

import (
	"fmt"
	"io"

	"github.com/Estetika101/verdict/internal/model"
)

// WriteTasks emits verdict-tasks.md: a flat, agent-facing checklist grouped by
// severity then module, derived from the same findings as every other format
// (v0.4 §6b). Each item leads with the stable ID for dedup across runs.
func WriteTasks(w io.Writer, rep model.Report) {
	fmt.Fprintf(w, "# verdict action items\n\n")

	writeGroup(w, rep, "## Errors (fix before next deploy)", func(f model.Finding) bool {
		return f.Status == model.StatusFail && f.Severity == model.SeverityError
	})
	writeGroup(w, rep, "## Warnings (fix when convenient)", func(f model.Finding) bool {
		return f.Status == model.StatusFail && f.Severity == model.SeverityWarn
	})
	writeGroup(w, rep, "## Informational (not scored, no action required)", func(f model.Finding) bool {
		return f.Status == model.StatusInfo
	})
	writeGroup(w, rep, "## Skipped (not checked this run — see reason)", func(f model.Finding) bool {
		return f.Status == model.StatusSkipped
	})

	fmt.Fprintf(w, "## Needs human review (not auto-detectable — see spec §5b)\n\n")
	fmt.Fprintf(w, "- [ ] Automated checks cover only the mechanical slice of each discipline. "+
		"Review content quality, alt-text meaningfulness, and business-logic security by hand.\n")
}

func writeGroup(w io.Writer, rep model.Report, heading string, match func(model.Finding) bool) {
	var lines []string
	for _, site := range rep.Sites {
		for _, f := range site.Findings {
			if !match(f) {
				continue
			}
			lines = append(lines, taskLine(f))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "%s\n\n", heading)
	for _, l := range lines {
		fmt.Fprint(w, l)
	}
	fmt.Fprintln(w)
}

func taskLine(f model.Finding) string {
	switch f.Status {
	case model.StatusSkipped:
		return fmt.Sprintf("- [ ] **[%s]** %s — %s (%s): %s\n",
			f.ID, f.Module, f.Criterion, f.Location.URL, f.Reason)
	case model.StatusInfo:
		return fmt.Sprintf("- [ ] **[%s]** %s — %s (%s): %s\n",
			f.ID, f.Module, f.Criterion, f.Location.URL, f.Observed)
	default:
		fix := f.SuggestedFix
		if fix == "" {
			fix = f.Required
		}
		return fmt.Sprintf("- [ ] **[%s]** %s — %s on %s.\n      %s\n",
			f.ID, f.Module, f.Criterion, f.Location.URL, fix)
	}
}
