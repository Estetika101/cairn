package plugin

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// The ABI is deliberately dumb: JSON over linear memory, length-prefixed as a
// packed u64 = (ptr<<32)|len. Any guest language can implement it. Ownership
// follows "producer allocates, consumer frees": the host frees the meta()/run()
// result buffers after reading them; the guest frees a fetch result after
// decoding it. Because a guest alloc() may grow (and relocate) memory, the host
// re-reads mod.Memory() immediately before every access.

// wireMeta mirrors the guest's CheckMeta JSON.
type wireMeta struct {
	ID       string `json:"id"`
	Module   string `json:"module"`
	Tier     int    `json:"tier"`
	Scope    string `json:"scope"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
}

// wirePage is a page as handed to a guest: URL + headers (body is fetched on
// demand via the host fetch capability, so it isn't shoved across eagerly).
type wirePage struct {
	URL     string              `json:"url"`
	Status  int                 `json:"status,omitempty"`
	Headers map[string][]string `json:"headers"`
}

// wireContext is the serialized CheckContext passed into run().
type wireContext struct {
	Scope  string     `json:"scope"`
	Page   wirePage   `json:"page"`
	Corpus []wirePage `json:"corpus,omitempty"`
}

// wireFinding is a finding as emitted by a guest (a subset of model.Finding).
type wireFinding struct {
	Module       string `json:"module"`
	Scope        string `json:"scope"`
	Criterion    string `json:"criterion,omitempty"`
	Status       string `json:"status"`
	Severity     string `json:"severity,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Observed     string `json:"observed,omitempty"`
	Required     string `json:"required,omitempty"`
	SuggestedFix string `json:"suggestedFix,omitempty"`
	Location     struct {
		URL string `json:"url"`
	} `json:"location"`
}

func unpack(v uint64) (ptr, length uint32) { return uint32(v >> 32), uint32(v) }

// writeToGuest allocates len(data) bytes in guest memory (via the guest's own
// allocator) and copies data in. The memory handle is fetched AFTER alloc,
// since alloc may have grown it.
func writeToGuest(ctx context.Context, mod api.Module, data []byte) (uint32, error) {
	alloc := mod.ExportedFunction("alloc")
	if alloc == nil {
		return 0, fmt.Errorf("plugin: guest exports no alloc")
	}
	res, err := alloc.Call(ctx, uint64(len(data)))
	if err != nil {
		return 0, err
	}
	ptr := uint32(res[0])
	if !mod.Memory().Write(ptr, data) {
		return 0, fmt.Errorf("plugin: write out of bounds")
	}
	return ptr, nil
}

// readFromGuest copies length bytes at ptr out of guest memory.
func readFromGuest(mod api.Module, ptr, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	view, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("plugin: read out of bounds")
	}
	out := make([]byte, len(view))
	copy(out, view) // the view is only valid until the next memory resize
	return out, nil
}

// freeGuest returns a buffer to the guest allocator. Best-effort: a guest that
// exports no free simply leaks for the (short-lived) instance.
func freeGuest(ctx context.Context, mod api.Module, ptr, length uint32) {
	if f := mod.ExportedFunction("free"); f != nil {
		_, _ = f.Call(ctx, uint64(ptr), uint64(length))
	}
}
