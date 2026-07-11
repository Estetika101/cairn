package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyTurnstile_EmptyTokenFailsWithoutCallingOut(t *testing.T) {
	// No token submitted at all — must fail immediately, no network call
	// (there's no real secret/endpoint here, so a network call would error,
	// proving this path returns before attempting one).
	ok, err := verifyTurnstile(context.Background(), "fake-secret", "", "203.0.113.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("empty token should never verify as success")
	}
}

func withMockTurnstile(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := turnstileVerifyURL
	turnstileVerifyURL = srv.URL
	t.Cleanup(func() { turnstileVerifyURL = old })
}

func TestVerifyTurnstile_Success(t *testing.T) {
	withMockTurnstile(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("secret") != "s3cr3t" || r.FormValue("response") != "good-token" {
			t.Errorf("unexpected form values: secret=%q response=%q", r.FormValue("secret"), r.FormValue("response"))
		}
		json.NewEncoder(w).Encode(turnstileVerifyResponse{Success: true})
	})
	ok, err := verifyTurnstile(context.Background(), "s3cr3t", "good-token", "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected success")
	}
}

func TestVerifyTurnstile_RejectedToken(t *testing.T) {
	withMockTurnstile(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(turnstileVerifyResponse{Success: false, ErrorCodes: []string{"invalid-input-response"}})
	})
	ok, err := verifyTurnstile(context.Background(), "s3cr3t", "bad-token", "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected failure for a rejected token")
	}
}
