package model

import (
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// PageData is the fetched artifact a check inspects. For an in-process built-in
// it carries a live goquery.Document handle and the full body. A WASM guest
// receives only a serialized view of this (bytes over the ABI), so a check
// written against Doc is not automatically portable to a plugin (v0.4 §2c).
type PageData struct {
	RequestedURL  string
	FinalURL      string // after redirects
	Status        int
	Headers       http.Header
	Body          []byte
	Doc           *goquery.Document // parsed once by the engine, shared read-only
	RedirectChain []string
	TimingMs      int64
	FetchedAt     time.Time
	FromCache     bool // true when served from the per-run memo
}
