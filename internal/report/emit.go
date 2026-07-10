package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Estetika101/verdict/internal/model"
)

// Emit writes every requested output format. Console goes to stdout; file
// formats are written under outDir. The set of formats is validated upstream
// (config), so an unknown format here is a programming error, not user error.
func Emit(rep model.Report, formats []string, outDir string, stdout io.Writer, color bool) error {
	needsDir := false
	for _, f := range formats {
		if f != "console" {
			needsDir = true
		}
	}
	if needsDir {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}

	for _, format := range formats {
		switch format {
		case "console":
			WriteConsole(stdout, rep, color)
		case "json":
			if err := writeFile(filepath.Join(outDir, "report.json"), func(w io.Writer) error { return WriteJSON(w, rep) }); err != nil {
				return err
			}
		case "markdown":
			if err := writeFile(filepath.Join(outDir, "report.md"), func(w io.Writer) error { WriteMarkdown(w, rep); return nil }); err != nil {
				return err
			}
		case "tasks":
			if err := writeFile(filepath.Join(outDir, "verdict-tasks.md"), func(w io.Writer) error { WriteTasks(w, rep); return nil }); err != nil {
				return err
			}
		default:
			return fmt.Errorf("report: unknown format %q", format)
		}
	}
	return nil
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer f.Close()
	return write(f)
}

// ExitCode returns the process exit code for a report given the failOn gate.
// Only status:fail findings at or above the threshold count; pass/skipped/info
// never affect it (v0.4 §6b). error > warn.
func ExitCode(rep model.Report, failOn string) int {
	if failOn == "off" {
		return 0
	}
	for _, site := range rep.Sites {
		for _, f := range site.Findings {
			if f.Status != model.StatusFail {
				continue
			}
			switch failOn {
			case "error":
				if f.Severity == model.SeverityError {
					return 1
				}
			case "warn":
				if f.Severity == model.SeverityError || f.Severity == model.SeverityWarn {
					return 1
				}
			}
		}
	}
	return 0
}
