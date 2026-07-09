// Package report renders a model.Report to the output formats. JSON is the
// source of truth; console, markdown, and tasks are all derived from the same
// findings, never recomputed (v0.4 §6b).
package report

import (
	"encoding/json"
	"io"

	"github.com/Estetika101/cairn/internal/model"
)

// WriteJSON emits the report envelope as indented JSON.
func WriteJSON(w io.Writer, rep model.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(rep)
}
