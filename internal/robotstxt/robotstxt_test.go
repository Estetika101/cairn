package robotstxt_test

import (
	"testing"

	"github.com/Estetika101/verdict/internal/robotstxt"
)

func TestAllowDisallow(t *testing.T) {
	body := []byte("User-agent: *\nDisallow: /private\n")
	r := robotstxt.Parse(body, "verdict")
	if r.Allowed("/private/x") {
		t.Error("/private/x should be disallowed")
	}
	if !r.Allowed("/public") {
		t.Error("/public should be allowed (no matching rule)")
	}
}

func TestSpecificGroupBeatsStar(t *testing.T) {
	body := []byte("User-agent: *\nDisallow: /\n\nUser-agent: GPTBot\nAllow: /\n")
	if !robotstxt.Parse(body, "GPTBot").Allowed("/anything") {
		t.Error("GPTBot has its own group allowing everything; should win over the * disallow-all")
	}
	if robotstxt.Parse(body, "ClaudeBot").Allowed("/anything") {
		t.Error("ClaudeBot has no specific group; should fall back to * disallow-all")
	}
}

func TestLongestMatchWins(t *testing.T) {
	body := []byte("User-agent: *\nDisallow: /\nAllow: /public/\n")
	r := robotstxt.Parse(body, "verdict")
	if !r.Allowed("/public/page") {
		t.Error("/public/page: the longer, more specific Allow should win over the blanket Disallow")
	}
	if r.Allowed("/private") {
		t.Error("/private should still be disallowed")
	}
}

func TestAllowWinsExactTie(t *testing.T) {
	body := []byte("User-agent: *\nDisallow: /x\nAllow: /x\n")
	if !robotstxt.Parse(body, "verdict").Allowed("/x") {
		t.Error("on an exact-length tie, Allow should win per the standard resolution")
	}
}

func TestEmptyBodyAllowsAll(t *testing.T) {
	if !robotstxt.Parse(nil, "verdict").Allowed("/anything") {
		t.Error("no robots.txt content should mean allow-all")
	}
}

func TestCommentsAndCaseInsensitiveDirectives(t *testing.T) {
	body := []byte("# comment line\nUSER-AGENT: *\nDISALLOW: /admin # trailing comment\n")
	r := robotstxt.Parse(body, "verdict")
	if r.Allowed("/admin/panel") {
		t.Error("directive keys should be matched case-insensitively, and comments stripped")
	}
}

func TestProductToken(t *testing.T) {
	cases := map[string]string{
		"verdict/0.1 (+https://example.com)": "verdict",
		"GPTBot":                             "GPTBot",
		"":                                   "*",
	}
	for ua, want := range cases {
		if got := robotstxt.ProductToken(ua); got != want {
			t.Errorf("ProductToken(%q) = %q, want %q", ua, got, want)
		}
	}
}
