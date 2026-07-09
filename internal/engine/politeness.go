package engine

import (
	"sync"
	"time"
)

// limiter composes the two politeness knobs that v0.4 §7c keeps separate: a
// GLOBAL in-flight cap across all hosts (maxConcurrentRequests) and a per-host
// gate (concurrency + minimum delay). Every request — crawl, check Fetch, or
// robots.txt — passes through it, so third-party hosts are throttled exactly
// like the audited origin.
type limiter struct {
	global      chan struct{}
	perHostConc int
	delay       time.Duration

	mu    sync.Mutex
	hosts map[string]*hostGate
}

type hostGate struct {
	tokens chan struct{}
	mu     sync.Mutex
	next   time.Time // earliest wall-clock time the next request to this host may start
}

func newLimiter(maxConcurrent, perHostConc, delayMs int) *limiter {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if perHostConc < 1 {
		perHostConc = 1
	}
	return &limiter{
		global:      make(chan struct{}, maxConcurrent),
		perHostConc: perHostConc,
		delay:       time.Duration(delayMs) * time.Millisecond,
		hosts:       map[string]*hostGate{},
	}
}

func (l *limiter) gate(host string) *hostGate {
	l.mu.Lock()
	defer l.mu.Unlock()
	g, ok := l.hosts[host]
	if !ok {
		g = &hostGate{tokens: make(chan struct{}, l.perHostConc)}
		l.hosts[host] = g
	}
	return g
}

// acquire blocks until this request is allowed to hit the host, then returns a
// release func. It reserves monotonically increasing per-host time slots spaced
// by delay, so N successive same-host requests start >= delay apart even when
// per-host concurrency would otherwise let them race.
func (l *limiter) acquire(host string) func() {
	l.global <- struct{}{}
	g := l.gate(host)
	g.tokens <- struct{}{}

	g.mu.Lock()
	now := time.Now()
	start := now
	if g.next.After(now) {
		start = g.next
	}
	g.next = start.Add(l.delay)
	g.mu.Unlock()

	if d := time.Until(start); d > 0 {
		time.Sleep(d)
	}

	return func() {
		<-g.tokens
		<-l.global
	}
}
