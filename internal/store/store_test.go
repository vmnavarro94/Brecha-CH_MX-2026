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
	s := NewStore(t.TempDir())
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
	s := NewStore(t.TempDir())
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
	s := NewStore(t.TempDir())
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
	s := NewStore(t.TempDir())
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
	s := NewStore(t.TempDir())
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

// TestLoadTrades_LegacyPayloadBackwardCompat verifies that a Trade payload written
// before partial_fill / requested_volume existed loads with zero-values for those
// fields. This locks the spec D10 contract — old SQLite rows must not break parsing.
func TestLoadTrades_LegacyPayloadBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Hand-craft a payload representing a pre-D10 trade: no partial_fill, no requested_volume.
	legacyID := "legacy-001"
	legacyPayload := `{
		"ID": "` + legacyID + `",
		"OpportunityID": "opp-1",
		"BuyExchange": "binance",
		"SellExchange": "kraken",
		"BuyPrice": "50000.00",
		"SellPrice": "50300.00",
		"Volume": "0.01",
		"GrossProfit": "3.00",
		"Fees": "1.00",
		"NetProfit": "2.00",
		"Slippage": "0",
		"ExecutedAt": "2026-01-01T00:00:00Z"
	}`

	// Insert directly into SQLite, bypassing the Go struct path so the new fields
	// genuinely never appear in the payload.
	_, err := s.db.Exec(
		`INSERT INTO trades (id, executed_at, buy_exchange, sell_exchange, buy_price,
			sell_price, volume, gross_profit, fees, net_profit, slippage, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		legacyID, "2026-01-01T00:00:00Z", "binance", "kraken", "50000.00",
		"50300.00", "0.01", "3.00", "1.00", "2.00", "0", legacyPayload,
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	s.Close()

	// Reopen — loadTrades() runs and must not panic on missing keys.
	s2 := NewStore(dir)
	defer s2.Close()

	trades := s2.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("AllTrades: got %d, want 1", len(trades))
	}
	tr := trades[0]
	if tr.ID != legacyID {
		t.Errorf("ID: got %q, want %q", tr.ID, legacyID)
	}
	if tr.PartialFill != false {
		t.Errorf("PartialFill on legacy row: got %v, want false (zero value)", tr.PartialFill)
	}
	if !tr.RequestedVolume.IsZero() {
		t.Errorf("RequestedVolume on legacy row: got %s, want 0 (zero value)", tr.RequestedVolume.String())
	}
}
