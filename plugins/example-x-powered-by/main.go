// example-x-powered-by is a demo third-party cairn check, compiled to WASM. It
// flags the presence of an X-Powered-By response header (an information-leak
// smell) as status:info. It needs only the page headers it is handed, so it
// imports no host capability at all — proving a plugin the sandbox grants
// nothing to still works.
//
// Build: tinygo build -o x-powered-by.wasm -target=wasm-unknown -no-debug ./
//
//go:build tinygo

package main

import (
	"encoding/json"
	"strings"
	"unsafe"
)

func main() {}

// --- ABI: linear-memory allocation (producer allocates, consumer frees) ---

var pinned = map[uintptr][]byte{}

//go:wasmexport alloc
func alloc(size uint32) uint32 {
	if size == 0 {
		size = 1
	}
	b := make([]byte, size)
	p := uintptr(unsafe.Pointer(&b[0]))
	pinned[p] = b
	return uint32(p)
}

//go:wasmexport free
func free(ptr uint32, _ uint32) {
	delete(pinned, uintptr(ptr))
}

func pack(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	p := alloc(uint32(len(b)))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(p))), len(b)), b)
	return (uint64(p) << 32) | uint64(len(b))
}

func readMem(ptr, ln uint32) []byte {
	if ln == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), ln)
}

// --- ABI wire types (a subset of cairn's model, JSON-compatible) ---

type wireMeta struct {
	ID       string `json:"id"`
	Module   string `json:"module"`
	Tier     int    `json:"tier"`
	Scope    string `json:"scope"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
}

type wireContext struct {
	Scope string `json:"scope"`
	Page  struct {
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
	} `json:"page"`
}

type wireFinding struct {
	Module    string `json:"module"`
	Scope     string `json:"scope"`
	Criterion string `json:"criterion,omitempty"`
	Status    string `json:"status"`
	Location  struct {
		URL string `json:"url"`
	} `json:"location"`
	Observed string `json:"observed,omitempty"`
}

//go:wasmexport meta
func meta() uint64 {
	b, _ := json.Marshal(wireMeta{
		ID: "example-x-powered-by", Module: "security", Tier: 1,
		Scope: "page", Severity: "warn", Title: "X-Powered-By header present",
	})
	return pack(b)
}

//go:wasmexport run
func run(ctxPtr, ctxLen uint32) uint64 {
	var wc wireContext
	_ = json.Unmarshal(readMem(ctxPtr, ctxLen), &wc)

	var findings []wireFinding
	if v := headerGet(wc.Page.Headers, "X-Powered-By"); v != "" {
		var f wireFinding
		f.Module = "security"
		f.Scope = "page"
		f.Criterion = "info: X-Powered-By"
		f.Status = "info"
		f.Location.URL = wc.Page.URL
		f.Observed = "X-Powered-By: " + v
		findings = append(findings, f)
	}
	b, _ := json.Marshal(findings)
	return pack(b)
}

func headerGet(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
