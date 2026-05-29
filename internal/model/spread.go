package model

import "math"

const (
	// MinSamples is the minimum number of samples before the model is considered ready.
	MinSamples = 100
	// WindowSize is the maximum number of samples kept in the ring buffer.
	WindowSize = 500
)

// SpreadModel maintains a running mean and variance using the Welford one-pass algorithm
// over a fixed-size ring buffer of the most recent WindowSize samples.
type SpreadModel struct {
	// Welford running state
	n  int
	mu float64 // running mean
	m2 float64 // sum of squared deviations

	// Ring buffer
	buf []float64
	pos int  // next write position (circular)
	len int  // current number of elements in the buffer
}

// NewSpreadModel creates a new SpreadModel ready to receive samples.
func NewSpreadModel() *SpreadModel {
	return &SpreadModel{
		buf: make([]float64, WindowSize),
	}
}

// Update adds a new sample to the model.
// If the buffer is at capacity the oldest sample is evicted before the new one is added.
func (m *SpreadModel) Update(x float64) {
	if m.len == WindowSize {
		// Evict the oldest sample from Welford state using reverse Welford.
		old := m.buf[m.pos]
		m.n--
		if m.n == 0 {
			m.mu = 0
			m.m2 = 0
		} else {
			oldMu := (float64(m.n+1)*m.mu - old) / float64(m.n)
			m.m2 -= (old - m.mu) * (old - oldMu)
			m.mu = oldMu
		}
		// Overwrite oldest slot.
		m.buf[m.pos] = x
		m.pos = (m.pos + 1) % WindowSize
		// Add new sample to Welford state.
		m.n++
		delta := x - m.mu
		m.mu += delta / float64(m.n)
		delta2 := x - m.mu
		m.m2 += delta * delta2
	} else {
		// Buffer not yet full.
		m.buf[(m.pos+m.len)%WindowSize] = x
		m.len++
		// Add to Welford state.
		m.n++
		delta := x - m.mu
		m.mu += delta / float64(m.n)
		delta2 := x - m.mu
		m.m2 += delta * delta2
	}
}

// Mean returns the current running mean.
func (m *SpreadModel) Mean() float64 {
	return m.mu
}

// Std returns the population standard deviation of the current window.
func (m *SpreadModel) Std() float64 {
	if m.n < 2 {
		return 0
	}
	variance := m.m2 / float64(m.n)
	if variance < 0 {
		return 0
	}
	return math.Sqrt(variance)
}

// ZScore returns (x - mean) / std. Returns 0 when std == 0 to avoid divide-by-zero.
func (m *SpreadModel) ZScore(x float64) float64 {
	std := m.Std()
	if std == 0 {
		return 0
	}
	return (x - m.mu) / std
}

// IsReady returns true when at least MinSamples samples have been ingested.
func (m *SpreadModel) IsReady() bool {
	return m.n >= MinSamples
}
