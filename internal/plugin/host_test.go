package plugin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Estetika101/verdict/internal/engine"
	"github.com/Estetika101/verdict/internal/model"
	"github.com/Estetika101/verdict/internal/plugin"
)

const (
	demoWasm        = "../../plugins/example-x-powered-by/x-powered-by.wasm"
	needsWasiWasm   = "testdata/needs-wasi/needs-wasi.wasm"
	fetchCallerWasm = "testdata/fetch-caller/fetch-caller.wasm"
	busyLoopWasm    = "testdata/busy-loop/busy-loop.wasm"
)

// stubCtx is a CheckContext whose Fetch is programmable, so a test can count
// fetches or route them through a real engine Fetcher.
type stubCtx struct {
	scope      model.Scope
	page       model.PageData
	corpus     []model.PageData
	fetch      func(context.Context, string) (model.PageData, error)
	fetchCount *int
}

func (s *stubCtx) Scope() model.Scope        { return s.scope }
func (s *stubCtx) Page() model.PageData      { return s.page }
func (s *stubCtx) Corpus() []model.PageData  { return s.corpus }
func (s *stubCtx) Config() model.CheckConfig { return model.CheckConfig{} }
func (s *stubCtx) Logf(string, ...any)       {}
func (s *stubCtx) Fetch(ctx context.Context, url string) (model.PageData, error) {
	if s.fetchCount != nil {
		*s.fetchCount++
	}
	if s.fetch != nil {
		return s.fetch(ctx, url)
	}
	return model.PageData{}, nil
}

// Row 3 (happy path) + denial case 2 (no ambient network): the demo plugin runs,
// emits its info finding, and makes zero fetches — it was granted no capability.
func TestDemoRunsAndTouchesNoNetwork(t *testing.T) {
	ctx := context.Background()
	p, err := plugin.Load(ctx, demoWasm)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer p.Close(ctx)

	if p.Meta().ID != "example-x-powered-by" {
		t.Fatalf("meta id = %q", p.Meta().ID)
	}

	count := 0
	cc := &stubCtx{
		scope:      model.ScopePage,
		fetchCount: &count,
		page: model.PageData{
			FinalURL: "https://example.test/",
			Headers:  http.Header{"X-Powered-By": {"PHP/8.1"}},
		},
	}
	fs, err := p.Run(cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fs) != 1 || fs[0].Status != model.StatusInfo {
		t.Fatalf("findings = %+v, want 1 info", fs)
	}
	if !strings.Contains(fs[0].Observed, "PHP/8.1") {
		t.Errorf("observed = %q, want it to mention the header value", fs[0].Observed)
	}
	if count != 0 {
		t.Errorf("demo made %d fetches; a no-capability plugin must make 0", count)
	}
}

func TestDemoNoHeaderNoFinding(t *testing.T) {
	ctx := context.Background()
	p, err := plugin.Load(ctx, demoWasm)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer p.Close(ctx)

	cc := &stubCtx{scope: model.ScopePage, page: model.PageData{FinalURL: "https://x/", Headers: http.Header{}}}
	fs, err := p.Run(cc)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("findings = %+v, want none when X-Powered-By absent", fs)
	}
}

// Denial case 1: a guest importing a capability the host does not provide (WASI
// fd_write) fails to load — it never reaches execution.
func TestMissingCapabilityFailsToLoad(t *testing.T) {
	_, err := plugin.Load(context.Background(), needsWasiWasm)
	if err == nil {
		t.Fatal("expected load to fail: host grants no WASI")
	}
	if !strings.Contains(err.Error(), "wasi_snapshot_preview1") && !strings.Contains(err.Error(), "fd_write") {
		t.Errorf("error = %q, want it to name the missing WASI import", err.Error())
	}
}

// Denial case 3: the one granted capability (fetch) is still governed. A guest
// that fetches a robots-disallowed URL gets the typed robots error back, proving
// the sandbox boundary and the politeness boundary are the same boundary.
func TestGrantedFetchIsGoverned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
			return
		}
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()

	f := engine.NewFetcher(model.CrawlConfig{
		RequestTimeoutMs:      5000,
		UserAgent:             "verdict/0.1 (+test)",
		MaxConcurrentRequests: 8,
		MaxExtraFetches:       500,
		RespectRobots:         true,
		PerHost:               model.PerHostConfig{Concurrency: 2, DelayMs: 0},
	})

	ctx := context.Background()
	p, err := plugin.Load(ctx, fetchCallerWasm)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer p.Close(ctx)

	cc := &stubCtx{
		scope: model.ScopePage,
		page:  model.PageData{FinalURL: srv.URL + "/private"},
		fetch: func(c context.Context, url string) (model.PageData, error) {
			return f.Fetch(c, url, engine.ExtraFetch)
		},
	}
	fs, err := p.Run(cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("findings = %+v, want 1", fs)
	}
	if !strings.Contains(fs[0].Observed, "disallowed by robots.txt") {
		t.Errorf("observed = %q, want the robots error surfaced through host fetch", fs[0].Observed)
	}
}

// Resource bound (handover P1-6): a runaway guest is interrupted on the wall
// clock and recorded as skipped — never allowed to hang the run.
func TestRunawayGuestIsInterrupted(t *testing.T) {
	old := plugin.RunTimeout
	plugin.RunTimeout = 300 * time.Millisecond
	defer func() { plugin.RunTimeout = old }()

	ctx := context.Background()
	p, err := plugin.Load(ctx, busyLoopWasm)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer p.Close(ctx)

	start := time.Now()
	cc := &stubCtx{scope: model.ScopePage, page: model.PageData{FinalURL: "https://x/"}}
	fs, err := p.Run(cc)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("run took %v; interrupt did not fire", elapsed)
	}
	if len(fs) != 1 || fs[0].Status != model.StatusSkipped {
		t.Fatalf("findings = %+v, want 1 skipped", fs)
	}
	if !strings.Contains(fs[0].Reason, "timeout") {
		t.Errorf("reason = %q, want a timeout reason", fs[0].Reason)
	}
}
