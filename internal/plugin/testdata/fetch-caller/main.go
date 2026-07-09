// fetch-caller is a denial-suite test guest: it DOES import cairn.fetch and calls
// it on the URL it is handed, proving the one granted capability is still routed
// through the engine's gates (a robots-disallowed URL comes back as an error).
//
// Build: tinygo build -o fetch-caller.wasm -target=wasm-unknown -no-debug ./
//
//go:build tinygo

package main

import (
	"encoding/json"
	"strconv"
	"unsafe"
)

func main() {}

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
func free(ptr uint32, _ uint32) { delete(pinned, uintptr(ptr)) }

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

//go:wasmimport cairn fetch
func hostFetch(urlPtr, urlLen uint32) uint64

type wireContext struct {
	Page struct {
		URL string `json:"url"`
	} `json:"page"`
}

type wireFinding struct {
	Module    string `json:"module"`
	Scope     string `json:"scope"`
	Criterion string `json:"criterion"`
	Status    string `json:"status"`
	Location  struct {
		URL string `json:"url"`
	} `json:"location"`
	Observed string `json:"observed"`
}

//go:wasmexport meta
func meta() uint64 {
	return pack([]byte(`{"id":"fetch-caller","module":"links","tier":1,"scope":"page","severity":"warn","title":"fetch caller"}`))
}

//go:wasmexport run
func run(ctxPtr, ctxLen uint32) uint64 {
	var wc wireContext
	_ = json.Unmarshal(readMem(ctxPtr, ctxLen), &wc)
	url := wc.Page.URL

	packed := hostFetch(strPtr(url), uint32(len(url)))
	rptr, rln := uint32(packed>>32), uint32(packed)
	var fr struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(readMem(rptr, rln), &fr)
	free(rptr, rln) // guest frees the host-provided fetch result after decode

	var f wireFinding
	f.Module = "links"
	f.Scope = "page"
	f.Criterion = "plugin: fetch"
	f.Status = "info"
	f.Location.URL = url
	if fr.Error != "" {
		f.Observed = "fetch error: " + fr.Error
	} else {
		f.Observed = "fetched status " + strconv.Itoa(fr.Status)
	}
	b, _ := json.Marshal([]wireFinding{f})
	return pack(b)
}

func strPtr(s string) uint32 {
	if len(s) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(unsafe.StringData(s))))
}
