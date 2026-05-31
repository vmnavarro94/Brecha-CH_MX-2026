package sizing

import "sync"

// KellyEstimator tracks trade returns using Welford's online algorithm and
// computes a fractional Kelly position size. Thread-safe via RWMutex.
type KellyEstimator struct {
	mu      sync.RWMutex
	n       int
	mean    float64
	m2      float64
	minN    int
	fracCap float64
}

// New creates a KellyEstimator that requires at least minSamples before returning
// a non-zero fraction, and caps the result at fracCap.
func New(minSamples int, fracCap float64) *KellyEstimator {
	return &KellyEstimator{
		minN:    minSamples,
		fracCap: fracCap,
	}
}

// Record adds a new observation using Welford's online mean/variance update.
// Safe for concurrent use.
func (k *KellyEstimator) Record(netPct float64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.n++
	delta := netPct - k.mean
	k.mean += delta / float64(k.n)
	k.m2 += delta * (netPct - k.mean)
}

// Fraction returns the fractional Kelly position size.
//
// Returns 0 when:
//   - Samples() < minN (cold-start guard)
//   - population variance is zero or negative (degenerate distribution)
//   - raw Kelly fraction is negative (mean <= 0)
//
// The result is capped at fracCap (full Kelly is rarely used in practice).
func (k *KellyEstimator) Fraction() float64 {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if k.n < k.minN {
		return 0
	}
	v := k.m2 / float64(k.n) // population variance
	if v <= 0 {
		return 0
	}
	raw := k.mean / v
	if raw < 0 {
		return 0
	}
	if raw < k.fracCap {
		return raw
	}
	return k.fracCap
}

// Samples returns the number of observations recorded so far.
// Safe for concurrent use.
func (k *KellyEstimator) Samples() int {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.n
}
