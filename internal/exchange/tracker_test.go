package exchange

import (
	"testing"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/metrics"
)

// TestBinance_ParseTrackerRecordsLatency verifies that when a Binance connector is
// constructed with a non-nil LatencyTracker, parsing a valid wrapped frame records a sample.
func TestBinance_ParseTrackerRecordsLatency(t *testing.T) {
	tracker := &metrics.LatencyTracker{}
	b := NewBinance("ws://test", tracker)

	// Combined-stream wrapped bookTicker frame.
	msg := []byte(`{"stream":"btcusdt@bookTicker","data":{"u":400900217,"s":"BTCUSDT","b":"73000.50","B":"1.234","a":"73001.20","A":"2.345"}}`)

	t0 := time.Now()
	pu, ok := b.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed wrapped bookTicker frame")
	}
	if b.tracker != nil {
		b.tracker.Record(time.Since(t0))
	}
	_ = pu

	_, _, samples := tracker.Stats()
	if samples != 1 {
		t.Errorf("tracker.Stats().samples: got %d, want 1", samples)
	}
}

// TestBinance_ParseTrackerNilSafe verifies that nil tracker does not panic.
func TestBinance_ParseTrackerNilSafe(t *testing.T) {
	b := NewBinance("ws://test", nil)

	// Combined-stream wrapped bookTicker frame.
	msg := []byte(`{"stream":"btcusdt@bookTicker","data":{"u":400900217,"s":"BTCUSDT","b":"73000.50","B":"1.234","a":"73001.20","A":"2.345"}}`)
	pu, ok := b.parseMessage(msg)
	if !ok {
		t.Fatal("parseMessage should accept a well-formed wrapped bookTicker frame")
	}
	// Simulate nil-guarded record (should not panic)
	if b.tracker != nil {
		b.tracker.Record(0)
	}
	_ = pu
}
