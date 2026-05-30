package engine

import (
	"math"
	"sort"
	"sync"
	"time"
)

const latencyRingSize = 1024

// LatencyTracker records the elapsed time of ProcessUpdate calls in a fixed-size
// ring buffer and computes p50/p99 percentiles on demand.
type LatencyTracker struct {
	mu    sync.RWMutex
	ring  [latencyRingSize]int64 // nanoseconds
	idx   int                    // next write slot [0, latencyRingSize)
	count int                    // capped at latencyRingSize
}

// Record stores elapsed into the ring buffer. Safe for concurrent use.
func (t *LatencyTracker) Record(elapsed time.Duration) {
	t.mu.Lock()
	t.ring[t.idx] = int64(elapsed)
	t.idx = (t.idx + 1) % latencyRingSize
	if t.count < latencyRingSize {
		t.count++
	}
	t.mu.Unlock()
}

// Stats returns (p50, p99, samples). Computation happens outside the lock using
// a scratch copy so the hot path is not blocked. Returns zeros when count == 0.
func (t *LatencyTracker) Stats() (p50, p99 time.Duration, samples int) {
	t.mu.RLock()
	n := t.count
	if n == 0 {
		t.mu.RUnlock()
		return 0, 0, 0
	}
	scratch := make([]int64, n)
	copy(scratch, t.ring[:n])
	t.mu.RUnlock()

	sort.Slice(scratch, func(i, j int) bool { return scratch[i] < scratch[j] })
	return time.Duration(scratch[percentileIndex(n, 0.50)]),
		time.Duration(scratch[percentileIndex(n, 0.99)]),
		n
}

// percentileIndex returns the nearest-rank index for percentile p in a slice of
// length n. Formula: ceil(p*n) - 1, clamped to [0, n-1].
func percentileIndex(n int, p float64) int {
	i := int(math.Ceil(p*float64(n))) - 1
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
