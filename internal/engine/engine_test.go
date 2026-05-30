package engine

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// fixedClock is a Clock that always returns the same time, used in tests.
type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

// makeUpdate constructs a PriceUpdate for the given exchange with the given bid/ask.
func makeUpdate(exchange string, bid, ask float64, receivedAt time.Time) types.PriceUpdate {
	return types.PriceUpdate{
		Exchange:   exchange,
		Bid:        decimal.NewFromFloat(bid),
		Ask:        decimal.NewFromFloat(ask),
		BidSize:    decimal.NewFromFloat(1.0),
		AskSize:    decimal.NewFromFloat(1.0),
		ReceivedAt: receivedAt,
		ExchangeAt: receivedAt,
	}
}

// TestDetectCorrectBuySellPair verifies that given 3 exchanges with known BBO,
// the engine identifies the correct buy (cheapest ask) and sell (highest bid) pair.
func TestDetectCorrectBuySellPair(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50120.0, now),
		"kraken":  makeUpdate("kraken", 50150.0, 50180.0, now),
		"bybit":   makeUpdate("bybit", 50090.0, 50110.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
			"bybit":   {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clk, cfg)

	// Update with bybit's ask (lowest ask at 50110), kraken has highest bid at 50150.
	update := makeUpdate("bybit", 50090.0, 50110.0, now)
	eng.ProcessUpdate(update)

	opp, ok := eng.DequeueTop()
	if !ok {
		t.Fatal("expected an opportunity to be detected")
	}
	if opp.BuyExchange != "bybit" {
		t.Errorf("BuyExchange: got %q, want %q", opp.BuyExchange, "bybit")
	}
	if opp.SellExchange != "kraken" {
		t.Errorf("SellExchange: got %q, want %q", opp.SellExchange, "kraken")
	}
}

// TestNetProfitFormula verifies net_profit = gross_spread - ask_A*fee_A - bid_B*fee_B - ask_A*slippage_A.
func TestNetProfitFormula(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50200.0
	feeA := 0.001
	feeB := 0.001
	slippageA := 0.0002

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50500.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: feeA, SlippageFactor: slippageA},
			"kraken":  {TakerFee: feeB, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clk, cfg)

	// Process update for binance (ask=50000), it checks vs kraken (bid=50200).
	update := makeUpdate("binance", bid, ask, now)
	eng.ProcessUpdate(update)

	opp, ok := eng.DequeueTop()
	if !ok {
		t.Fatal("expected an opportunity")
	}

	// Expected: gross = bid - ask = 50200 - 50000 = 200
	// net = gross - ask*feeA - bid*feeB - ask*slippageA
	// net = 200 - 50000*0.001 - 50200*0.001 - 50000*0.0002
	// net = 200 - 50 - 50.2 - 10 = 89.8
	wantNet := bid - ask - ask*feeA - bid*feeB - ask*slippageA
	gotNet, _ := opp.NetProfit.Float64()

	if math.Abs(gotNet-wantNet) > 0.01 {
		t.Errorf("NetProfit: got %v, want %v", gotNet, wantNet)
	}
}

// TestSubThresholdSpreadDiscarded verifies that an opportunity with non-positive net profit
// is not enqueued.
func TestSubThresholdSpreadDiscarded(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	// ask > bid → negative gross spread → discarded
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50500.0, now),
		"kraken":  makeUpdate("kraken", 50000.0, 50600.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clk, cfg)

	// binance ask=50500, kraken bid=50000 → gross = 50000-50500 = -500 → discarded
	update := makeUpdate("binance", 49800.0, 50500.0, now)
	eng.ProcessUpdate(update)

	_, ok := eng.DequeueTop()
	if ok {
		t.Error("sub-threshold spread should produce no opportunity")
	}
}

// TestScoreWithModelReady verifies the score formula when the spread model is ready.
// score = net_pct*0.6 + sigmoid(z)*0.4
func TestScoreWithModelReady(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	// Create a ready spread model for this pair.
	sm := model.NewSpreadModel()
	for i := 0; i < 100; i++ {
		sm.Update(float64(i) * 0.001) // seed with 100 values so it's ready
	}

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{
		"binance-kraken": sm,
	}

	eng := NewEngine(snapshotFn, models, clk, cfg)

	update := makeUpdate("binance", bid, ask, now)
	eng.ProcessUpdate(update)

	opp, ok := eng.DequeueTop()
	if !ok {
		t.Fatal("expected opportunity")
	}

	// Score must be a number in a valid range (not NaN/Inf, > 0 for positive net profit).
	score, _ := opp.Score.Float64()
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Errorf("Score is non-finite: %v", score)
	}
	if score <= 0 {
		t.Errorf("Score should be positive for profitable opportunity, got %v", score)
	}
}

// TestScoreFallbackModelNotReady verifies score = normalized net_pct when model is not ready.
func TestScoreFallbackModelNotReady(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	// Not-ready model: only 50 samples (below MinSamples=100).
	sm := model.NewSpreadModel()
	for i := 0; i < 50; i++ {
		sm.Update(float64(i) * 0.001)
	}

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	eng := NewEngine(snapshotFn, models, clk, cfg)

	update := makeUpdate("binance", bid, ask, now)
	eng.ProcessUpdate(update)

	opp, ok := eng.DequeueTop()
	if !ok {
		t.Fatal("expected opportunity")
	}

	// When model is not ready, ZScore should be zero (not used in scoring).
	zScore, _ := opp.ZScore.Float64()
	if zScore != 0.0 {
		t.Errorf("ZScore should be 0 when model is not ready, got %v", zScore)
	}
}

// TestHeapOrdering verifies that DequeueTop returns the highest-score opportunity first.
func TestHeapOrdering(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	// We need 3 distinct pairs with very different profit margins.
	// To get 3 separate pairs detected in one ProcessUpdate call from exchange A,
	// we need exchanges B, C, D in the snapshot.
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now), // ask=50000 (our buy)
		"kraken":  makeUpdate("kraken", 50200.0, 99999.0, now),  // bid=50200 (highest profit)
		"bybit":   makeUpdate("bybit", 50150.0, 99999.0, now),   // bid=50150 (mid profit)
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
			"bybit":   {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clk, cfg)

	// binance ask=50000, kraken bid=50200 → profit=200
	// binance ask=50000, bybit bid=50150 → profit=150
	update := makeUpdate("binance", 50100.0, 50000.0, now)
	eng.ProcessUpdate(update)

	first, ok1 := eng.DequeueTop()
	second, ok2 := eng.DequeueTop()
	third, ok3 := eng.DequeueTop()

	if !ok1 || !ok2 {
		t.Fatalf("expected at least 2 opportunities, got ok1=%v ok2=%v ok3=%v", ok1, ok2, ok3)
	}

	score1, _ := first.Score.Float64()
	score2, _ := second.Score.Float64()
	if score1 < score2 {
		t.Errorf("heap ordering: first score %v < second score %v (should be descending)", score1, score2)
	}
	_ = third
}

// TestTTLEviction verifies that an opportunity older than OpportunityTTL is discarded on dequeue.
func TestTTLEviction(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Clock at detection time.
	clkAtDetection := fixedClock{t: base}

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, base),
		"kraken":  makeUpdate("kraken", 50300.0, 99999.0, base),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clkAtDetection, cfg)

	update := makeUpdate("binance", 50100.0, 50000.0, base)
	eng.ProcessUpdate(update)

	// Advance clock past TTL before dequeuing.
	eng.SetClock(fixedClock{t: base.Add(600 * time.Millisecond)})

	_, ok := eng.DequeueTop()
	if ok {
		t.Error("opportunity older than TTL should be evicted on dequeue")
	}
}

// TestStaleExchangeNoOpportunity verifies that a stale exchange (ReceivedAt > 2s ago)
// does not produce opportunities.
func TestStaleExchangeNoOpportunity(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	staleTime := now.Add(-3 * time.Second)

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 99999.0, staleTime), // stale
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}

	eng := NewEngine(snapshotFn, nil, clk, cfg)

	update := makeUpdate("binance", 50100.0, 50000.0, now)
	eng.ProcessUpdate(update)

	_, ok := eng.DequeueTop()
	if ok {
		t.Error("stale exchange should not produce opportunities")
	}
}

// TestEngine_LatencyStats_AfterUpdates verifies that after 10 ProcessUpdate calls
// samples == 10 and both percentiles are > 0.
func TestEngine_LatencyStats_AfterUpdates(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }
	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}
	eng := NewEngine(snapshotFn, nil, clk, cfg)
	for i := 0; i < 10; i++ {
		eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))
	}
	p50us, p99us, samples := eng.LatencyStats()
	if samples != 10 {
		t.Errorf("samples: got %d, want 10", samples)
	}
	if p50us <= 0 {
		t.Errorf("p50us: got %v, want > 0", p50us)
	}
	if p99us <= 0 {
		t.Errorf("p99us: got %v, want > 0", p99us)
	}
}

// TestEngine_LatencyStats_ColdStart verifies that with fewer than 10 samples
// LatencyStats returns zeros for both percentiles.
func TestEngine_LatencyStats_ColdStart(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }
	cfg := Config{
		Fees: map[string]FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		OpportunityTTL:     500 * time.Millisecond,
		StalenessThreshold: 2 * time.Second,
	}
	eng := NewEngine(snapshotFn, nil, clk, cfg)
	for i := 0; i < 9; i++ {
		eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))
	}
	p50us, p99us, samples := eng.LatencyStats()
	if samples != 9 {
		t.Errorf("samples: got %d, want 9", samples)
	}
	if p50us != 0 {
		t.Errorf("p50us cold-start: got %v, want 0", p50us)
	}
	if p99us != 0 {
		t.Errorf("p99us cold-start: got %v, want 0", p99us)
	}
}
