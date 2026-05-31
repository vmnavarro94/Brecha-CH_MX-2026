package backtest

import (
	"sync"
	"time"
)

// ReplayClock is a clock whose current time is controlled by the replay loop.
// It satisfies types.Clock. RWMutex is used because Now() is on the hot path
// and many goroutines may read concurrently.
type ReplayClock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewReplayClock returns a ReplayClock initialised to zero time.
func NewReplayClock() *ReplayClock {
	return &ReplayClock{}
}

// Set advances (or resets) the clock to t. Called by the replay loop.
func (c *ReplayClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// Now returns the most recently set time. Satisfies types.Clock.
func (c *ReplayClock) Now() time.Time {
	c.mu.RLock()
	t := c.now
	c.mu.RUnlock()
	return t
}
