package demo

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToLimit(t *testing.T) {
	r := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !r.Allow("ip1") {
			t.Fatalf("request %d should be allowed (limit is 3)", i+1)
		}
	}
	if r.Allow("ip1") {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiter_SeparateKeysIndependent(t *testing.T) {
	r := newRateLimiter(1, time.Minute)
	if !r.Allow("ip1") {
		t.Error("ip1 first request should be allowed")
	}
	if !r.Allow("ip2") {
		t.Error("ip2 should have its own independent limit")
	}
	if r.Allow("ip1") {
		t.Error("ip1 second request should be denied")
	}
}

func TestRateLimiter_ResetsAfterWindow(t *testing.T) {
	r := newRateLimiter(1, 30*time.Millisecond)
	if !r.Allow("ip1") {
		t.Fatal("first request should be allowed")
	}
	if r.Allow("ip1") {
		t.Fatal("second request within the window should be denied")
	}
	time.Sleep(40 * time.Millisecond)
	if !r.Allow("ip1") {
		t.Error("request after the window rolled over should be allowed again")
	}
}

func TestRateLimiter_Sweep(t *testing.T) {
	r := newRateLimiter(1, 20*time.Millisecond)
	r.Allow("ip1")
	time.Sleep(30 * time.Millisecond)
	r.sweep()
	r.mu.Lock()
	_, exists := r.counts["ip1"]
	r.mu.Unlock()
	if exists {
		t.Error("expired window should have been swept")
	}
}
