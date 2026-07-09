// busy-loop is a denial-suite test guest that never returns from run(), proving
// the host's wall-clock interrupt: a runaway plugin is killed and recorded as
// skipped, never allowed to hang the run.
//
// Build: tinygo build -o busy-loop.wasm -target=wasm-unknown -no-debug ./
//
//go:build tinygo

package main

import "unsafe"

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

//go:wasmexport meta
func meta() uint64 {
	b := []byte(`{"id":"busy-loop","module":"test","tier":1,"scope":"page","severity":"warn","title":"busy loop"}`)
	p := alloc(uint32(len(b)))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(p))), len(b)), b)
	return (uint64(p) << 32) | uint64(len(b))
}

var sink uint32

//go:wasmexport run
func run(_, _ uint32) uint64 {
	for {
		sink++ // side effect prevents the loop being optimized away
	}
}
