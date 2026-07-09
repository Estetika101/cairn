package engine

import (
	"context"
	"log"

	"github.com/Estetika101/cairn/internal/model"
)

// checkCtx is the concrete CheckContext handed to both built-in checks and (via
// the plugin host) WASM guests. Fetch is the only network handle it exposes —
// there is no other way for a check to reach the network.
type checkCtx struct {
	scope    model.Scope
	page     model.PageData
	corpus   []model.PageData
	fetcher  *Fetcher
	checkCfg model.CheckConfig
	logf     func(string, ...any)
}

func (c *checkCtx) Scope() model.Scope        { return c.scope }
func (c *checkCtx) Page() model.PageData      { return c.page }
func (c *checkCtx) Corpus() []model.PageData  { return c.corpus }
func (c *checkCtx) Config() model.CheckConfig { return c.checkCfg }

func (c *checkCtx) Fetch(ctx context.Context, rawURL string) (model.PageData, error) {
	// Checks always draw from the per-site budget.
	return c.fetcher.Fetch(ctx, rawURL, ExtraFetch)
}

func (c *checkCtx) Logf(format string, args ...any) {
	if c.logf != nil {
		c.logf(format, args...)
		return
	}
	log.Printf(format, args...)
}
