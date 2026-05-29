package model

import (
	"math"
	"math/big"
	"testing"
)

// batchMean computes the mean of a slice of float64.
func batchMean(vals []float64) float64 {
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// batchStd computes the population standard deviation using math/big for precision.
func batchStd(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	prec := uint(256)
	sum := new(big.Float).SetPrec(prec)
	for _, v := range vals {
		sum.Add(sum, new(big.Float).SetPrec(prec).SetFloat64(v))
	}
	n := new(big.Float).SetPrec(prec).SetInt64(int64(len(vals)))
	mean := new(big.Float).SetPrec(prec).Quo(sum, n)

	var sumSq = new(big.Float).SetPrec(prec)
	for _, v := range vals {
		diff := new(big.Float).SetPrec(prec).Sub(new(big.Float).SetPrec(prec).SetFloat64(v), mean)
		sq := new(big.Float).SetPrec(prec).Mul(diff, diff)
		sumSq.Add(sumSq, sq)
	}
	variance := new(big.Float).SetPrec(prec).Quo(sumSq, n)
	varF64, _ := variance.Float64()
	return math.Sqrt(varF64)
}

// TestFirstSample verifies that adding a single sample initializes state correctly.
func TestFirstSample(t *testing.T) {
	m := NewSpreadModel()
	m.Update(42.0)

	if got := m.Mean(); math.Abs(got-42.0) > 1e-9 {
		t.Errorf("Mean after first sample: got %v, want 42.0", got)
	}
	if got := m.Std(); got != 0.0 {
		t.Errorf("Std after first sample: got %v, want 0.0", got)
	}
	if m.IsReady() {
		t.Error("IsReady should be false after 1 sample")
	}
}

// TestIsReadyAt99 verifies IsReady is false with 99 samples.
func TestIsReadyAt99(t *testing.T) {
	m := NewSpreadModel()
	for i := 0; i < 99; i++ {
		m.Update(float64(i))
	}
	if m.IsReady() {
		t.Error("IsReady should be false at 99 samples")
	}
}

// TestIsReadyAt100 verifies IsReady is true with exactly 100 samples.
func TestIsReadyAt100(t *testing.T) {
	m := NewSpreadModel()
	for i := 0; i < 100; i++ {
		m.Update(float64(i))
	}
	if !m.IsReady() {
		t.Error("IsReady should be true at 100 samples")
	}
}

// TestWelfordConvergence feeds 500 known values and verifies Mean and Std match batch calculation.
func TestWelfordConvergence(t *testing.T) {
	const n = 500
	vals := make([]float64, n)
	m := NewSpreadModel()
	for i := 0; i < n; i++ {
		vals[i] = float64(i+1) * 1.5
		m.Update(vals[i])
	}

	wantMean := batchMean(vals)
	wantStd := batchStd(vals)
	const tol = 1e-6

	if got := m.Mean(); math.Abs(got-wantMean) > tol {
		t.Errorf("Mean after 500 samples: got %v, want %v (diff %v)", got, wantMean, math.Abs(got-wantMean))
	}
	if got := m.Std(); math.Abs(got-wantStd) > tol {
		t.Errorf("Std after 500 samples: got %v, want %v (diff %v)", got, wantStd, math.Abs(got-wantStd))
	}
}

// TestRingBufferEviction verifies that after 501 samples the window stays at 500
// and the oldest sample is no longer part of the statistics.
func TestRingBufferEviction(t *testing.T) {
	m := NewSpreadModel()

	// Fill the ring buffer.
	for i := 0; i < 500; i++ {
		m.Update(float64(i + 1))
	}

	// Snapshot mean and std for the first 500 samples.
	meanBefore := m.Mean()

	// Add one more sample, which should evict sample #1 (value=1.0).
	m.Update(501.0)

	meanAfter := m.Mean()

	// After eviction, mean for samples [2..501] is (2+501)/2 = 251.5
	wantMeanAfter := batchMean(makeRange(2, 502))
	const tol = 1e-6
	if math.Abs(meanAfter-wantMeanAfter) > tol {
		t.Errorf("Mean after eviction: got %v, want %v (was %v)", meanAfter, wantMeanAfter, meanBefore)
	}
}

// makeRange generates a slice [start, start+1, ..., start+count-1] as float64.
func makeRange(start, end int) []float64 {
	out := make([]float64, end-start)
	for i := range out {
		out[i] = float64(start + i)
	}
	return out
}

// TestZScoreFormula verifies the z-score formula z = (x - mean) / std.
func TestZScoreFormula(t *testing.T) {
	m := NewSpreadModel()
	// Feed 100 samples of fixed value 10.0 then vary to get a non-zero std.
	for i := 0; i < 100; i++ {
		m.Update(10.0)
	}
	// Now add variation so std > 0.
	for i := 0; i < 100; i++ {
		m.Update(float64(i))
	}
	mean := m.Mean()
	std := m.Std()
	x := mean + 2*std
	want := 2.0
	if std == 0 {
		t.Skip("std is zero, cannot test z-score formula")
	}
	got := m.ZScore(x)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("ZScore(%v): got %v, want %v", x, got, want)
	}
}

// TestZScoreZeroVarianceGuard verifies that ZScore returns 0 when all samples are identical.
func TestZScoreZeroVarianceGuard(t *testing.T) {
	m := NewSpreadModel()
	for i := 0; i < 100; i++ {
		m.Update(5.0)
	}
	if !m.IsReady() {
		t.Fatal("model should be ready after 100 samples")
	}
	got := m.ZScore(5.0)
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("ZScore with zero std returned non-finite %v", got)
	}
	if got != 0.0 {
		t.Errorf("ZScore with zero std: got %v, want 0.0", got)
	}
}
