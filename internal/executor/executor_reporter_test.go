package executor

import (
	"testing"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/depth"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
	"math/rand"
)

// fakeReporter captures RecordTradeReturn calls for assertions.
type fakeReporter struct {
	calls []reporterCall
}

type reporterCall struct {
	buyEx  string
	sellEx string
	netPct float64
}

func (f *fakeReporter) RecordTradeReturn(buyEx, sellEx string, netPct float64) {
	f.calls = append(f.calls, reporterCall{buyEx, sellEx, netPct})
}

func reporterDepthCfg() depth.Config {
	return depth.Config{
		N:         1,
		StepPct:   0.0001,
		MinQtyBTC: 0.1,
		MaxQtyBTC: 0.1,
		Rand:      rand.New(rand.NewSource(1)),
	}
}

func makeOppWithStrategy(buyEx, sellEx string, buyPrice, sellPrice, maxVol float64, strategy string) *types.Opportunity {
	opp := makeOpp(buyEx, sellEx, buyPrice, sellPrice, maxVol)
	opp.Strategy = strategy
	return opp
}

// TestSpotExecutor_CallsRecordTradeReturn_ForSpatial verifies I1:
// reporter is called after a successful spatial trade.
func TestSpotExecutor_CallsRecordTradeReturn_ForSpatial(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	w := wallet.NewMultiWallet([]string{"binance", "kraken"}, map[string]float64{"USDT": 1000.0, "BTC": 1.0})
	st := store.NewStore(t.TempDir())

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOppWithStrategy("binance", "kraken", ask, bid, 0.01, "spatial")
	st.Save(*opp)

	reporter := &fakeReporter{}
	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, reporterDepthCfg())
	ex = ex.WithTradeReturnReporter(reporter)

	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reporter.calls) != 1 {
		t.Fatalf("RecordTradeReturn calls: got %d, want 1", len(reporter.calls))
	}
	call := reporter.calls[0]
	if call.buyEx != "binance" {
		t.Errorf("buyEx: got %q, want %q", call.buyEx, "binance")
	}
	if call.sellEx != "kraken" {
		t.Errorf("sellEx: got %q, want %q", call.sellEx, "kraken")
	}
	// netPct should be (vwapSell - vwapBuy) / vwapBuy — positive value.
	if call.netPct <= 0 {
		t.Errorf("netPct should be positive, got %v", call.netPct)
	}
}

// TestSpotExecutor_DoesNotCallRecordTradeReturn_ForTriangular verifies I1:
// reporter is NOT called for non-spatial strategies.
func TestSpotExecutor_DoesNotCallRecordTradeReturn_ForTriangular(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	w := wallet.NewMultiWallet([]string{"binance", "kraken"}, map[string]float64{"USDT": 1000.0, "BTC": 1.0})
	st := store.NewStore(t.TempDir())

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOppWithStrategy("binance", "kraken", ask, bid, 0.01, "triangular")
	st.Save(*opp)

	reporter := &fakeReporter{}
	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, reporterDepthCfg())
	ex = ex.WithTradeReturnReporter(reporter)

	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reporter.calls) != 0 {
		t.Errorf("RecordTradeReturn should NOT be called for triangular, got %d calls", len(reporter.calls))
	}
}
