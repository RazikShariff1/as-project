// Package ratelimit is a small fixed-window limiter, used to slow down password guessing from one address.
package ratelimit

import (
	"sync"
	"time"
)

const (
	window     = time.Minute
	sweepAbove = 10000 // sweep expired windows once this many keys are tracked
)

type bucket struct {
	start time.Time
	count int
}

// Limiter allows a fixed number of events per key per minute. It is in memory, so with several gateway instances each
// enforces the limit on its own; the account service's per-account lockout is the shared backstop.
type Limiter struct {
	mu    sync.Mutex
	limit int
	now   func() time.Time
	keys  map[string]*bucket
}

// New returns a limiter allowing limit events per key per minute.
func New(limit int) *Limiter {
	return &Limiter{limit: limit, now: time.Now, keys: map[string]*bucket{}}
}

// Allow records an event for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	if len(l.keys) > sweepAbove {
		for k, b := range l.keys {
			if now.Sub(b.start) >= window {
				delete(l.keys, k)
			}
		}
	}

	b, ok := l.keys[key]
	if !ok || now.Sub(b.start) >= window {
		b = &bucket{start: now}
		l.keys[key] = b
	}

	b.count++

	return b.count <= l.limit
}
