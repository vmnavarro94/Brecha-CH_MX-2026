package uptime

import (
	"testing"
)

// TestTracker_AllFresh: 10 samples, all fresh → uptime = 1.0
func TestTracker_AllFresh(t *testing.T) {
	tr := NewTracker()
	for i := 0; i < 10; i++ {
		tr.Sample("binance", true)
	}
	got := tr.UptimePct("binance")
	if got != 1.0 {
		t.Errorf("UptimePct(all fresh): got %v, want 1.0", got)
	}
}

// TestTracker_HalfStale: 10 samples, 5 fresh + 5 stale → uptime = 0.5
func TestTracker_HalfStale(t *testing.T) {
	tr := NewTracker()
	for i := 0; i < 5; i++ {
		tr.Sample("binance", true)
	}
	for i := 0; i < 5; i++ {
		tr.Sample("binance", false)
	}
	got := tr.UptimePct("binance")
	if got != 0.5 {
		t.Errorf("UptimePct(half stale): got %v, want 0.5", got)
	}
}

// TestTracker_NoSamples: no samples → uptime = 0
func TestTracker_NoSamples(t *testing.T) {
	tr := NewTracker()
	got := tr.UptimePct("unknown")
	if got != 0.0 {
		t.Errorf("UptimePct(no samples): got %v, want 0.0", got)
	}
}

// TestTracker_Snapshot: returns map of all exchange uptimes
func TestTracker_Snapshot(t *testing.T) {
	tr := NewTracker()
	tr.Sample("binance", true)
	tr.Sample("binance", true)
	tr.Sample("kraken", true)
	tr.Sample("kraken", false)

	snap := tr.Snapshot()
	if snap["binance"] != 1.0 {
		t.Errorf("Snapshot binance: got %v, want 1.0", snap["binance"])
	}
	if snap["kraken"] != 0.5 {
		t.Errorf("Snapshot kraken: got %v, want 0.5", snap["kraken"])
	}
}

// TestTracker_ConcurrentSafe: parallel samples must not race.
func TestTracker_ConcurrentSafe(t *testing.T) {
	tr := NewTracker()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				tr.Sample("binance", j%2 == 0)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	got := tr.UptimePct("binance")
	// 400 samples, half true → ~0.5
	if got < 0.45 || got > 0.55 {
		t.Errorf("UptimePct(concurrent): got %v, want ~0.5", got)
	}
}
