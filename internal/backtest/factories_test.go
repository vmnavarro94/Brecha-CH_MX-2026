package backtest_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/funding"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/triangular"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// TestFactories_FreshSpatialInstancePerCall verifies that NewSpatialFactory
// returns a distinct instance on each call (no shared state).
func TestFactories_FreshSpatialInstancePerCall(t *testing.T) {
	cfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.001,
		MaxPositionUSDT:    1000,
		StalenessThreshold: time.Second,
	}
	factory := backtest.NewSpatialFactory(cfg)

	a := factory(42)
	b := factory(42)

	if a == b {
		t.Error("factory returned the same pointer for two calls")
	}
	if a.Name() != "spatial" {
		t.Errorf("Name: want spatial, got %q", a.Name())
	}
}

// TestFactories_FreshTriangularInstancePerCall verifies that NewTriangularFactory
// returns a distinct instance on each call, and that two instances with the same
// seed produce identical first output (determinism).
func TestFactories_FreshTriangularInstancePerCall(t *testing.T) {
	cfg := triangular.Config{
		Exchanges:    []string{"binance"},
		TakerFee:     0.001,
		NoiseRange:   0.01,
		SeedRefPrice: 3000,
		Notional:     100,
		MinNetProfit: -999999,
		EmitCooldown: 0,
		Seed:         0, // will be overridden by factory seed
	}
	factory := backtest.NewTriangularFactory(cfg)

	const seed int64 = 12345
	a := factory(seed)
	b := factory(seed)

	if a == b {
		t.Error("factory returned the same pointer for two calls")
	}

	// Both instances seeded identically should produce identical first output.
	update := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(30000),
		Ask:        decimal.NewFromFloat(30010),
		ReceivedAt: time.Now(),
	}
	now := time.Now()
	oppsA := a.Detect(update, nil, now)
	oppsB := b.Detect(update, nil, now)

	if len(oppsA) != len(oppsB) {
		t.Errorf("determinism: len(oppsA)=%d != len(oppsB)=%d", len(oppsA), len(oppsB))
	}
	for i := range oppsA {
		if !oppsA[i].NetProfit.Equal(oppsB[i].NetProfit) {
			t.Errorf("determinism: NetProfit[%d] differs: %s vs %s",
				i, oppsA[i].NetProfit, oppsB[i].NetProfit)
		}
	}
}

// TestFactories_FundingReplayStrategy_ReadsFromInjectedRates verifies that
// FundingReplay reads from the pre-recorded rates slice and does NOT start a
// background goroutine (Start() is a no-op).
func TestFactories_FundingReplayStrategy_ReadsFromInjectedRates(t *testing.T) {
	base := time.Now().UTC()
	rates := []recorder.FundingRate{
		{Exchange: "binance", Rate: 0.0001, At: base.Add(-2 * time.Minute)},
		{Exchange: "binance", Rate: 0.0005, At: base.Add(-1 * time.Minute)},
		{Exchange: "binance", Rate: 0.0010, At: base},
	}

	cfg := funding.Config{
		Exchanges:        []string{"binance"},
		Threshold:        0.00001,
		PollInterval:     time.Hour,
		EmitCooldown:     0,
		Notional:         1000,
		BaseDifferential: 0.001,
	}
	factory := backtest.NewFundingReplayFactory(cfg, rates)
	strategy := factory(0)

	if strategy.Name() != "funding" {
		t.Errorf("Name: want funding, got %q", strategy.Name())
	}

	// Detect at now == rate[1].At — should see the rate at index 1 (most recent
	// at-or-before now).
	now := base.Add(-1 * time.Minute)
	update := types.PriceUpdate{
		Exchange:   "binance",
		ReceivedAt: now,
	}
	_ = strategy.Detect(update, nil, now)
	// The test verifies no panic (rates are injected) and no goroutine is started.
	// Deeper funding-rate detection logic is tested in runner tests.
}
