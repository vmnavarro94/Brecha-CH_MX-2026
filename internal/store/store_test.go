package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

func newOpp(id string, status types.OpportunityStatus) types.Opportunity {
	return types.Opportunity{
		ID:         id,
		Status:     status,
		DetectedAt: time.Now(),
	}
}

// TestSaveAndGetByID verifies that an opportunity saved to the store can be retrieved by ID.
func TestSaveAndGetByID(t *testing.T) {
	s := NewStore()
	opp := newOpp("abc", types.StatusDetected)
	s.Save(opp)

	got, ok := s.GetByID("abc")
	if !ok {
		t.Fatal("GetByID should return true for saved opportunity")
	}
	if got.ID != "abc" {
		t.Errorf("GetByID returned wrong opportunity: got ID %v, want 'abc'", got.ID)
	}
	if got.Status != types.StatusDetected {
		t.Errorf("GetByID returned wrong status: got %v, want %v", got.Status, types.StatusDetected)
	}
}

// TestGetByIDUnknown verifies that GetByID returns nil/false for an unknown ID.
func TestGetByIDUnknown(t *testing.T) {
	s := NewStore()
	got, ok := s.GetByID("nonexistent")
	if ok {
		t.Error("GetByID should return false for unknown ID")
	}
	if got != nil {
		t.Errorf("GetByID should return nil for unknown ID, got %v", got)
	}
}

// TestQueryByStatusReturnsMatchingOnly verifies QueryByStatus filters correctly.
func TestQueryByStatusReturnsMatchingOnly(t *testing.T) {
	s := NewStore()
	s.Save(newOpp("1", types.StatusDetected))
	s.Save(newOpp("2", types.StatusDetected))
	s.Save(newOpp("3", types.StatusDetected))
	s.Save(newOpp("4", types.StatusExecuted))
	s.Save(newOpp("5", types.StatusExecuted))

	executed := s.QueryByStatus(types.StatusExecuted)
	if len(executed) != 2 {
		t.Errorf("QueryByStatus(executed): got %d results, want 2", len(executed))
	}
	for _, opp := range executed {
		if opp.Status != types.StatusExecuted {
			t.Errorf("QueryByStatus returned opportunity with wrong status: %v", opp.Status)
		}
	}

	detected := s.QueryByStatus(types.StatusDetected)
	if len(detected) != 3 {
		t.Errorf("QueryByStatus(detected): got %d results, want 3", len(detected))
	}
}

// TestSaveTradeAndAllTrades verifies that SaveTrade persists a trade retrievable via AllTrades.
func TestSaveTradeAndAllTrades(t *testing.T) {
	s := NewStore()
	trade := types.Trade{
		ID:            "t1",
		OpportunityID: "opp1",
		ExecutedAt:    time.Now(),
	}
	s.SaveTrade(trade)

	trades := s.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("AllTrades: got %d trades, want 1", len(trades))
	}
	if trades[0].ID != "t1" {
		t.Errorf("AllTrades: got trade ID %v, want 't1'", trades[0].ID)
	}
}

// TestConcurrentInserts spawns 50 goroutines inserting opportunities simultaneously
// and verifies no data race occurs. Run with -race.
func TestConcurrentInserts(t *testing.T) {
	s := NewStore()
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			s.Save(newOpp(fmt.Sprintf("opp-%d", i), types.StatusDetected))
		}()
	}
	wg.Wait()

	all := s.QueryByStatus(types.StatusDetected)
	if len(all) != n {
		t.Errorf("ConcurrentInserts: expected %d opportunities, got %d", n, len(all))
	}
}
