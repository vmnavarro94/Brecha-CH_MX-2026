package store

import (
	"fmt"
	"testing"
	"time"
)

// TestStore_SaveBacktestRun verifies that SaveBacktestRun persists a run entry.
func TestStore_SaveBacktestRun(t *testing.T) {
	s := NewStore(t.TempDir())
	defer s.Close()

	now := time.Now().UTC().Truncate(time.Second)
	rec := BacktestRunRecord{
		ID:              "run-001",
		StartedAt:       now,
		EndedAt:         now.Add(2 * time.Second),
		FromTS:          now.Add(-1 * time.Hour),
		ToTS:            now,
		StrategiesJSON:  `["spatial"]`,
		MetricsJSON:     `{"spatial":{"TotalPnL":5,"TradeCount":2}}`,
		Status:          "done",
	}

	if err := s.SaveBacktestRun(rec); err != nil {
		t.Fatalf("SaveBacktestRun: %v", err)
	}

	runs, err := s.ListBacktestRuns()
	if err != nil {
		t.Fatalf("ListBacktestRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].ID != "run-001" {
		t.Errorf("ID: got %q, want run-001", runs[0].ID)
	}
	if runs[0].Status != "done" {
		t.Errorf("Status: got %q, want done", runs[0].Status)
	}
}

// TestStore_GetBacktestRun verifies that GetBacktestRun retrieves a persisted run by ID.
func TestStore_GetBacktestRun(t *testing.T) {
	s := NewStore(t.TempDir())
	defer s.Close()

	now := time.Now().UTC()
	rec := BacktestRunRecord{
		ID:             "run-get-001",
		StartedAt:      now,
		EndedAt:        now.Add(3 * time.Second),
		FromTS:         now.Add(-2 * time.Hour),
		ToTS:           now,
		StrategiesJSON: `["triangular"]`,
		MetricsJSON:    `{"triangular":{"TotalPnL":12.5,"Sharpe":1.2,"TradeCount":5}}`,
		Status:         "done",
	}

	if err := s.SaveBacktestRun(rec); err != nil {
		t.Fatalf("SaveBacktestRun: %v", err)
	}

	got, err := s.GetBacktestRun("run-get-001")
	if err != nil {
		t.Fatalf("GetBacktestRun: %v", err)
	}
	if got.ID != "run-get-001" {
		t.Errorf("ID: got %q, want run-get-001", got.ID)
	}
	if got.MetricsJSON != `{"triangular":{"TotalPnL":12.5,"Sharpe":1.2,"TradeCount":5}}` {
		t.Errorf("MetricsJSON round-trip failed: got %q", got.MetricsJSON)
	}
}

// TestStore_GetBacktestRun_NotFound verifies an error is returned for an unknown run ID.
func TestStore_GetBacktestRun_NotFound(t *testing.T) {
	s := NewStore(t.TempDir())
	defer s.Close()

	_, err := s.GetBacktestRun("nonexistent-id")
	if err == nil {
		t.Fatal("expected an error for unknown run ID, got nil")
	}
}

// TestStore_ListBacktestRuns verifies runs are returned sorted by started_at DESC.
func TestStore_ListBacktestRuns(t *testing.T) {
	s := NewStore(t.TempDir())
	defer s.Close()

	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		rec := BacktestRunRecord{
			ID:             fmt.Sprintf("run-%d", i),
			StartedAt:      base.Add(time.Duration(i) * time.Minute),
			EndedAt:        base.Add(time.Duration(i)*time.Minute + 10*time.Second),
			FromTS:         base,
			ToTS:           base.Add(1 * time.Hour),
			StrategiesJSON: `[]`,
			MetricsJSON:    `{}`,
			Status:         "done",
		}
		if err := s.SaveBacktestRun(rec); err != nil {
			t.Fatalf("SaveBacktestRun %d: %v", i, err)
		}
	}

	runs, err := s.ListBacktestRuns()
	if err != nil {
		t.Fatalf("ListBacktestRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}

	// sorted DESC — most-recent (run-2) first
	if runs[0].ID != "run-2" {
		t.Errorf("first run (most recent): got %q, want run-2", runs[0].ID)
	}
	if runs[2].ID != "run-0" {
		t.Errorf("last run (oldest): got %q, want run-0", runs[2].ID)
	}
}
