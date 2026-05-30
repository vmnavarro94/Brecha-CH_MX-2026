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

// TestWalk_ExactSingleLevel: target fully covered by first level.
// spec D3 scenario 1
func TestWalk_ExactSingleLevel(t *testing.T) {
	levels := []Level{
		{Price: decimal.NewFromFloat(50000), Qty: decimal.NewFromFloat(0.025)},
		{Price: decimal.NewFromFloat(50005), Qty: decimal.NewFromFloat(0.05)},
	}
	target := decimal.NewFromFloat(0.02)

	filled, vwap, partial := Walk(levels, target)

	filledF, _ := filled.Float64()
	vwapF, _ := vwap.Float64()

	if filledF != 0.02 {
		t.Errorf("filled: got %v, want 0.02", filledF)
	}
	if vwapF != 50000 {
		t.Errorf("vwap: got %v, want 50000", vwapF)
	}
	if partial {
		t.Errorf("partial: got true, want false")
	}
}

// TestWalk_ExactMultiLevel: target requires two levels, exact fill.
// spec D3 scenario 2
func TestWalk_ExactMultiLevel(t *testing.T) {
	levels := []Level{
		{Price: decimal.NewFromFloat(50000), Qty: decimal.NewFromFloat(0.025)},
		{Price: decimal.NewFromFloat(50005), Qty: decimal.NewFromFloat(0.05)},
	}
	target := decimal.NewFromFloat(0.05)

	filled, vwap, partial := Walk(levels, target)

	filledF, _ := filled.Float64()
	vwapF, _ := vwap.Float64()

	// cost = 0.025*50000 + 0.025*50005 = 1250 + 1250.125 = 2500.125
	// vwap = 2500.125 / 0.05 = 50002.5
	if filledF != 0.05 {
		t.Errorf("filled: got %v, want 0.05", filledF)
	}
	if vwapF != 50002.5 {
		t.Errorf("vwap: got %v, want 50002.5", vwapF)
	}
	if partial {
		t.Errorf("partial: got true, want false")
	}
}

// TestWalk_LiquidityExhausted: all liquidity exhausted before target reached.
// spec D3 scenario 3
func TestWalk_LiquidityExhausted(t *testing.T) {
	levels := []Level{
		{Price: decimal.NewFromFloat(50000), Qty: decimal.NewFromFloat(0.025)},
		{Price: decimal.NewFromFloat(50005), Qty: decimal.NewFromFloat(0.05)},
	}
	target := decimal.NewFromFloat(0.10)

	filled, vwap, partial := Walk(levels, target)

	filledF, _ := filled.Float64()
	// vwap = (0.025*50000 + 0.05*50005) / 0.075 = (1250 + 2500.25) / 0.075 = 3750.25/0.075 ≈ 50003.3333
	vwapF, _ := vwap.Float64()

	if filledF != 0.075 {
		t.Errorf("filled: got %v, want 0.075", filledF)
	}
	if vwapF < 50003.0 || vwapF > 50004.0 {
		t.Errorf("vwap: got %v, want ~50003.33", vwapF)
	}
	if !partial {
		t.Errorf("partial: got false, want true")
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
