// Package dashboard is the local, embedded web UI (v0.4 §6c): a viewer over
// the same report.json every other output format derives from, plus a config
// editor and audit trigger. It is deliberately NOT a model.Reporter: unlike a
// one-shot Emit, this is a long-running HTTP server, a distinct subsystem
// (v0.4 §6c's own framing).
package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Estetika101/cairn/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed assets/*
var assetsFS embed.FS

// Options configures a Server. ReportDir is required (it's what /api/report
// and the report view read). ConfigPath and RunAudit are optional together:
// with both empty/nil, the dashboard is a pure read-only viewer, matching
// `cairn serve --report` with no --config given.
type Options struct {
	ReportDir  string
	ConfigPath string       // "" disables /api/config entirely
	RunAudit   func() error // nil disables /api/audit entirely
	// AllowRemoteConfig gates the WRITE-capable endpoints (/api/config POST,
	// /api/audit POST), not the read-only ones. Reading a report or the
	// current config over the LAN is the lower-risk case already documented
	// in the CLI's --host flag; writing config or triggering a crawl is not,
	// so those two stay localhost-only unless this is explicitly true
	// (v0.4 §6c rule 3).
	AllowRemoteConfig bool
}

// Server serves the dashboard for one report directory (and optionally one
// editable config file).
type Server struct {
	opts Options
	mux  *http.ServeMux
}

// New builds a dashboard per opts. The report view is read fresh from disk on
// every request, so a page reload always shows the latest completed run — no
// caching, no staleness.
func New(opts Options) (*Server, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}

	s := &Server{opts: opts}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/robots.txt", robotsHandler)
	mux.HandleFunc("/api/report", reportHandler(opts.ReportDir))

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			configGetHandler(opts.ConfigPath)(w, r)
		case http.MethodPost:
			s.requireLocal(configPostHandler(opts.ConfigPath))(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/audit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.requireLocal(auditPostHandler(opts.RunAudit))(w, r)
	})

	s.mux = mux
	return s, nil
}

// ServeHTTP applies noindex/nofollow to every response before dispatching.
// The dashboard shows a live audit of whatever site it's pointed at; if it's
// ever exposed beyond localhost (e.g. a playground subdomain), it must never
// be indexed or crawled by search engines. Belt-and-suspenders: the header
// covers every response including the JSON APIs, robots.txt covers crawlers
// that ignore headers, and the HTML itself carries a <meta robots> tag too.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	s.mux.ServeHTTP(w, r)
}

// requireLocal gates a write-capable handler behind a loopback check unless
// AllowRemoteConfig is set (v0.4 §6c rule 3) — config controls what gets
// crawled and can carry respectRobots:false, so it is never a silent default
// to expose it alongside the (lower-risk) read-only viewer.
func (s *Server) requireLocal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.opts.AllowRemoteConfig && !isLoopback(r.RemoteAddr) {
			http.Error(w, "config-editing and audit-trigger endpoints are localhost-only "+
				"unless serve.allowRemoteConfig is set", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func robotsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
}

// ListenAndServe starts the dashboard on addr and blocks until it exits.
func ListenAndServe(addr string, opts Options) error {
	srv, err := New(opts)
	if err != nil {
		return err
	}
	return http.ListenAndServe(addr, srv)
}

// reportHandler serves report.json verbatim — the report view's only data
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

// configGetHandler returns the full parsed config as JSON — everything on
// disk, not just the fields the current form edits, so a future richer form
// (or a curious user) can see the whole picture.
func configGetHandler(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configPath == "" {
			http.Error(w, "config editing not available: cairn serve was not given --config", http.StatusNotImplemented)
			return
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(cfg)
	}
}

// configPostHandler merges a JSON body onto a freshly loaded copy of the
// on-disk config, so fields the sender's JSON doesn't mention (crawl, tier2,
// plugins, serve — anything the scaffold form doesn't render) are left
// exactly as they were, never blanked out. Validates through the identical
// path Parse/Load use before writing anything (v0.4 §6c rule 2: one validator
// behind every on-ramp).
//
// Known gap, stated plainly rather than left silent: this writes via
// yaml.Marshal, which does NOT preserve comments or key order in the existing
// file. A hand-written cairn.yaml with explanatory comments will lose them the
// first time it's saved from this form. Comment-preserving writes (via
// yaml.v3's Node API) are real work, deferred rather than done partially.
func configPostHandler(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configPath == "" {
			http.Error(w, "config editing not available: cairn serve was not given --config", http.StatusNotImplemented)
			return
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "could not read request body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, cfg); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out, err := yaml.Marshal(cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(configPath, out, 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// auditPostHandler runs a fresh audit synchronously and reports success/error.
// Synchronous is a deliberate scaffold-scope simplification — a real async job
// queue (so a slow crawl doesn't hold the HTTP request open) is future work,
// same pattern as `cairn watch` being reserved rather than half-built.
func auditPostHandler(run func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if run == nil {
			http.Error(w, "audit trigger not available: cairn serve was not given --config", http.StatusNotImplemented)
			return
		}
		if err := run(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
