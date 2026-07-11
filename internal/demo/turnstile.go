package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// turnstileVerifyURL is a var, not a const, so tests can point it at a local
// httptest server instead of a real network call to Cloudflare.
var turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// verifyTurnstile checks a visitor-submitted Cloudflare Turnstile token.
// Callers only invoke this when a secret key is configured — an empty secret
// means Turnstile is off entirely (e.g. local dev), not "always fail closed."
// A missing/empty token is treated as a normal verification failure, not a
// separate error path, so callers have one branch to handle.
func verifyTurnstile(ctx context.Context, secretKey, token, remoteIP string) (bool, error) {
	if token == "" {
		return false, nil
	}
	form := url.Values{"secret": {secretKey}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("turnstile verify request failed: %w", err)
	}
	defer resp.Body.Close()

	var result turnstileVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("turnstile verify response: %w", err)
	}
	return result.Success, nil
}
