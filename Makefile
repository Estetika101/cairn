.PHONY: build test race vet plugins

build:
	go build -o cairn ./cmd/cairn

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# Rebuild the checked-in WASM plugin and denial-suite test guests.
# Requires TinyGo (https://tinygo.org). The .wasm files are committed so the Go
# tests run without TinyGo present; regenerate them only when a guest changes.
plugins:
	tinygo build -o plugins/example-x-powered-by/x-powered-by.wasm -target=wasm-unknown -no-debug ./plugins/example-x-powered-by
	tinygo build -o internal/plugin/testdata/fetch-caller/fetch-caller.wasm -target=wasm-unknown -no-debug ./internal/plugin/testdata/fetch-caller
	tinygo build -o internal/plugin/testdata/needs-wasi/needs-wasi.wasm -target=wasi -no-debug ./internal/plugin/testdata/needs-wasi
	tinygo build -o internal/plugin/testdata/busy-loop/busy-loop.wasm -target=wasm-unknown -no-debug ./internal/plugin/testdata/busy-loop
