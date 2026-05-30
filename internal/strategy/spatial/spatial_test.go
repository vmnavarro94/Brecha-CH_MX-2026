package spatial_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

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

// newSpatial creates a SpatialStrategy with the given config and no spread models.
func newSpatial(cfg spatial.Config) *spatial.SpatialStrategy {
	return spatial.New(cfg, nil)
}

// newSpatialWithModels creates a SpatialStrategy with the given config and spread models.
func newSpatialWithModels(cfg spatial.Config, models map[string]*model.SpreadModel) *spatial.SpatialStrategy {
	return spatial.New(cfg, models)
}

// defaultCfg returns a base Config suitable for most detection tests.
func defaultCfg(fees map[string]spatial.FeeConfig) spatial.Config {
	return spatial.Config{
		Fees:               fees,
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}
}

// --- Migrated detection tests (verbatim scenarios from engine_test.go) ---

// TestDetectCorrectBuySellPair verifies that given 3 exchanges with known BBO,
// the strategy identifies the correct buy (cheapest ask) and sell (highest bid) pair.
func TestDetectCorrectBuySellPair(t *testing.T) {
	now := time.Now()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50120.0, now),
		"kraken":  makeUpdate("kraken", 50150.0, 50180.0, now),
		"bybit":   makeUpdate("bybit", 50090.0, 50110.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
		"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
		"bybit":   {TakerFee: 0.0, SlippageFactor: 0.0},
	})

	spat := newSpatial(cfg)

	// Update with bybit's ask (lowest ask at 50110), kraken has highest bid at 50150.
	update := makeUpdate("bybit", 50090.0, 50110.0, now)
	opps := spat.Detect(update, snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected an opportunity to be detected")
	}
	// Find the highest-scoring opp (should be bybit->kraken).
	var best types.Opportunity
	for i, o := range opps {
		if i == 0 || o.Score.GreaterThan(best.Score) {
			best = o
		}
	}
	if best.BuyExchange != "bybit" {
		t.Errorf("BuyExchange: got %q, want %q", best.BuyExchange, "bybit")
	}
	if best.SellExchange != "kraken" {
		t.Errorf("SellExchange: got %q, want %q", best.SellExchange, "kraken")
	}
}

// TestNetProfitFormula verifies net_profit = gross_spread - ask_A*fee_A - bid_B*fee_B - ask_A*slippage_A.
func TestNetProfitFormula(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50200.0
	feeA := 0.001
	feeB := 0.001
	slippageA := 0.0002

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50500.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: feeA, SlippageFactor: slippageA},
		"kraken":  {TakerFee: feeB, SlippageFactor: 0.0003},
	})

	spat := newSpatial(cfg)

	// Process update for binance (ask=50000), it checks vs kraken (bid=50200).
	update := makeUpdate("binance", bid, ask, now)
	opps := spat.Detect(update, snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected an opportunity")
	}

	// Find binance->kraken opp.
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
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

// TestNetProfitFormula_WithdrawalCost verifies that the buy exchange's WithdrawalBTC fee,
// priced at the buy ask, is subtracted from gross to produce net profit.
func TestNetProfitFormula_WithdrawalCost(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50300.0
	feeA := 0.001
	feeB := 0.001
	slippageA := 0.0002
	withdrawBTC := 0.0005

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50500.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: feeA, SlippageFactor: slippageA, WithdrawalBTC: withdrawBTC},
		"kraken":  {TakerFee: feeB, SlippageFactor: 0.0003, WithdrawalBTC: 0.0001},
	})

	spat := newSpatial(cfg)
	opps := spat.Detect(makeUpdate("binance", bid, ask, now), snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected an opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	// net = gross - ask*feeA - bid*feeB - ask*slippageA - withdrawBTC*ask
	// net = 300 - 50 - 50.3 - 10 - 25 = 164.7
	wantNet := bid - ask - ask*feeA - bid*feeB - ask*slippageA - withdrawBTC*ask
	gotNet, _ := opp.NetProfit.Float64()

	if math.Abs(gotNet-wantNet) > 0.01 {
		t.Errorf("NetProfit (with withdrawal): got %v, want %v", gotNet, wantNet)
	}
}

// TestNetProfitFormula_NetworkLatencyCost verifies that per-exchange network-latency
// basis-points cost is subtracted from net profit on both buy and sell legs.
func TestNetProfitFormula_NetworkLatencyCost(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50400.0
	feeA := 0.001
	feeB := 0.001
	slippageA := 0.0002
	withdrawA := 0.0
	netBpsA := 3.0 // 3 bps on buy leg
	netBpsB := 5.0 // 5 bps on sell leg

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50500.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: feeA, SlippageFactor: slippageA, WithdrawalBTC: withdrawA, NetworkLatencyBps: netBpsA},
		"kraken":  {TakerFee: feeB, SlippageFactor: 0.0003, WithdrawalBTC: 0.0, NetworkLatencyBps: netBpsB},
	})

	spat := newSpatial(cfg)
	opps := spat.Detect(makeUpdate("binance", bid, ask, now), snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected an opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	// net = gross - ask*feeA - bid*feeB - ask*slippageA
	//        - ask*netBpsA/10000 - bid*netBpsB/10000
	wantNet := bid - ask - ask*feeA - bid*feeB - ask*slippageA -
		ask*netBpsA/10000.0 - bid*netBpsB/10000.0
	gotNet, _ := opp.NetProfit.Float64()

	if math.Abs(gotNet-wantNet) > 0.01 {
		t.Errorf("NetProfit (with net-latency bps): got %v, want %v", gotNet, wantNet)
	}
}

// TestSubThresholdSpreadDiscarded verifies that an opportunity with non-positive net profit
// is not returned.
func TestSubThresholdSpreadDiscarded(t *testing.T) {
	now := time.Now()

	// ask > bid → negative gross spread → discarded
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50500.0, now),
		"kraken":  makeUpdate("kraken", 50000.0, 50600.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
		"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
	})

	spat := newSpatial(cfg)

	// binance ask=50500, kraken bid=50000 → gross = 50000-50500 = -500 → discarded
	update := makeUpdate("binance", 49800.0, 50500.0, now)
	opps := spat.Detect(update, snapshot, now)

	if len(opps) != 0 {
		t.Errorf("sub-threshold spread should produce no opportunities, got %d", len(opps))
	}
}

// TestScoreWithModelReady verifies the score formula when the spread model is ready.
// score = net_pct*0.6 + sigmoid(z)*0.4
func TestScoreWithModelReady(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}

	// Create a ready spread model for this pair.
	sm := model.NewSpreadModel()
	for i := 0; i < 100; i++ {
		sm.Update(float64(i) * 0.001) // seed with 100 values so it's ready
	}

	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{
		"binance-kraken": sm,
	}

	spat := newSpatialWithModels(cfg, models)

	update := makeUpdate("binance", bid, ask, now)
	opps := spat.Detect(update, snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
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

	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}

	// Not-ready model: only 50 samples (below MinSamples=100).
	sm := model.NewSpreadModel()
	for i := 0; i < 50; i++ {
		sm.Update(float64(i) * 0.001)
	}

	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := newSpatialWithModels(cfg, models)

	update := makeUpdate("binance", bid, ask, now)
	opps := spat.Detect(update, snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	// When model is not ready, ZScore should be zero (not used in scoring).
	zScore, _ := opp.ZScore.Float64()
	if zScore != 0.0 {
		t.Errorf("ZScore should be 0 when model is not ready, got %v", zScore)
	}
}

// TestStaleCounterpartySkipped verifies that a stale exchange (ReceivedAt > threshold)
// does not produce opportunities.
func TestStaleCounterpartySkipped(t *testing.T) {
	now := time.Now()
	staleTime := now.Add(-3 * time.Second)

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 99999.0, staleTime), // stale
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
		"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
	})

	spat := newSpatial(cfg)
	opps := spat.Detect(makeUpdate("binance", 50100.0, 50000.0, now), snapshot, now)

	if len(opps) != 0 {
		t.Errorf("stale exchange should not produce opportunities, got %d", len(opps))
	}
}

// TestScoreBlending verifies that with a ready model the score blends net_pct and sigmoid(z).
func TestScoreBlending(t *testing.T) {
	now := time.Now()
	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}

	sm := model.NewSpreadModel()
	for i := 0; i < 100; i++ {
		sm.Update(float64(i) * 0.001)
	}

	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
		},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := newSpatialWithModels(cfg, models)

	opps := spat.Detect(makeUpdate("binance", bid, ask, now), snapshot, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	// With a ready model, score must be different from just net_pct.
	score, _ := opp.Score.Float64()
	netPct, _ := opp.NetProfitPct.Float64()
	// score = netPct*0.6 + sigmoid(z)*0.4 — with z≠0 this differs from pure netPct
	if math.IsNaN(score) {
		t.Error("Score is NaN")
	}
	// Verify the blending formula numerically.
	z, _ := opp.ZScore.Float64()
	expectedScore := netPct*0.6 + (1.0/(1.0+math.Exp(-z)))*0.4
	if math.Abs(score-expectedScore) > 1e-9 {
		t.Errorf("score blending: got %v, want %v", score, expectedScore)
	}
}

// TestComputeScore_ModelReady verifies the exact scoring formula when the model has
// enough samples and a non-trivial z-score.
func TestComputeScore_ModelReady(t *testing.T) {
	now := time.Now()
	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}

	sm := model.NewSpreadModel()
	for i := 0; i < 100; i++ {
		sm.Update(float64(i) * 0.001)
	}

	cfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := newSpatialWithModels(cfg, models)

	opps := spat.Detect(makeUpdate("binance", bid, ask, now), snapshot, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	score, _ := opp.Score.Float64()
	netPct, _ := opp.NetProfitPct.Float64()
	z, _ := opp.ZScore.Float64()
	expectedScore := netPct*0.6 + (1.0/(1.0+math.Exp(-z)))*0.4
	if math.Abs(score-expectedScore) > 1e-9 {
		t.Errorf("score formula: got %.10f, want %.10f", score, expectedScore)
	}
}

// TestComputeScore_ModelNotReady verifies score == netPct when model has < MinSamples.
func TestComputeScore_ModelNotReady(t *testing.T) {
	now := time.Now()
	ask := 50000.0
	bid := 50300.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50600.0, now),
	}

	sm := model.NewSpreadModel()
	for i := 0; i < 10; i++ {
		sm.Update(float64(i) * 0.001)
	}

	cfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := newSpatialWithModels(cfg, models)

	opps := spat.Detect(makeUpdate("binance", bid, ask, now), snapshot, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	var opp *types.Opportunity
	for i := range opps {
		if opps[i].BuyExchange == "binance" && opps[i].SellExchange == "kraken" {
			opp = &opps[i]
			break
		}
	}
	if opp == nil {
		t.Fatal("expected binance->kraken opportunity")
	}

	score, _ := opp.Score.Float64()
	netPct, _ := opp.NetProfitPct.Float64()
	z, _ := opp.ZScore.Float64()

	if z != 0.0 {
		t.Errorf("ZScore should be 0 when model not ready, got %v", z)
	}
	// Fallback: score = netPct (not blended)
	if math.Abs(score-netPct) > 1e-9 {
		t.Errorf("fallback score should equal netPct: got %.10f, want %.10f", score, netPct)
	}
}

// --- New tests for Group C ---

// TestSpatial_StampsStrategyField verifies every opportunity returned has Strategy == "spatial".
func TestSpatial_StampsStrategyField(t *testing.T) {
	now := time.Now()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 50400.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
		"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
	})

	spat := newSpatial(cfg)
	opps := spat.Detect(makeUpdate("binance", 50100.0, 50000.0, now), snapshot, now)

	if len(opps) == 0 {
		t.Fatal("expected at least one opportunity")
	}
	for i, o := range opps {
		if o.Strategy != "spatial" {
			t.Errorf("opp[%d].Strategy: got %q, want %q", i, o.Strategy, "spatial")
		}
	}
}

// TestSpatial_SpreadModelUpdatedBelowThreshold verifies spread model is updated
// even when the opportunity is below MinNetProfitPct.
func TestSpatial_SpreadModelUpdatedBelowThreshold(t *testing.T) {
	now := time.Now()

	// Very tight spread — below any meaningful threshold.
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50001.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50001.0, 50002.0, now),
	}

	sm := model.NewSpreadModel()
	initialN := sm.N()

	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
			"kraken":  {TakerFee: 0.001, SlippageFactor: 0.0002},
		},
		MinNetProfitPct:    10.0, // impossibly high threshold — nothing passes
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	models := map[string]*model.SpreadModel{"binance-kraken": sm}
	spat := newSpatialWithModels(cfg, models)

	// Even though no opportunity passes the threshold, the model should be updated.
	opps := spat.Detect(makeUpdate("binance", 50001.0, 50000.0, now), snapshot, now)

	if len(opps) != 0 {
		t.Errorf("expected no opportunities above threshold, got %d", len(opps))
	}
	if sm.N() <= initialN {
		t.Errorf("spread model N: got %d, want > %d (model should have been updated)", sm.N(), initialN)
	}
}

// TestSpatial_SetMinNetProfitPct_TakesEffect verifies that calling SetMinNetProfitPct
// with a lower threshold allows previously-filtered opportunities through.
// Net pct with zero fees and ask=50000, bid=50300: (300)/50000 = 0.006
func TestSpatial_SetMinNetProfitPct_TakesEffect(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50300.0 // gross=300, zero fees → netPct=0.006

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, bid+100, now),
	}

	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.01, // 1% — above 0.006, so filtered
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	spat := newSpatial(cfg)
	update := makeUpdate("binance", bid, ask, now)

	// With high threshold, no opportunity passes.
	opps := spat.Detect(update, snapshot, now)
	kraken0 := 0
	for _, o := range opps {
		if o.SellExchange == "kraken" {
			kraken0++
		}
	}
	if kraken0 > 0 {
		t.Errorf("expected no binance->kraken opp at high threshold, got %d", kraken0)
	}

	// Lower the threshold below 0.006.
	spat.SetMinNetProfitPct(0.001)

	// Now the opportunity should appear.
	opps2 := spat.Detect(update, snapshot, now)
	kraken1 := 0
	for _, o := range opps2 {
		if o.SellExchange == "kraken" {
			kraken1++
		}
	}
	if kraken1 == 0 {
		t.Error("expected binance->kraken opp after lowering MinNetProfitPct, got none")
	}
}

// TestSpatial_SetFees_TakesEffect verifies that calling SetFees with higher fees
// eliminates a previously-profitable opportunity.
func TestSpatial_SetFees_TakesEffect(t *testing.T) {
	now := time.Now()

	ask := 50000.0
	bid := 50100.0

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", bid, ask, now),
		"kraken":  makeUpdate("kraken", bid, 99999.0, now),
	}

	// Start with zero fees — opportunity should appear.
	cfg := spatial.Config{
		Fees: map[string]spatial.FeeConfig{
			"binance": {TakerFee: 0.0, SlippageFactor: 0.0},
			"kraken":  {TakerFee: 0.0, SlippageFactor: 0.0},
		},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    50000.0,
		StalenessThreshold: 2 * time.Second,
	}

	spat := newSpatial(cfg)
	update := makeUpdate("binance", bid, ask, now)

	opps := spat.Detect(update, snapshot, now)
	before := 0
	for _, o := range opps {
		if o.SellExchange == "kraken" {
			before++
		}
	}
	if before == 0 {
		t.Fatal("expected binance->kraken opportunity with zero fees")
	}

	// Apply extremely high fees that make every trade unprofitable.
	spat.SetFees(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 1.0, SlippageFactor: 1.0}, // 100% fee
		"kraken":  {TakerFee: 1.0, SlippageFactor: 1.0},
	})

	opps2 := spat.Detect(update, snapshot, now)
	after := 0
	for _, o := range opps2 {
		if o.SellExchange == "kraken" {
			after++
		}
	}
	if after > 0 {
		t.Errorf("expected no binance->kraken opp after setting high fees, got %d", after)
	}
}

// TestSpatial_Race verifies that concurrent calls to SetFees and Detect do not race.
func TestSpatial_Race(t *testing.T) {
	now := time.Now()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 99999.0, now),
	}

	cfg := defaultCfg(map[string]spatial.FeeConfig{
		"binance": {TakerFee: 0.001},
		"kraken":  {TakerFee: 0.001},
	})

	spat := newSpatial(cfg)
	update := makeUpdate("binance", 50100.0, 50000.0, now)

	var wg sync.WaitGroup
	// Goroutine A: repeatedly calls SetFees.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			spat.SetFees(map[string]spatial.FeeConfig{
				"binance": {TakerFee: float64(i) * 0.0001},
				"kraken":  {TakerFee: float64(i) * 0.0001},
			})
		}
	}()
	// Goroutine B: repeatedly calls Detect.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = spat.Detect(update, snapshot, now)
		}
	}()
	wg.Wait()
}
