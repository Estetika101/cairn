// needs-wasi is a denial-suite test guest built for the WASI target, so it
// imports wasi_snapshot_preview1 functions (fd_write, via println). The verdict
// host provides no WASI, so this guest must FAIL TO INSTANTIATE — proving a
// plugin that reaches for a capability the host doesn't grant never even loads.
//
// Build: tinygo build -o needs-wasi.wasm -target=wasi -no-debug ./
//
//go:build tinygo

package main

func main() {
	println("this guest tries to write to stdout via WASI fd_write")
}
