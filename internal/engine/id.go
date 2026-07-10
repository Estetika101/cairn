package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
)

// AssignIDs stamps each finding with a content-hash ID:
//
//	sha1(module \n criterion \n normalize(location.url) \n selector)[:10]
//
// location.affectedUrls is deliberately NOT part of the hash, so a site-scoped
// finding's identity is stable as referrers come and go across runs (v0.4 §6b).
// The uniqueness invariant — (module, criterion, normalize(url), selector) is
// unique per finding — is the caller's responsibility (checks distinguish
// otherwise-colliding findings via distinct criteria).
func AssignIDs(findings []model.Finding) {
	for i := range findings {
		f := &findings[i]
		f.ID = findingID(f.Module, f.Criterion, normalizeIDURL(f.Location.URL), f.Location.Selector)
	}
}

func findingID(module, criterion, normURL, selector string) string {
	sum := sha1.Sum([]byte(module + "\n" + criterion + "\n" + normURL + "\n" + selector))
	return hex.EncodeToString(sum[:])[:10]
}

// normalizeIDURL is the finding-ID normalization: lowercase scheme+host, drop
// default ports, and KEEP path, query, AND fragment. This is intentionally
// different from the fetch-cache canonicalization (which drops the fragment) —
// two findings on the same page but different fragments must not collide.
func normalizeIDURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}
	out := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out
}
