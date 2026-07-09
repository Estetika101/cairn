// Package plugin runs third-party checks as sandboxed WebAssembly. A plugin has
// no ambient authority: the wazero host grants it exactly one capability —
// cairn.fetch, which routes into the engine's Fetcher — and nothing else (no
// WASI, no filesystem, no sockets). What v0.1/v0.2 could only ask checks to
// honor, the sandbox removes the authority to violate (v0.4 §2c).
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const memoryLimitPages = 256 // 16 MiB cap on guest linear memory

// RunTimeout bounds a single plugin run's wall clock. A guest that exceeds it is
// interrupted (wazero closes the module on context-done) and recorded as skipped
// — a runaway plugin can never hang the whole audit. Overridable for tests.
var RunTimeout = 5 * time.Second

// Plugin is a WASM check. It satisfies model.Check, so it flows through the exact
// same runner path as a built-in — no privileged built-in path.
type Plugin struct {
	runtime wazero.Runtime
	code    wazero.CompiledModule
	meta    model.CheckMeta
	path    string
}

// Load compiles a .wasm plugin, wires the cairn host module, and reads its Meta.
// Instantiation is where a guest importing a capability the host doesn't provide
// (e.g. WASI) fails — so a plugin that tries to reach outside the sandbox never
// even loads.
func Load(ctx context.Context, path string) (*Plugin, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", path, err)
	}

	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(memoryLimitPages))

	if _, err := r.NewHostModuleBuilder("cairn").
		NewFunctionBuilder().WithFunc(hostFetch).Export("fetch").
		Instantiate(ctx); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: host module: %w", path, err)
	}

	code, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: compile: %w", path, err)
	}

	p := &Plugin{runtime: r, code: code, path: path}
	m, err := p.readMeta(ctx)
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("plugin %s: %w", path, err)
	}
	p.meta = m
	return p, nil
}

// Close releases the runtime.
func (p *Plugin) Close(ctx context.Context) error { return p.runtime.Close(ctx) }

// Meta returns the plugin's declared metadata.
func (p *Plugin) Meta() model.CheckMeta { return p.meta }

// Run instantiates a fresh guest, hands it the serialized context, and collects
// its findings. Any trap/timeout/OOM is turned into a single skipped finding —
// a misbehaving plugin is never allowed to fail the whole run.
func (p *Plugin) Run(cc model.CheckContext) ([]model.Finding, error) {
	runCtx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	defer cancel()
	ctx := withState(runCtx, &hostState{cc: cc})

	mod, err := p.instantiate(ctx)
	if err != nil {
		return []model.Finding{p.skip(cc, "plugin failed to instantiate")}, nil
	}
	defer mod.Close(ctx)

	ctxJSON := serializeContext(cc)
	ptr, err := writeToGuest(ctx, mod, ctxJSON)
	if err != nil {
		return []model.Finding{p.skip(cc, "plugin abi error")}, nil
	}
	run := mod.ExportedFunction("run")
	if run == nil {
		return []model.Finding{p.skip(cc, "plugin exports no run")}, nil
	}
	res, err := run.Call(ctx, uint64(ptr), uint64(len(ctxJSON)))
	freeGuest(ctx, mod, ptr, uint32(len(ctxJSON)))
	if err != nil {
		return []model.Finding{p.skip(cc, "plugin timeout/oom")}, nil
	}

	rptr, rlen := unpack(res[0])
	raw, err := readFromGuest(mod, rptr, rlen)
	freeGuest(ctx, mod, rptr, rlen)
	if err != nil {
		return []model.Finding{p.skip(cc, "plugin abi error")}, nil
	}

	var wfs []wireFinding
	if err := json.Unmarshal(raw, &wfs); err != nil {
		return []model.Finding{p.skip(cc, "plugin returned invalid findings")}, nil
	}
	return p.toModelFindings(cc, wfs), nil
}

func (p *Plugin) instantiate(ctx context.Context) (api.Module, error) {
	return p.runtime.InstantiateModule(ctx, p.code,
		wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize"))
}

func (p *Plugin) readMeta(ctx context.Context) (model.CheckMeta, error) {
	mod, err := p.instantiate(ctx)
	if err != nil {
		return model.CheckMeta{}, err
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("meta")
	if fn == nil {
		return model.CheckMeta{}, fmt.Errorf("guest exports no meta")
	}
	res, err := fn.Call(ctx)
	if err != nil {
		return model.CheckMeta{}, err
	}
	ptr, length := unpack(res[0])
	raw, err := readFromGuest(mod, ptr, length)
	freeGuest(ctx, mod, ptr, length)
	if err != nil {
		return model.CheckMeta{}, err
	}
	var wm wireMeta
	if err := json.Unmarshal(raw, &wm); err != nil {
		return model.CheckMeta{}, fmt.Errorf("bad meta json: %w", err)
	}
	if wm.ID == "" {
		return model.CheckMeta{}, fmt.Errorf("plugin meta has empty id")
	}
	return model.CheckMeta{
		ID: wm.ID, Module: wm.Module, Tier: wm.Tier,
		Scope: model.Scope(wm.Scope), Severity: model.Severity(wm.Severity), Title: wm.Title,
	}, nil
}

func serializeContext(cc model.CheckContext) []byte {
	wc := wireContext{Scope: string(cc.Scope())}
	if cc.Scope() == model.ScopeSite {
		for _, p := range cc.Corpus() {
			wc.Corpus = append(wc.Corpus, wirePage{URL: pageURL(p), Status: p.Status, Headers: p.Headers})
		}
	} else {
		p := cc.Page()
		wc.Page = wirePage{URL: pageURL(p), Status: p.Status, Headers: p.Headers}
	}
	b, _ := json.Marshal(wc)
	return b
}

func (p *Plugin) toModelFindings(cc model.CheckContext, wfs []wireFinding) []model.Finding {
	out := make([]model.Finding, 0, len(wfs))
	for _, wf := range wfs {
		loc := model.Location{URL: wf.Location.URL}
		if loc.URL == "" {
			loc.URL = ctxURL(cc)
		}
		out = append(out, model.Finding{
			Module:       wf.Module,
			Scope:        model.Scope(wf.Scope),
			Criterion:    wf.Criterion,
			Status:       model.Status(wf.Status),
			Severity:     model.Severity(wf.Severity),
			Reason:       wf.Reason,
			Location:     loc,
			Observed:     wf.Observed,
			Required:     wf.Required,
			SuggestedFix: wf.SuggestedFix,
		})
	}
	return out
}

func (p *Plugin) skip(cc model.CheckContext, reason string) model.Finding {
	return model.Finding{
		Module:    p.meta.Module,
		Scope:     p.meta.Scope,
		Criterion: p.meta.ID,
		Status:    model.StatusSkipped,
		Reason:    reason,
		Location:  model.Location{URL: ctxURL(cc)},
	}
}

func ctxURL(cc model.CheckContext) string {
	if cc.Scope() == model.ScopePage {
		return pageURL(cc.Page())
	}
	if c := cc.Corpus(); len(c) > 0 {
		return pageURL(c[0])
	}
	return ""
}

func pageURL(p model.PageData) string {
	if p.FinalURL != "" {
		return p.FinalURL
	}
	return p.RequestedURL
}
