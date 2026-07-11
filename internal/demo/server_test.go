package demo

import (
	"net/http"
	"testing"
)

// Regression: RemoteAddr's ephemeral port must be stripped, or the rate
// limiter keys on a value that's different on every single connection from
// the same client — silently disabling rate limiting entirely. Caught live:
// six curl requests in a row from one machine all sailed through as "200 OK"
// because each one carried a different source port.
func TestClientIP_StripsPortFromRemoteAddr(t *testing.T) {
	r1 := &http.Request{RemoteAddr: "203.0.113.5:51234", Header: http.Header{}}
	r2 := &http.Request{RemoteAddr: "203.0.113.5:60000", Header: http.Header{}}
	ip1, ip2 := clientIP(r1), clientIP(r2)
	if ip1 != ip2 {
		t.Fatalf("clientIP should ignore ephemeral port so repeat requests share a key: got %q and %q", ip1, ip2)
	}
	if ip1 != "203.0.113.5" {
		t.Errorf("clientIP = %q, want the bare IP 203.0.113.5", ip1)
	}
}

func TestClientIP_PrefersFlyHeader(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "10.0.0.1:1234", // Fly's internal proxy hop
		Header:     http.Header{"Fly-Client-Ip": []string{"198.51.100.7"}},
	}
	if got := clientIP(r); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want the Fly-Client-IP value", got)
	}
}

func TestClientIP_FallsBackToXForwardedFor(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "10.0.0.1:1234",
		Header:     http.Header{"X-Forwarded-For": []string{"198.51.100.7, 10.0.0.2"}},
	}
	if got := clientIP(r); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want the first X-Forwarded-For entry", got)
	}
}

func TestNormalizeSubmittedURL(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"example.com", "https://example.com", true},
		{"https://example.com/page", "https://example.com/page", true},
		{"  example.com  ", "https://example.com", true},
		{"", "", false},
		{"ftp://example.com", "", false},
		{"not a url at all !!", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeSubmittedURL(tc.in)
		if ok != tc.wantOK {
			t.Errorf("normalizeSubmittedURL(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("normalizeSubmittedURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
