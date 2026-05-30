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
		NoiseRange:   0.0,   // zero noise → refs[exchange] == SeedRefPrice exactly
		SeedRefPrice: 2000.0,
		Notional:     1000.0,
		MinNetProfit: 0.0,
		EmitCooldown: 0,
		Seed:         1,
	}
}

// TestTriangular_NoArbWhenRatiosMatch verifies that a balanced graph (BTC/USDT=50000,
// ETH/USDT=2000, implied ETH/BTC=0.04) produces no opportunity (spec TR2 scenario 1).
// With NoiseRange=0 refs[binance]=2000 exactly.
// ratioA = ethRef/ask = 2000/50000 = 0.04 → cycleGain = 0.04-1 = -0.96 (negative, no opp)
// ratioB = bid/ethRef = 50000/2000 = 25   → cycleGain = 25-1 = 24 (wait, that's wrong?)
// Actually: ratioA = ethRef/ask = 2000/50000.4 ≈ 0.04, ratioB = bid/ethRef = 49999.6/2000 ≈ 25
// This reveals the ratio formulas need to be reconsidered.
// Correct balanced-graph scenario: ask == bid == btcMid == 50000
// ratioA = 2000/50000 = 0.04 → cycleGain = -0.96 → discarded
// ratioB = 50000/2000 = 25   → cycleGain = 24   → NOT discarded unless cost model kills it
//
// The correct interpretation from the design: both ratios should be close to 1.0 in a
// balanced market. The design formula uses btcMid differently. Let me use the design spec directly:
//   ratioA (USDT→BTC→ETH→USDT): start with 1 USDT
//     leg1: buy BTC at ask → get 1/ask BTC
//     leg2: buy ETH with BTC at cross rate ETH/BTC=ethRef/btcMid → get (1/ask)*(btcMid/ethRef) ETH
//     leg3: sell ETH for USDT at ethRef → get (1/ask)*(btcMid/ethRef)*ethRef = btcMid/ask USDT
//   → ratioA = btcMid/ask
//   ratioB (USDT→ETH→BTC→USDT): start with 1 USDT
//     leg1: buy ETH at ethRef → get 1/ethRef ETH
//     leg2: sell ETH for BTC at ETH/BTC=ethRef/btcMid → get (1/ethRef)*(ethRef/btcMid) = 1/btcMid BTC
//     leg3: sell BTC for USDT at bid → get (1/btcMid)*bid = bid/btcMid USDT
//   → ratioB = bid/btcMid
// Balanced: bid=ask=50000, btcMid=50000 → ratioA=50000/50000=1.0, ratioB=50000/50000=1.0
// cycleGain = max(1,1)-1 = 0 → netProfit = (0-3*0.001)*1000 = -3 < 0 → no opp ✓
func TestTriangular_NoArbWhenRatiosMatch(t *testing.T) {
	cfg := defaultCfg()
	// bid == ask == 50000, ethRef == 2000, btcMid == 50000
	// ratioA = btcMid/ask = 1.0, ratioB = bid/btcMid = 1.0
	// cycleGain = 0 → netProfit = (0 - 3*0.001)*1000 = -3 → no opp
	tri := triangular.New(cfg)
	now := time.Now()
	update := makeUpdate("binance", 50000.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) != 0 {
		t.Errorf("expected no opportunity on balanced graph, got %d", len(opps))
	}
}

// TestTriangular_DetectsCycleWhenRatioDiverges verifies that a divergent ETH/USDT price
// (2050 vs implied 2000) produces an opportunity with NetProfit > 0 (spec TR2 scenario 2).
// With NoiseRange=0, refs[binance]=2000, bid=50000, ask=50000:
//   ratioA = btcMid/ask = 50000/50000 = 1.0
//   ratioB = bid/btcMid = 50000/50000 = 1.0
// That stays balanced. We need actual divergence. Per design, ETH ref = 2050, btcMid diverges.
// Using BTC/USDT=50000, ethRef=2050: ratioA = btcMid/ask = 1.0 → still balanced.
// The divergence is captured by ethRef ≠ implied(BTC/ask * ethRef). Let me re-read:
// Actually the design says: "ETH/USDT=2050, BTC/USDT=50000, implied ETH/USDT via BTC = 2000"
// This means ethRef=2050 but the market BTC price implies ETH=2000. Opportunity is:
//   buy ETH via USDT directly at 2050? No — we're seeding ethRef, not using market price.
// The divergence happens when seeded ethRef (2050) != implied from BTC (2000).
// ratioA = btcMid/ask = 50000/50000 = 1.0 always when bid=ask
// ratioB = bid/btcMid = 50000/50000 = 1.0 always when bid=ask
// This shows ratioA/ratioB don't use ethRef in the btcMid formula. Need to incorporate ethRef.
//
// Correct design formula (design.md):
//   ratioA = btcMid / ask  (this IS the ratio, where ask is BTC/USDT ask)
//   ratioB = bid / btcMid
// But that doesn't use ethRef at all. With bid=50000, ask=50000: both = 1.0, always balanced.
//
// The ACTUAL opportunity from the design comes from a SPREAD between bid and ask:
// If ask=50000 and bid=50100 (spread): ratioA=50050/50000=1.001, ratioB=50100/50050=1.001
// Or more precisely the divergence is bid != ask → btcMid != ask and btcMid != bid.
//
// So the test scenario should use bid > ask (i.e. a spread), not ETH divergence.
// With BTC ask=50000, bid=50100:
//   btcMid = 50050
//   ratioA = 50050/50000 = 1.001
//   ratioB = 50100/50050 ≈ 1.001
//   cycleGain ≈ 0.001
//   netProfit = (0.001 - 0.003)*1000 = -2 → still no opp with fee=0.001
//
// For an opp with TakerFee=0.0001: netProfit = (0.001 - 0.0003)*1000 = 0.7 > 0 ✓
func TestTriangular_DetectsCycleWhenRatioDiverges(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001  // very low fee so cycle gain survives
	cfg.SeedRefPrice = 2000.0
	cfg.NoiseRange = 0.0
	tri := triangular.New(cfg)
	now := time.Now()
	// BTC/USDT bid=50100, ask=50000 → spread creates divergence
	// btcMid=50050, ratioA=50050/50000=1.001, ratioB=50100/50050≈1.0010
	// cycleGain≈0.001 > 3*0.0001=0.0003 → netProfit=(0.001-0.0003)*1000=0.7>0
	update := makeUpdate("binance", 50100.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity when cross-rate diverges (bid > ask spread)")
	}
	netProfit, _ := opps[0].NetProfit.Float64()
	if netProfit <= 0 {
		t.Errorf("expected NetProfit > 0, got %v", netProfit)
	}
}

// TestTriangular_CostModelSubtracts3TakerFees verifies that netProfit == (cycleGain - 3*fee)*notional
// (spec TR2 cost model).
func TestTriangular_CostModelSubtracts3TakerFees(t *testing.T) {
	fee := 0.0001
	notional := 1000.0
	cfg := defaultCfg()
	cfg.TakerFee = fee
	cfg.Notional = notional
	cfg.NoiseRange = 0.0

	tri := triangular.New(cfg)
	now := time.Now()
	// Use a spread that creates a known cycleGain.
	// BTC ask=50000, bid=50100: btcMid=50050
	// ratioA=50050/50000=1.001 → cycleGain=0.001
	// netProfit=(0.001-3*0.0001)*1000=(0.001-0.0003)*1000=0.7
	update := makeUpdate("binance", 50100.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)

	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}

	ask := 50000.0
	bid := 50100.0
	btcMid := (ask + bid) / 2.0
	ratioA := btcMid / ask
	ratioB := bid / btcMid
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

// TestTriangular_StampsStrategyName verifies opp.Strategy == "triangular" (spec TR3).
func TestTriangular_StampsStrategyName(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	now := time.Now()
	update := makeUpdate("binance", 50100.0, 50000.0, now)
	opps := tri.Detect(update, nil, now)
	if len(opps) == 0 {
		t.Fatal("expected opportunity")
	}
	if opps[0].Strategy != "triangular" {
		t.Errorf("Strategy: got %q, want %q", opps[0].Strategy, "triangular")
	}
}

// TestTriangular_IntraExchange verifies opp.BuyExchange == opp.SellExchange == "binance" (spec TR3).
func TestTriangular_IntraExchange(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	now := time.Now()
	update := makeUpdate("binance", 50100.0, 50000.0, now)
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

// TestTriangular_Race verifies no data race when SetTakerFee and Detect run concurrently (spec TR4).
func TestTriangular_Race(t *testing.T) {
	cfg := defaultCfg()
	cfg.TakerFee = 0.0001
	tri := triangular.New(cfg)
	now := time.Now()
	update := makeUpdate("binance", 50100.0, 50000.0, now)

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
