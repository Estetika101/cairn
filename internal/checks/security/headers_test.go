package security

import (
	"context"
	"net/http"
	"testing"

	"github.com/Estetika101/cairn/internal/model"
	"github.com/Estetika101/cairn/internal/report"
)

// stubCtx is a page-scoped CheckContext over a synthetic PageData — enough to
// unit-test a pure header check without any network.
type stubCtx struct{ page model.PageData }

func (s stubCtx) Scope() model.Scope                                    { return model.ScopePage }
func (s stubCtx) Page() model.PageData                                  { return s.page }
func (s stubCtx) Corpus() []model.PageData                              { return nil }
func (s stubCtx) Config() model.CheckConfig                             { return model.CheckConfig{} }
func (s stubCtx) Logf(string, ...any)                                   {}
func (s stubCtx) Fetch(context.Context, string) (model.PageData, error) { return model.PageData{}, nil }

func page(headers map[string]string) model.PageData {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return model.PageData{FinalURL: "https://example.test/", Headers: h}
}

func byCriterion(fs []model.Finding) map[string]model.Finding {
	m := map[string]model.Finding{}
	for _, f := range fs {
		m[f.Criterion] = f
	}
	return m
}

// Row 1: full security headers over https -> 5 pass, no severities.
func TestSecurityHeaders_AllPass(t *testing.T) {
	fs, err := New().Run(stubCtx{page(map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Content-Security-Policy":   "default-src 'self'",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 5 {
		t.Fatalf("got %d findings, want 5", len(fs))
	}
	for _, f := range fs {
		if f.Status != model.StatusPass {
			t.Errorf("%s: status %s, want pass", f.Criterion, f.Status)
		}
		if f.Severity != "" {
			t.Errorf("%s: pass should carry no severity, got %s", f.Criterion, f.Severity)
		}
	}
}

// Row 2: missing HSTS + Referrer-Policy -> fail/error and fail/warn; the report
// exits non-zero under failOn: error.
func TestSecurityHeaders_MissingHSTSAndReferrer(t *testing.T) {
	fs, err := New().Run(stubCtx{page(map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "SAMEORIGIN",
	})})
	if err != nil {
		t.Fatal(err)
	}
	by := byCriterion(fs)

	hsts := by["Strict-Transport-Security"]
	if hsts.Status != model.StatusFail || hsts.Severity != model.SeverityError {
		t.Errorf("HSTS = %s/%s, want fail/error", hsts.Status, hsts.Severity)
	}
	ref := by["Referrer-Policy"]
	if ref.Status != model.StatusFail || ref.Severity != model.SeverityWarn {
		t.Errorf("Referrer-Policy = %s/%s, want fail/warn", ref.Status, ref.Severity)
	}
	if by["Content-Security-Policy"].Status != model.StatusPass {
		t.Errorf("CSP should pass when present")
	}

	rep := model.Report{Sites: []model.SiteReport{{Findings: fs}}}
	if code := report.ExitCode(rep, "error"); code == 0 {
		t.Errorf("exit code = 0, want non-zero (HSTS error present)")
	}
	if code := report.ExitCode(rep, "off"); code != 0 {
		t.Errorf("exit code with failOn:off = %d, want 0", code)
	}
}
