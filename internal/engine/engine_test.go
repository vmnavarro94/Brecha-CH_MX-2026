package engine

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
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

// fakeStrategy is a test double that returns a fixed slice of opportunities per Detect call.
type fakeStrategy struct {
	name string
	opps []types.Opportunity
}

func (f *fakeStrategy) Name() string { return f.name }
func (f *fakeStrategy) Detect(_ types.PriceUpdate, _ map[string]types.PriceUpdate, _ time.Time) []types.Opportunity {
	return f.opps
}

// makeOpp creates a minimal Opportunity with the given strategy and score.
func makeOpp(strategy string, score float64) types.Opportunity {
	return types.Opportunity{
		ID:          "opp-" + strategy,
		Strategy:    strategy,
		Score:       decimal.NewFromFloat(score),
		DetectedAt:  time.Now(),
		Status:      types.StatusDetected,
		BuyExchange: "binance",
	}
}

// TestEngine_EmptyStrategiesNoOpp verifies that with no registered strategies,
// ProcessUpdate produces no opportunities.
func TestEngine_EmptyStrategiesNoOpp(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, nil)

	eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))

	_, ok := eng.DequeueTop()
	if ok {
		t.Error("expected no opportunities with no strategies registered")
	}
}

// TestEngine_FanOutAcrossStrategies verifies that two registered strategies each
// contribute opportunities to the heap from a single ProcessUpdate call.
func TestEngine_FanOutAcrossStrategies(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	opp1 := makeOpp("spatial", 0.9)
	opp2 := makeOpp("triangular", 0.7)

	s1 := &fakeStrategy{name: "spatial", opps: []types.Opportunity{opp1}}
	s2 := &fakeStrategy{name: "triangular", opps: []types.Opportunity{opp2}}

	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, []StrategyIface{s1, s2})

	eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))

	first, ok1 := eng.DequeueTop()
	second, ok2 := eng.DequeueTop()

	if !ok1 || !ok2 {
		t.Fatalf("expected 2 opportunities, got ok1=%v ok2=%v", ok1, ok2)
	}
	// Higher score must come first (max-heap).
	score1, _ := first.Score.Float64()
	score2, _ := second.Score.Float64()
	if score1 < score2 {
		t.Errorf("heap ordering: first score %v < second score %v (should be descending)", score1, score2)
	}
}

// TestEngine_MultipleOppsFromOneStrategy verifies that all opportunities returned by
// a single strategy's Detect are added to the heap.
func TestEngine_MultipleOppsFromOneStrategy(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	opps := []types.Opportunity{
		makeOpp("spatial", 0.8),
		makeOpp("spatial", 0.6),
		makeOpp("spatial", 0.4),
	}
	s := &fakeStrategy{name: "spatial", opps: opps}

	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, []StrategyIface{s})

	eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))

	count := 0
	for {
		_, ok := eng.DequeueTop()
		if !ok {
			break
		}
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 opportunities, got %d", count)
	}
}

// TestEngine_NoOverwriteStrategyField verifies that the engine does NOT stamp
// opp.Strategy — it is set by the strategy itself and must be preserved.
func TestEngine_NoOverwriteStrategyField(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	opp := makeOpp("spatial", 0.9)
	opp.Strategy = "spatial" // already stamped by the strategy

	s := &fakeStrategy{name: "spatial", opps: []types.Opportunity{opp}}

	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, []StrategyIface{s})

	eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))

	got, ok := eng.DequeueTop()
	if !ok {
		t.Fatal("expected opportunity")
	}
	if got.Strategy != "spatial" {
		t.Errorf("opp.Strategy: got %q, want %q — engine must not overwrite strategy stamp", got.Strategy, "spatial")
	}
}

// TestEngine_LatencyStats_AfterUpdates verifies that after 10 ProcessUpdate calls
// samples == 10 and both percentiles are > 0.
func TestEngine_LatencyStats_AfterUpdates(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	snapshotFn := func() map[string]types.PriceUpdate {
		return map[string]types.PriceUpdate{
			"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		}
	}
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, nil)
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

	snapshotFn := func() map[string]types.PriceUpdate {
		return map[string]types.PriceUpdate{
			"binance": makeUpdate("binance", 50100.0, 50000.0, now),
		}
	}
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, nil)
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

// TestEngine_DequeueTopRespectsTTL verifies that an opportunity older than TTL
// is evicted and not returned.
func TestEngine_DequeueTopRespectsTTL(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clkDetection := fixedClock{t: base}

	opp := types.Opportunity{
		ID:         "opp-ttl",
		Score:      decimal.NewFromFloat(0.9),
		DetectedAt: base,
		Status:     types.StatusDetected,
	}
	s := &fakeStrategy{name: "spatial", opps: []types.Opportunity{opp}}

	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clkDetection, Config{OpportunityTTL: 500 * time.Millisecond}, []StrategyIface{s})

	eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, base))

	// Advance clock past TTL.
	eng.SetClock(fixedClock{t: base.Add(600 * time.Millisecond)})

	_, ok := eng.DequeueTop()
	if ok {
		t.Error("opportunity older than TTL should be evicted on dequeue")
	}
}

// TestEngine_ProcessedCount verifies ProcessedCount increments per ProcessUpdate call.
func TestEngine_ProcessedCount(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}
	snapshotFn := func() map[string]types.PriceUpdate { return nil }
	eng := NewEngine(snapshotFn, clk, Config{OpportunityTTL: 500 * time.Millisecond}, nil)

	for i := 0; i < 5; i++ {
		eng.ProcessUpdate(makeUpdate("binance", 50100.0, 50000.0, now))
	}
	if got := eng.ProcessedCount(); got != 5 {
		t.Errorf("ProcessedCount: got %d, want 5", got)
	}
}
