// Package dashboard is the local, embedded web UI (v0.4 §6c): a viewer over
// the same report.json every other output format derives from. It is read-only
// — no database, no writes, no external calls — and renders exactly what's on
// disk. It is deliberately NOT a model.Reporter: unlike a one-shot Emit, this is
// a long-running HTTP server, a distinct subsystem (v0.4 §6c's own framing).
package dashboard

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed assets/*
var assetsFS embed.FS

// Server serves the dashboard for a single report directory.
type Server struct {
	reportDir string
	mux       *http.ServeMux
}

// New builds a dashboard bound to reportDir (where cairn audit wrote
// report.json). The UI is read fresh from disk on every request, so a page
// reload always shows the latest completed run — no caching, no staleness.
func New(reportDir string) (*Server, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/report", reportHandler(reportDir))

	return &Server{reportDir: reportDir, mux: mux}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// ListenAndServe starts the dashboard on addr and blocks until it exits.
func ListenAndServe(addr, reportDir string) error {
	srv, err := New(reportDir)
	if err != nil {
		return err
	}
	return http.ListenAndServe(addr, srv)
}

// reportHandler serves report.json verbatim — the dashboard's only data
// source, so it can never show anything the JSON report doesn't already say.
func reportHandler(reportDir string) http.HandlerFunc {
	path := filepath.Join(reportDir, "report.json")
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("no report.json in %s yet — run `cairn audit` first", reportDir), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	}
}
