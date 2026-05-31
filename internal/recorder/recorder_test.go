package recorder_test

import (
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// tempDir creates a temporary directory for the test and registers cleanup.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "recorder_test_*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestRecorder_RecordsAndQueriesFrames covers spec R1: single-frame round-trip
// and ordering of 100 frames.
func TestRecorder_RecordsAndQueriesFrames(t *testing.T) {
	dir := tempDir(t)
	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec.Close()

	base := time.Now().UTC().Truncate(time.Millisecond)

	// Single-frame round-trip.
	frame := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(50000),
		Ask:        decimal.NewFromFloat(50001),
		BidSize:    decimal.NewFromFloat(1.5),
		AskSize:    decimal.NewFromFloat(2.0),
		ReceivedAt: base,
	}
	if err := rec.RecordFrame(frame); err != nil {
		t.Fatalf("RecordFrame: %v", err)
	}

	frames, err := rec.QueryFrames(base.Add(-time.Second), base.Add(time.Second))
	if err != nil {
		t.Fatalf("QueryFrames: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(frames))
	}
	got := frames[0]
	if got.Exchange != frame.Exchange {
		t.Errorf("Exchange: want %q, got %q", frame.Exchange, got.Exchange)
	}
	if !got.Bid.Equal(frame.Bid) {
		t.Errorf("Bid: want %s, got %s", frame.Bid, got.Bid)
	}
	if !got.Ask.Equal(frame.Ask) {
		t.Errorf("Ask: want %s, got %s", frame.Ask, got.Ask)
	}
	if !got.ReceivedAt.Equal(base) {
		t.Errorf("ReceivedAt: want %v, got %v", base, got.ReceivedAt)
	}

	// Ordering guarantee: 100 frames spread over 1 hour.
	rec2, err := recorder.New(tempDir(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec2.Close()

	start := time.Now().UTC()
	for i := 0; i < 100; i++ {
		ts := start.Add(time.Duration(i) * 36 * time.Second) // spread over ~1h
		f := types.PriceUpdate{
			Exchange:   "binance",
			Bid:        decimal.NewFromFloat(float64(50000 + i)),
			Ask:        decimal.NewFromFloat(float64(50001 + i)),
			ReceivedAt: ts,
		}
		if err := rec2.RecordFrame(f); err != nil {
			t.Fatalf("RecordFrame i=%d: %v", i, err)
		}
	}

	result, err := rec2.QueryFrames(start.Add(-time.Second), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("QueryFrames: %v", err)
	}
	if len(result) != 100 {
		t.Fatalf("want 100 frames, got %d", len(result))
	}
	for i := 1; i < len(result); i++ {
		if result[i].ReceivedAt.Before(result[i-1].ReceivedAt) {
			t.Errorf("ordering broken at index %d", i)
		}
	}
}

// TestRecorder_FundingRates covers spec R2: single rate round-trip and range ordering.
func TestRecorder_FundingRates(t *testing.T) {
	dir := tempDir(t)
	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec.Close()

	base := time.Now().UTC().Truncate(time.Millisecond)

	if err := rec.RecordFundingRate("binance", 0.0001, base); err != nil {
		t.Fatalf("RecordFundingRate: %v", err)
	}

	rates, err := rec.QueryFundingRates(base.Add(-time.Second), base.Add(time.Second))
	if err != nil {
		t.Fatalf("QueryFundingRates: %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("want 1 rate, got %d", len(rates))
	}
	r := rates[0]
	if r.Exchange != "binance" {
		t.Errorf("Exchange: want binance, got %q", r.Exchange)
	}
	if r.Rate != 0.0001 {
		t.Errorf("Rate: want 0.0001, got %f", r.Rate)
	}
	if !r.At.Equal(base) {
		t.Errorf("At: want %v, got %v", base, r.At)
	}

	// Range ordering: multiple rates.
	start := time.Now().UTC()
	dir2 := tempDir(t)
	rec2, err := recorder.New(dir2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec2.Close()

	for i := 0; i < 5; i++ {
		ts := start.Add(time.Duration(i) * time.Minute)
		if err := rec2.RecordFundingRate("bybit", 0.001*float64(i), ts); err != nil {
			t.Fatalf("RecordFundingRate i=%d: %v", i, err)
		}
	}

	result, err := rec2.QueryFundingRates(start.Add(-time.Second), start.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryFundingRates: %v", err)
	}
	for i := 1; i < len(result); i++ {
		if result[i].At.Before(result[i-1].At) {
			t.Errorf("ordering broken at index %d", i)
		}
	}
}

// TestRecorder_WALAndIndexes covers spec R3: WAL mode active and both indexes present.
func TestRecorder_WALAndIndexes(t *testing.T) {
	dir := tempDir(t)
	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec.Close()

	mode, err := rec.JournalMode()
	if err != nil {
		t.Fatalf("JournalMode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode: want wal, got %q", mode)
	}

	indexes, err := rec.IndexNames()
	if err != nil {
		t.Fatalf("IndexNames: %v", err)
	}
	found := map[string]bool{}
	for _, idx := range indexes {
		found[idx] = true
	}
	if !found["idx_frames_ts"] {
		t.Errorf("idx_frames_ts not found in %v", indexes)
	}
	if !found["idx_funding_ts"] {
		t.Errorf("idx_funding_ts not found in %v", indexes)
	}
}

// TestRecorder_TimeRangeFilter covers spec R1 boundary: only in-range frames returned.
func TestRecorder_TimeRangeFilter(t *testing.T) {
	dir := tempDir(t)
	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rec.Close()

	now := time.Now().UTC()
	before := now.Add(-2 * time.Hour)
	after := now.Add(2 * time.Hour)
	inside := now

	for _, ts := range []time.Time{before, inside, after} {
		f := types.PriceUpdate{
			Exchange:   "binance",
			Bid:        decimal.NewFromFloat(50000),
			Ask:        decimal.NewFromFloat(50001),
			ReceivedAt: ts,
		}
		if err := rec.RecordFrame(f); err != nil {
			t.Fatalf("RecordFrame: %v", err)
		}
	}

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	frames, err := rec.QueryFrames(from, to)
	if err != nil {
		t.Fatalf("QueryFrames: %v", err)
	}
	if len(frames) != 1 {
		t.Errorf("want 1 in-range frame, got %d", len(frames))
	}
}

// TestRecorder_Close covers spec: Close makes subsequent RecordFrame return an error.
func TestRecorder_Close(t *testing.T) {
	dir := tempDir(t)
	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(50000),
		Ask:        decimal.NewFromFloat(50001),
		ReceivedAt: time.Now(),
	}
	if err := rec.RecordFrame(f); err == nil {
		t.Error("expected error after Close, got nil")
	}
}
