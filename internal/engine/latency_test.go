package engine

import (
	"sync"
	"testing"
	"time"
)

// TestLatencyTracker_Empty verifies that a fresh tracker returns zeros.
func TestLatencyTracker_Empty(t *testing.T) {
	lt := &LatencyTracker{}
	p50, p99, samples := lt.Stats()
	if p50 != 0 {
		t.Errorf("p50: got %v, want 0", p50)
	}
	if p99 != 0 {
		t.Errorf("p99: got %v, want 0", p99)
	}
	if samples != 0 {
		t.Errorf("samples: got %d, want 0", samples)
	}
}

// TestLatencyTracker_ColdStart verifies that fewer than 10 samples returns samples == 9.
func TestLatencyTracker_ColdStart(t *testing.T) {
	lt := &LatencyTracker{}
	for i := 0; i < 9; i++ {
		lt.Record(time.Duration(i+1) * time.Nanosecond)
	}
	_, _, samples := lt.Stats()
	if samples != 9 {
		t.Errorf("samples: got %d, want 9", samples)
	}
}

// TestLatencyTracker_Stats_KnownInput feeds values 1..1024 ns and expects
// p50 == 512ns, p99 == 1014ns, samples == 1024.
func TestLatencyTracker_Stats_KnownInput(t *testing.T) {
	lt := &LatencyTracker{}
	for i := 1; i <= 1024; i++ {
		lt.Record(time.Duration(i) * time.Nanosecond)
	}
	p50, p99, samples := lt.Stats()
	if samples != 1024 {
		t.Errorf("samples: got %d, want 1024", samples)
	}
	if p50 != 512*time.Nanosecond {
		t.Errorf("p50: got %v, want 512ns", p50)
	}
	if p99 != 1014*time.Nanosecond {
		t.Errorf("p99: got %v, want 1014ns", p99)
	}
}

// TestLatencyTracker_RingWrap records 1025 values and verifies samples stays at 1024.
func TestLatencyTracker_RingWrap(t *testing.T) {
	lt := &LatencyTracker{}
	for i := 0; i < 1025; i++ {
		lt.Record(time.Duration(i+1) * time.Nanosecond)
	}
	_, _, samples := lt.Stats()
	if samples != 1024 {
		t.Errorf("samples: got %d, want 1024 after ring wrap", samples)
	}
}

// TestLatencyTracker_Race exercises concurrent Record + Stats under the race detector.
func TestLatencyTracker_Race(t *testing.T) {
	lt := &LatencyTracker{}
	const workers = 8
	const iters = 500

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if i%2 == 0 {
					lt.Record(time.Duration(i+1) * time.Nanosecond)
				} else {
					lt.Stats()
				}
			}
		}(w)
	}
	wg.Wait()
}
