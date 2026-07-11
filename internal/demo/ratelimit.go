package demo

import (
	"sync"
	"time"
)

// rateLimiter is a simple per-key fixed-window counter: N requests allowed
// per key per window, reset when the window rolls over. That's the right
// amount of sophistication for a single-instance public demo endpoint — a
// token bucket or sliding-log limiter would be more precise, but this is
// trivial to reason about and sufficient to stop casual abuse of an
// unauthenticated "fetch a URL for me" endpoint.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	counts map[string]*windowCount
}

type windowCount struct {
	n           int
	windowStart time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, counts: map[string]*windowCount{}}
}

// Allow reports whether key (typically a client IP) may make another
// request right now, incrementing its count if so.
func (r *rateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	wc, ok := r.counts[key]
	if !ok || now.Sub(wc.windowStart) >= r.window {
		r.counts[key] = &windowCount{n: 1, windowStart: now}
		return true
	}
	if wc.n >= r.limit {
		return false
	}
	wc.n++
	return true
}

// sweep discards windows that have already expired, so the map doesn't grow
// unboundedly over the life of a long-running process. Call periodically
// (the server wires this to a background ticker), not per-request.
func (r *rateLimiter) sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, wc := range r.counts {
		if now.Sub(wc.windowStart) >= r.window {
			delete(r.counts, k)
		}
	}
}
