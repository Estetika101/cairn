// Package security holds the security-header checks. In the slice this is the
// simplest possible page-scoped check: a pure function of the response headers,
// included to prove the page path end to end.
package security

import (
	"net/http"
	"strings"

	"github.com/Estetika101/verdict/internal/model"
)

// headersCheck flags missing security response headers. Severity is fixed per
// criterion (per v0.4 §6b): missing HSTS/CSP is always error; the advisory
// headers are always warn.
type headersCheck struct{}

// New returns the security-headers check.
func New() model.Check { return headersCheck{} }

func (headersCheck) Meta() model.CheckMeta {
	return model.CheckMeta{
		ID:       "security-headers",
		Module:   "security",
		Tier:     1,
		Scope:    model.ScopePage,
		Severity: model.SeverityError, // module default / fallback
		Title:    "Security response headers",
	}
}

func (headersCheck) Run(cc model.CheckContext) ([]model.Finding, error) {
	page := cc.Page()
	h := page.Headers
	loc := model.Location{URL: pageURL(page)}
	https := strings.HasPrefix(strings.ToLower(pageURL(page)), "https://")

	var out []model.Finding

	// HSTS is only meaningful over https; not applicable otherwise.
	if https {
		out = append(out, presence(h, "Strict-Transport-Security", model.SeverityError, loc,
			"Strict-Transport-Security header present",
			"Add Strict-Transport-Security: max-age=31536000; includeSubDomains to the server/CDN config."))
	}

	out = append(out, presence(h, "Content-Security-Policy", model.SeverityError, loc,
		"Content-Security-Policy header present",
		"Add a Content-Security-Policy header (start report-only, then enforce)."))

	out = append(out, nosniff(h, loc))
	out = append(out, framing(h, loc))

	out = append(out, presence(h, "Referrer-Policy", model.SeverityWarn, loc,
		"Referrer-Policy header present",
		"Add a Referrer-Policy header, e.g. strict-origin-when-cross-origin."))

	return out, nil
}

// presence emits pass if the header is present, else a fail at the given severity.
func presence(h http.Header, name string, sev model.Severity, loc model.Location, required, fix string) model.Finding {
	f := base(name, loc)
	if strings.TrimSpace(h.Get(name)) != "" {
		f.Status = model.StatusPass
		return f
	}
	f.Status = model.StatusFail
	f.Severity = sev
	f.Observed = name + " header absent"
	f.Required = required
	f.SuggestedFix = fix
	return f
}

// nosniff requires X-Content-Type-Options: nosniff exactly.
func nosniff(h http.Header, loc model.Location) model.Finding {
	f := base("X-Content-Type-Options", loc)
	v := strings.ToLower(strings.TrimSpace(h.Get("X-Content-Type-Options")))
	if v == "nosniff" {
		f.Status = model.StatusPass
		return f
	}
	f.Status = model.StatusFail
	f.Severity = model.SeverityWarn
	if v == "" {
		f.Observed = "X-Content-Type-Options header absent"
	} else {
		f.Observed = "X-Content-Type-Options: " + h.Get("X-Content-Type-Options")
	}
	f.Required = "X-Content-Type-Options: nosniff"
	f.SuggestedFix = "Add X-Content-Type-Options: nosniff to the server/CDN config."
	return f
}

// framing passes if X-Frame-Options is set OR the CSP includes frame-ancestors.
func framing(h http.Header, loc model.Location) model.Finding {
	f := base("X-Frame-Options / frame-ancestors", loc)
	xfo := strings.TrimSpace(h.Get("X-Frame-Options")) != ""
	csp := strings.Contains(strings.ToLower(h.Get("Content-Security-Policy")), "frame-ancestors")
	if xfo || csp {
		f.Status = model.StatusPass
		return f
	}
	f.Status = model.StatusFail
	f.Severity = model.SeverityWarn
	f.Observed = "neither X-Frame-Options nor CSP frame-ancestors present"
	f.Required = "X-Frame-Options or a CSP frame-ancestors directive"
	f.SuggestedFix = "Add X-Frame-Options: DENY (or SAMEORIGIN), or a CSP frame-ancestors directive."
	return f
}

func base(criterion string, loc model.Location) model.Finding {
	return model.Finding{
		Module:    "security",
		Scope:     model.ScopePage,
		Criterion: criterion,
		Location:  loc,
	}
}

func pageURL(p model.PageData) string {
	if p.FinalURL != "" {
		return p.FinalURL
	}
	return p.RequestedURL
}
