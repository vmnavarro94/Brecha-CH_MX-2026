package spatial_test

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// --- helpers shared across Kelly/correlation/adaptive tests ---

func zeroFees() map[string]spatial.FeeConfig {
	return map[string]spatial.FeeConfig{
		"binance": {},
		"kraken":  {},
	}
}

// makeKellyConfig returns a minimal Config for adaptive/Kelly/correlation tests.
func makeKellyConfig() spatial.Config {
	return spatial.Config{
		Fees:               zeroFees(),
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
		KellyMinSamples:    10,
		KellyFraction:      0.25,
		CorrWindowN:        50,
		CorrPenaltyWeight:  0.3,
	}
}

// oppBinanceKraken returns a snapshot + update where binance(ask) buys and kraken(bid) sells.
// Spread: gross = bid - ask = bigBid - smallAsk → positive. No fees → netPct = gross/ask.
func priceSnapshot(buyAsk, sellBid float64, now time.Time) (map[string]types.PriceUpdate, types.PriceUpdate) {
	snapshot := map[string]types.PriceUpdate{
		"binance": {Exchange: "binance", Bid: decimal.NewFromFloat(buyAsk - 1), Ask: decimal.NewFromFloat(buyAsk), ReceivedAt: now},
		"kraken":  {Exchange: "kraken", Bid: decimal.NewFromFloat(sellBid), Ask: decimal.NewFromFloat(sellBid + 1), ReceivedAt: now},
	}
	update := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(buyAsk - 1),
		Ask:        decimal.NewFromFloat(buyAsk),
		ReceivedAt: now,
	}
	return snapshot, update
}

// findBinanceKraken finds the binance→kraken opportunity in a slice, or returns nil.
func findBinanceKraken(opps []types.Opportunity) *types.Opportunity {
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			return &opps[i]
		}
	}
	return nil
}

// --- Phase 3: Adaptive threshold tests ---

// TestSpatial_AdaptiveThreshold_Blocks: netPct above base but below adaptiveMin → not emitted (A2).
func TestSpatial_AdaptiveThreshold_Blocks(t *testing.T) {
	now := time.Now()

	// Seed a spread model with enough samples so Std() is non-zero.
	sm := model.NewSpreadModel()
	for i := 0; i < 200; i++ {
		sm.Update(float64(i) * 0.00001) // small increments so std is roughly 0.001
	}
	std := sm.Std()
	if std == 0 {
		t.Skip("spread model std is zero; cannot test adaptive threshold")
	}

	cfg := makeKellyConfig()
	cfg.BaseMinNetProfitPct = 0.0001 // very low base
	cfg.AdaptiveCoeff = 100.0        // huge coeff → adaptiveMin = 0.0001 + 100*std >> netPct

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := spatial.New(cfg, models)

	// netPct = (50200-50000)/50000 = 0.004 → below adaptiveMin (which is large due to huge coeff)
	snapshot, update := priceSnapshot(50000.0, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	if opp := findBinanceKraken(opps); opp != nil {
		t.Errorf("opportunity emitted above base but below adaptiveMin (adaptiveMin=%.6f)", cfg.BaseMinNetProfitPct+cfg.AdaptiveCoeff*std)
	}
}

// TestSpatial_AdaptiveThreshold_Passes: netPct > adaptiveMin → opportunity emitted (A2).
func TestSpatial_AdaptiveThreshold_Passes(t *testing.T) {
	now := time.Now()

	sm := model.NewSpreadModel()
	for i := 0; i < 200; i++ {
		sm.Update(float64(i) * 0.00001)
	}

	cfg := makeKellyConfig()
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0 // adaptiveMin = 0 → all positive netPct pass

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := spatial.New(cfg, models)

	snapshot, update := priceSnapshot(50000.0, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	if opp := findBinanceKraken(opps); opp == nil {
		t.Error("expected opportunity when adaptiveMin=0")
	}
}

// --- Phase 4: Kelly sizing tests ---

// TestSpatial_KellyColdStartUsesMaxPosition: fresh strategy → MaxVolume == MaxPositionUSDT/buyAsk (K2, NR3).
func TestSpatial_KellyColdStartUsesMaxPosition(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	spat := spatial.New(cfg, nil)

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected opportunity")
	}
	gotVol, _ := opp.MaxVolume.Float64()
	wantVol := maxPos / buyAsk
	if math.Abs(gotVol-wantVol) > 1e-9 {
		t.Errorf("MaxVolume cold-start: got %.9f, want %.9f", gotVol, wantVol)
	}
}

// TestSpatial_KellyAfterTradesScalesVolume: after 10+ positive trades, MaxVolume < MaxPositionUSDT/buyAsk (K1, K4).
func TestSpatial_KellyAfterTradesScalesVolume(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.KellyMinSamples = 10
	cfg.KellyFraction = 0.25 // cap

	// Use 0 adaptive coeff so the threshold doesn't filter.
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// Record 10 trades with alternating 0/1 returns → Welford mean=0.5, var=0.25 → raw=2.0 → capped at 0.25.
	// effectiveFraction = 0.25 → maxVolume = 0.25 * 50000 / 50000 = 0.25 BTC.
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			spat.RecordTradeReturn("binance", "kraken", 0.0)
		} else {
			spat.RecordTradeReturn("binance", "kraken", 1.0)
		}
	}

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected opportunity after recording trades")
	}

	gotVol, _ := opp.MaxVolume.Float64()
	fullVol := maxPos / buyAsk // 1.0 BTC
	if gotVol >= fullVol {
		t.Errorf("MaxVolume after Kelly: got %.9f, want < %.9f (should be scaled down)", gotVol, fullVol)
	}
}

// --- Phase 5: Correlation penalty tests ---

// TestSpatial_CorrelationPenalty_NoOverlap: 0 matching entries → penalty==1.0, MaxVolume unchanged (C2).
func TestSpatial_CorrelationPenalty_NoOverlap(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.CorrPenaltyWeight = 0.3
	cfg.CorrWindowN = 50
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// Record trades on a DIFFERENT pair (mexc-okx) so binance-kraken has no matches.
	for i := 0; i < 5; i++ {
		spat.RecordTradeReturn("mexc", "okx", 0.001)
	}

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected binance-kraken opportunity")
	}

	gotVol, _ := opp.MaxVolume.Float64()
	wantVol := maxPos / buyAsk // penalty=1.0, no scaling
	if math.Abs(gotVol-wantVol) > 1e-9 {
		t.Errorf("NoOverlap: MaxVolume got %.9f, want %.9f (penalty should be 1.0)", gotVol, wantVol)
	}
}

// TestSpatial_CorrelationPenalty_PartialOverlap: 5 matches in 50-window → penalty==0.97 (C2, C3).
func TestSpatial_CorrelationPenalty_PartialOverlap(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.CorrPenaltyWeight = 0.3
	cfg.CorrWindowN = 50
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// 5 trades with binance (shared leg with binance-kraken candidate).
	for i := 0; i < 5; i++ {
		spat.RecordTradeReturn("binance", "mexc", 0.001)
	}

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected binance-kraken opportunity")
	}

	gotVol, _ := opp.MaxVolume.Float64()
	// penalty = 1 - 0.3 * 5/50 = 1 - 0.03 = 0.97
	wantPenalty := 1.0 - 0.3*5.0/50.0
	wantVol := (maxPos / buyAsk) * wantPenalty
	if math.Abs(gotVol-wantVol) > 1e-6 {
		t.Errorf("PartialOverlap: MaxVolume got %.9f, want %.9f (penalty=%.3f)", gotVol, wantVol, wantPenalty)
	}
}

// TestSpatial_CorrelationPenalty_Floor: high overlap → penalty floored at 0.1 (C2 floor).
func TestSpatial_CorrelationPenalty_Floor(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.CorrPenaltyWeight = 0.3
	cfg.CorrWindowN = 50
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// 50 trades sharing a leg with the candidate → matches=50 → raw=1-0.3*50/50=0.7 → no floor.
	// To force floor: need raw < 0.1 → 1-0.3*m/50 < 0.1 → m > 30. Use CorrPenaltyWeight=1.0 for easier floor.
	// Actually with weight=0.3 and m=50: raw=1-0.3*50/50=0.7 (above floor).
	// Use weight=3.0 or inject all 50 as same-leg. Let's use CorrPenaltyWeight=1.0 and m>40.
	// But cfg was already set. Let me just use a cfg with higher penalty weight.
	// Rebuild with penalty weight=1.0 to easily hit the floor.
	cfg.CorrPenaltyWeight = 1.0
	spat = spatial.New(cfg, nil)
	// 50 trades with binance → matches=50 → raw=1-1.0*50/50=0 < 0.1 → floor=0.1
	for i := 0; i < 50; i++ {
		spat.RecordTradeReturn("binance", "mexc", 0.001)
	}

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected binance-kraken opportunity")
	}

	gotVol, _ := opp.MaxVolume.Float64()
	wantVol := (maxPos / buyAsk) * 0.1 // floor = 0.1
	if math.Abs(gotVol-wantVol) > 1e-6 {
		t.Errorf("Floor: MaxVolume got %.9f, want %.9f (floor=0.1)", gotVol, wantVol)
	}
}

// TestSpatial_RecordTradeReturn_UpdatesKellyAndRing: single call → estimator Samples()==1, ring.Len()==1 (M-SS1).
// This test accesses internal state indirectly via Kelly cold-start behavior.
func TestSpatial_RecordTradeReturn_UpdatesKellyAndRing(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.KellyMinSamples = 1 // warm up after 1 sample
	cfg.KellyFraction = 0.5
	cfg.CorrPenaltyWeight = 1.0 // 100% weight so 1 match → noticeable penalty
	cfg.CorrWindowN = 1         // window of 1 so 1 match = 100% overlap
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// Record one trade: alternating 0/1 won't work with minN=1. Use positive value.
	// With minN=1 and 1 positive record: fraction = mean/var... but with 1 sample, m2=0 → var=0 → fraction=0.
	// Actually Welford with n=1: m2=0, var=0 → zero-variance guard → fraction=0. So Kelly stays 1.0 (cold-start fallback).
	// Correlation: 1 match in window=1 → penalty = 1 - 1.0*1/1 = 0 → floored at 0.1.
	spat.RecordTradeReturn("binance", "kraken", 0.001)

	snapshot, update := priceSnapshot(buyAsk, 50200.0, now)
	opps := spat.Detect(update, snapshot, now)

	opp := findBinanceKraken(opps)
	if opp == nil {
		t.Fatal("expected opportunity")
	}

	// After RecordTradeReturn: ring has 1 entry matching binance-kraken.
	// penalty = 1 - 1.0*1/1 = 0 → floor 0.1
	// MaxVolume = (1.0 * 50000/50000) * 0.1 = 0.1
	gotVol, _ := opp.MaxVolume.Float64()
	wantVol := (maxPos / buyAsk) * 0.1
	if math.Abs(gotVol-wantVol) > 1e-9 {
		t.Errorf("UpdatesKellyAndRing: MaxVolume got %.9f, want %.9f (penalty=0.1 from ring)", gotVol, wantVol)
	}
}

// TestSpatial_RecordTradeReturn_PerPairIsolation: record for pair A; pair B estimator unaffected (K4).
func TestSpatial_RecordTradeReturn_PerPairIsolation(t *testing.T) {
	now := time.Now()
	const buyAsk = 50000.0
	const maxPos = 50000.0

	cfg := makeKellyConfig()
	cfg.MaxPositionUSDT = maxPos
	cfg.KellyMinSamples = 10
	cfg.KellyFraction = 0.25
	cfg.CorrPenaltyWeight = 0.0 // disable correlation to isolate Kelly
	cfg.CorrWindowN = 50
	cfg.BaseMinNetProfitPct = 0.0
	cfg.AdaptiveCoeff = 0.0

	spat := spatial.New(cfg, nil)

	// Record 10 trades for binance-kraken → Kelly fraction becomes non-zero.
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			spat.RecordTradeReturn("binance", "kraken", 0.0)
		} else {
			spat.RecordTradeReturn("binance", "kraken", 1.0)
		}
	}

	// Now check binance-mexc (different pair) — its MaxVolume should be unscaled (full MaxPositionUSDT/buyAsk).
	snapshot2 := map[string]types.PriceUpdate{
		"binance": {Exchange: "binance", Bid: decimal.NewFromFloat(buyAsk - 1), Ask: decimal.NewFromFloat(buyAsk), ReceivedAt: now},
		"mexc":    {Exchange: "mexc", Bid: decimal.NewFromFloat(50200.0), Ask: decimal.NewFromFloat(50201.0), ReceivedAt: now},
	}
	update2 := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(buyAsk - 1),
		Ask:        decimal.NewFromFloat(buyAsk),
		ReceivedAt: now,
	}
	opps2 := spat.Detect(update2, snapshot2, now)

	var mexcOpp *types.Opportunity
	for i := range opps2 {
		if opps2[i].BuyExchange == "binance" && opps2[i].SellExchange == "mexc" {
			mexcOpp = &opps2[i]
			break
		}
	}
	if mexcOpp == nil {
		t.Fatal("expected binance-mexc opportunity")
	}

	gotVol, _ := mexcOpp.MaxVolume.Float64()
	wantVol := maxPos / buyAsk // should be 1.0 (unaffected Kelly for mexc)
	if math.Abs(gotVol-wantVol) > 1e-9 {
		t.Errorf("PerPairIsolation: binance-mexc MaxVolume got %.9f, want %.9f (should be unscaled)", gotVol, wantVol)
	}
}
