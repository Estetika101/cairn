package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Estetika101/verdict/internal/model"
	"github.com/tetratelabs/wazero/api"
)

// hostState carries the current run's CheckContext to the host fetch function.
// It is passed via context value so a fresh guest instance per Run never shares
// a fetch capability with another run.
type hostState struct {
	cc model.CheckContext
}

type stateKeyType struct{}

var stateKey stateKeyType

func withState(ctx context.Context, st *hostState) context.Context {
	return context.WithValue(ctx, stateKey, st)
}

func stateFrom(ctx context.Context) *hostState {
	st, _ := ctx.Value(stateKey).(*hostState)
	return st
}

// fetchResult is what the host fetch capability returns to a guest. By default
// it carries the body hash + length, not the bytes, so the boundary stays thin
// (a fetch_body variant is future work). `error` is one of the typed strings
// from model (e.g. "disallowed by robots.txt") or "".
type fetchResult struct {
	Status     int                 `json:"status"`
	FinalURL   string              `json:"finalURL"`
	Headers    map[string][]string `json:"headers"`
	BodyLen    int                 `json:"bodyLen"`
	BodySha256 string              `json:"bodySha256"`
	Error      string              `json:"error"`
}

// hostFetch is the single capability the sandbox grants: it routes a guest's
// fetch straight into the engine's Fetcher (via the run's CheckContext), so a
// plugin is subject to the exact same robots/politeness/budget gates as a
// built-in. The sandbox boundary and the politeness boundary are one boundary.
func hostFetch(ctx context.Context, mod api.Module, urlPtr, urlLen uint32) uint64 {
	var res fetchResult

	urlBytes, err := readFromGuest(mod, urlPtr, urlLen)
	if err != nil {
		res.Error = "bad url pointer"
	} else if st := stateFrom(ctx); st == nil {
		res.Error = "no host capability"
	} else {
		pd, ferr := st.cc.Fetch(ctx, string(urlBytes))
		if ferr != nil {
			res.Error = ferr.Error()
		} else {
			sum := sha256.Sum256(pd.Body)
			res = fetchResult{
				Status:     pd.Status,
				FinalURL:   pd.FinalURL,
				Headers:    pd.Headers,
				BodyLen:    len(pd.Body),
				BodySha256: hex.EncodeToString(sum[:]),
			}
		}
	}

	b, _ := json.Marshal(res)
	ptr, werr := writeToGuest(ctx, mod, b)
	if werr != nil {
		return 0
	}
	return (uint64(ptr) << 32) | uint64(len(b))
}
