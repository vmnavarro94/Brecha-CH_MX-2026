package triangular_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/triangular"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// makeUpdate builds a PriceUpdate for BTC/USDT on the given exchange.
func makeUpdate(exchange string, bid, ask float64, now time.Time) types.PriceUpdate {
	return types.PriceUpdate{
		Exchange:   exchange,
		Bid:        decimal.NewFromFloat(bid),
		Ask:        decimal.NewFromFloat(ask),
		BidSize:    decimal.NewFromFloat(1.0),
		AskSize:    decimal.NewFromFloat(1.0),
		ReceivedAt: now,
		ExchangeAt: now,
	}
}

// defaultCfg returns a Config suitable for most detection tests.
// seed=1 → deterministic PRNG so tests are reproducible.
func defaultCfg() triangular.Config {
	return triangular.Config{
		Exchanges:    []string{"binance"},
		TakerFee:     0.001,
		NoiseRange:   0.0, // tests use SeedRefs to control state
		SeedRefPrice: 2000.0,
		Notional:     1000.0,
		MinNetProfit: 0.0,
		EmitCooldown: 0,
		Seed:         1,
	}
}

// TestTriangular_NoArbWhenRatiosMatch verifies that a balanced graph
// (ethBtcMarket == ethRef/btcMid, the implied cross) produces no opportunity.
// SeedRefs forces deterministic state.
func TestTriangular_NoArbWhenRatiosMatch(t *testing.T) {
	cfg := defaultCfg()
	tri := triangular.New(cfg)
	// ethRef=2000, ethBtcMarket=0.04 (implied for btcMid=50000 → 2000/50000=0.04)
	// ratioA = ethRef / (ask * ethBtcMarket) = 2000 / (50000 * 0.04) = 1.0
	// ratioB = (ethBtcMarket * bid) / ethRef = (0.04 * 50000) / 2000 = 1.0
	// cycleGain = 0 → netProfit = -3*fee*notional < 0 → no opp
	tri.SeedRefs("binance", 2000.0, 0.04)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) != 0 {
		t.Errorf("expected no opportunity on balanced graph, got %d", len(opps))
	}
}

// TestTriangular_DetectsCycleWhenRatioDiverges verifies that when the market BTC/ETH
// cross rate diverges from the implied (ethRef/btcMid), the cycle math produces a
// positive arbitrage opportunity. This is REAL triangular arb: 3 independent prices
// out of equilibrium.
func TestTriangular_DetectsCycleWhenRatioDiverges(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	// ethRef=2000, ethBtcMarket=0.0402 (0.5% above implied 0.04)
	// ratioA = 2000 / (50000 * 0.0402) ≈ 0.99502 (cycle A loses)
	// ratioB = (0.0402 * 50000) / 2000 = 1.005 (cycle B gains 0.5%)
	// cycleGain = 0.005 → after 3*0.0001 fees → netProfit ≈ $4.70 on $1000 notional
	tri.SeedRefs("binance", 2000.0, 0.0402)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity when ethBtcMarket diverges from implied")
	}
	netProfit, _ := opps[0].NetProfit.Float64()
	if netProfit <= 0 {
		t.Errorf("expected NetProfit > 0, got %v", netProfit)
	}
}

// TestTriangular_CostModelSubtracts3TakerFees verifies that netProfit reflects
// the exact 3-leg fee subtraction with the new 3-independent-price formula.
func TestTriangular_CostModelSubtracts3TakerFees(t *testing.T) {
	fee := 0.0001
	notional := 1000.0
	cfg := defaultCfg()
	cfg.TakerFee = fee
	cfg.Notional = notional

	tri := triangular.New(cfg)
	tri.SeedRefs("binance", 2000.0, 0.0402)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)

	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}

	// Compute expected math with the 3-independent-price model.
	ask := 50000.0
	bid := 50000.0
	ethRef := 2000.0
	ethBtcMarket := 0.0402
	ratioA := ethRef / (ask * ethBtcMarket)
	ratioB := (ethBtcMarket * bid) / ethRef
	cycleGain := ratioA
	if ratioB > ratioA {
		cycleGain = ratioB
	}
	cycleGain -= 1.0
	wantNetProfit := (cycleGain - 3*fee) * notional

	gotNetProfit, _ := opps[0].NetProfit.Float64()
	if math.Abs(gotNetProfit-wantNetProfit) > 1e-9 {
		t.Errorf("NetProfit: got %v, want %v (cycleGain=%v, fee=%v)", gotNetProfit, wantNetProfit, cycleGain, fee)
	}
}

// TestTriangular_EthRefParticipatesInMath verifies that changing ethRef alone (with
// ethBtcMarket fixed) changes the cycle ratios. Guards against ethRef being
// computed but discarded.
func TestTriangular_EthRefParticipatesInMath(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0
	tri1 := triangular.New(cfg)
	tri2 := triangular.New(cfg)

	// Same ethBtcMarket but different ethRef.
	tri1.SeedRefs("binance", 2000.0, 0.04)
	tri2.SeedRefs("binance", 2100.0, 0.04) // 5% higher ethRef

	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)

	opps1 := tri1.Detect(update, nil, now)
	opps2 := tri2.Detect(update, nil, now)

	// tri1: balanced → no opp
	// tri2: ethRef diverges from implied → must produce opp (cycleGain != 0)
	if len(opps1) != 0 {
		t.Errorf("tri1 (balanced) should not emit, got %d opps", len(opps1))
	}
	if len(opps2) == 0 {
		t.Fatal("tri2 (ethRef diverged) must emit — proves ethRef participates in math")
	}
	np2, _ := opps2[0].NetProfit.Float64()
	if np2 <= 0 {
		t.Errorf("tri2 NetProfit: got %v, want > 0", np2)
	}
}

// TestTriangular_StampsStrategyName verifies opp.Strategy == "triangular" (spec TR3).
func TestTriangular_StampsStrategyName(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	tri.SeedRefs("binance", 2000.0, 0.0402)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	if opps[0].Strategy != "triangular" {
		t.Errorf("Strategy: got %q, want %q", opps[0].Strategy, "triangular")
	}
}

// TestTriangular_IntraExchange verifies opp.BuyExchange == opp.SellExchange (spec TR3).
func TestTriangular_IntraExchange(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	tri.SeedRefs("binance", 2000.0, 0.0402)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	if opps[0].BuyExchange != "binance" {
		t.Errorf("BuyExchange: got %q, want %q", opps[0].BuyExchange, "binance")
	}
	if opps[0].SellExchange != "binance" {
		t.Errorf("SellExchange: got %q, want %q", opps[0].SellExchange, "binance")
	}
}

// TestTriangular_Race verifies no data race when SetTakerFee, SeedRefs and Detect
// run concurrently (spec TR4).
func TestTriangular_Race(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	tri.SeedRefs("binance", 2000.0, 0.0402)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			tri.SetTakerFee(float64(i) * 0.0001)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = tri.Detect(update, nil, now)
		}
	}()
	wg.Wait()
}
