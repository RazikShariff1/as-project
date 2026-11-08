package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAllowsUpToTheLimitPerKeyPerWindow(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	l := New(3)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		assert.True(t, l.Allow("1.1.1.1"), "attempt %d", i+1)
	}

	assert.False(t, l.Allow("1.1.1.1"), "the fourth in the same minute")
	assert.True(t, l.Allow("2.2.2.2"), "another address has its own allowance")

	now = now.Add(59 * time.Second)
	assert.False(t, l.Allow("1.1.1.1"), "still the same window")

	now = now.Add(2 * time.Second)
	assert.True(t, l.Allow("1.1.1.1"), "a new window starts after a minute")
}

func TestSweepsExpiredKeys(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	l := New(1)
	l.now = func() time.Time { return now }

	for i := 0; i <= sweepAbove; i++ {
		l.Allow(string(rune(i)) + "k")
	}

	assert.Greater(t, len(l.keys), sweepAbove)

	now = now.Add(2 * window)
	l.Allow("fresh")
	assert.Less(t, len(l.keys), 10, "stale windows are dropped so the map cannot grow without bound")
}
