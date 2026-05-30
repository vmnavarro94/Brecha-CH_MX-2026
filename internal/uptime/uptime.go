// Package uptime tracks per-exchange WS connection availability over the session.
// Each Sample call records a 1-bit observation (fresh = true, stale = false).
// UptimePct returns fresh_count / total_count for that exchange.
package uptime

import "sync"

// Tracker accumulates fresh/stale samples per exchange across the session.
// Safe for concurrent use.
type Tracker struct {
	mu      sync.RWMutex
	samples map[string]*counter
}

type counter struct {
	total int64
	fresh int64
}

// NewTracker creates an empty Tracker ready to receive samples.
func NewTracker() *Tracker {
	return &Tracker{samples: make(map[string]*counter)}
}

// Sample records one observation for the given exchange.
// fresh = true means the WS feed is delivering recent updates at sample time.
func (t *Tracker) Sample(exchange string, fresh bool) {
	t.mu.Lock()
	c, ok := t.samples[exchange]
	if !ok {
		c = &counter{}
		t.samples[exchange] = c
	}
	c.total++
	if fresh {
		c.fresh++
	}
	t.mu.Unlock()
}

// UptimePct returns the share of fresh samples for the given exchange in [0,1].
// Returns 0 if no samples have been recorded.
func (t *Tracker) UptimePct(exchange string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	c, ok := t.samples[exchange]
	if !ok || c.total == 0 {
		return 0
	}
	return float64(c.fresh) / float64(c.total)
}

// Snapshot returns a copy of per-exchange uptime percentages at this moment.
func (t *Tracker) Snapshot() map[string]float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]float64, len(t.samples))
	for ex, c := range t.samples {
		if c.total == 0 {
			out[ex] = 0
			continue
		}
		out[ex] = float64(c.fresh) / float64(c.total)
	}
	return out
}
