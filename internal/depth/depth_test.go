package depth

import (
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
)


func fixedRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

// TestAskLevels_Deterministic verifies exactly N ascending levels with quantities within bounds.
// spec D1, D2
func TestAskLevels_Deterministic(t *testing.T) {
	cfg := Config{
		N:         5,
		StepPct:   0.0001,
		MinQtyBTC: 0.005,
		MaxQtyBTC: 0.025,
		Rand:      fixedRand(42),
	}
	bbo := decimal.NewFromFloat(50000.0)

	levels := AskLevels(bbo, cfg)

	if len(levels) != 5 {
		t.Fatalf("expected 5 levels, got %d", len(levels))
	}

	// step = 50000 * 0.0001 = 5 → prices: 50000, 50005, 50010, 50015, 50020
	wantPrices := []float64{50000, 50005, 50010, 50015, 50020}
	for i, lvl := range levels {
		p, _ := lvl.Price.Float64()
		if p != wantPrices[i] {
			t.Errorf("ask level %d price: got %v, want %v", i, p, wantPrices[i])
		}
	}

	for i, lvl := range levels {
		q, _ := lvl.Qty.Float64()
		if q < cfg.MinQtyBTC || q > cfg.MaxQtyBTC {
			t.Errorf("ask level %d qty %v out of [%v, %v]", i, q, cfg.MinQtyBTC, cfg.MaxQtyBTC)
		}
	}
}

// TestBidLevels_Deterministic verifies exactly N descending levels with quantities within bounds.
// spec D1, D2
func TestBidLevels_Deterministic(t *testing.T) {
	cfg := Config{
		N:         5,
		StepPct:   0.0001,
		MinQtyBTC: 0.005,
		MaxQtyBTC: 0.025,
		Rand:      fixedRand(42),
	}
	bbo := decimal.NewFromFloat(50000.0)

	levels := BidLevels(bbo, cfg)

	if len(levels) != 5 {
		t.Fatalf("expected 5 levels, got %d", len(levels))
	}

	// step = 50000 * 0.0001 = 5 → prices: 50000, 49995, 49990, 49985, 49980
	wantPrices := []float64{50000, 49995, 49990, 49985, 49980}
	for i, lvl := range levels {
		p, _ := lvl.Price.Float64()
		if p != wantPrices[i] {
			t.Errorf("bid level %d price: got %v, want %v", i, p, wantPrices[i])
		}
	}

	for i, lvl := range levels {
		q, _ := lvl.Qty.Float64()
		if q < cfg.MinQtyBTC || q > cfg.MaxQtyBTC {
			t.Errorf("bid level %d qty %v out of [%v, %v]", i, q, cfg.MinQtyBTC, cfg.MaxQtyBTC)
		}
	}
}
