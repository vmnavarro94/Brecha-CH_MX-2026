package executor

import (
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/depth"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
)

// seededDepthCfg returns a depth.Config with a fixed seed for deterministic tests.
// N=1, min=max=0.1 → single level, qty always 0.1, vwap == bbo (no multi-level walk).
func seededDepthCfg(seed int64) depth.Config {
	return depth.Config{
		N:         1,
		StepPct:   0.0001,
		MinQtyBTC: 0.1,
		MaxQtyBTC: 0.1,
		Rand:      rand.New(rand.NewSource(seed)),
	}
}

// multiLevelDepthCfg returns a config with N=3 levels of fixed qty per level (no random),
// step_pct=0.0001 → 0.01% gap between levels. Required to assert that VWAP != BBO when
// walking multiple levels, which is needed by the adversarial reversal test.
func multiLevelDepthCfg(seed int64, qtyPerLevel float64) depth.Config {
	return depth.Config{
		N:         3,
		StepPct:   0.0001,
		MinQtyBTC: qtyPerLevel,
		MaxQtyBTC: qtyPerLevel,
		Rand:      rand.New(rand.NewSource(seed)),
	}
}

// fixedClock always returns the configured time.
type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

const stalenessThreshold = 2 * time.Second

func makeOpp(buyEx, sellEx string, buyPrice, sellPrice, maxVol float64) *types.Opportunity {
	return &types.Opportunity{
		ID:           "opp-1",
		BuyExchange:  buyEx,
		SellExchange: sellEx,
		BuyPrice:     decimal.NewFromFloat(buyPrice),
		SellPrice:    decimal.NewFromFloat(sellPrice),
		MaxVolume:    decimal.NewFromFloat(maxVol),
		DetectedAt:   time.Now(),
		Status:       types.StatusDetected,
	}
}

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

// TestStaleBuyPriceReturnsError verifies that a stale buy-side price marks the opportunity
// as Expired and returns ErrStalePrice.
func TestStaleBuyPriceReturnsError(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}
	staleTime := now.Add(-3 * time.Second)

	w := wallet.NewMultiWallet([]string{"binance", "kraken"}, map[string]float64{"USDT": 1000.0, "BTC": 1.0})
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50100.0, staleTime), // stale
		"kraken":  makeUpdate("kraken", 50200.0, 50300.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50100.0, 50200.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	err := ex.Execute(opp)

	if !errors.Is(err, ErrStalePrice) {
		t.Errorf("expected ErrStalePrice, got %v", err)
	}
	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusExpired {
		t.Errorf("opportunity status should be Expired, got %v", saved.Status)
	}
}

// TestStaleSellPriceReturnsError verifies that a stale sell-side price marks the opportunity
// as Expired and returns ErrStalePrice.
func TestStaleSellPriceReturnsError(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}
	staleTime := now.Add(-3 * time.Second)

	w := wallet.NewMultiWallet([]string{"binance", "kraken"}, map[string]float64{"USDT": 1000.0, "BTC": 1.0})
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50100.0, now),
		"kraken":  makeUpdate("kraken", 50200.0, 50300.0, staleTime), // stale
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50100.0, 50200.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	err := ex.Execute(opp)

	if !errors.Is(err, ErrStalePrice) {
		t.Errorf("expected ErrStalePrice, got %v", err)
	}
	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusExpired {
		t.Errorf("opportunity status should be Expired, got %v", saved.Status)
	}
}

// TestFreshPricesVolume verifies volume = min(MaxVolume, walletBalance/currentAsk).
func TestFreshPricesVolume(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	// Wallet has 500 USDT. ask=50000. walletBalance/ask = 500/50000 = 0.01
	// MaxVolume = 0.02. min(0.02, 0.01) = 0.01
	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 500.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.02)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	err := ex.Execute(opp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected volume = min(0.02, 500/50000) = min(0.02, 0.01) = 0.01
	trades := st.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	vol, _ := trades[0].Volume.Float64()
	if vol != 0.01 {
		t.Errorf("volume: got %v, want 0.01", vol)
	}
}

// TestZeroVolumeSkipsExecution verifies that zero computable volume sets status=Skipped
// and returns ErrInsufficientBalance.
func TestZeroVolumeSkipsExecution(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	// Wallet has 0 USDT → volume = 0/ask = 0 → skipped.
	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 0.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50000.0, 50300.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	err := ex.Execute(opp)

	if !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("expected ErrInsufficientBalance, got %v", err)
	}
	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusSkipped {
		t.Errorf("opportunity status should be Skipped, got %v", saved.Status)
	}
}

// TestSuccessfulExecutionUpdatesWallets verifies that a successful execution debits USDT
// from the buy exchange, credits BTC there, debits BTC from the sell exchange, and credits
// USDT there.
func TestSuccessfulExecutionUpdatesWallets(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	const initialUSDT = 1000.0
	const initialBTC = 1.0
	ask := 50000.0
	bid := 50300.0
	maxVol := 0.01 // volume = min(0.01, 1000/50000=0.02) = 0.01

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": initialUSDT, "BTC": initialBTC},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, maxVol)
	st.Save(*opp)

	// depth cfg: N=1, qty=0.1 → enough for maxVol=0.01; vwap == bbo
	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	volume := maxVol
	cost := volume * ask

	// Buy side (binance): USDT debited, BTC credited.
	gotUSDT := w.Balance("binance", "USDT")
	if gotUSDT != initialUSDT-cost {
		t.Errorf("binance USDT: got %v, want %v", gotUSDT, initialUSDT-cost)
	}
	gotBTC := w.Balance("binance", "BTC")
	if gotBTC != initialBTC+volume {
		t.Errorf("binance BTC: got %v, want %v", gotBTC, initialBTC+volume)
	}

	// Sell side (kraken): BTC debited, USDT credited.
	gotKrakenBTC := w.Balance("kraken", "BTC")
	if gotKrakenBTC != initialBTC-volume {
		t.Errorf("kraken BTC: got %v, want %v", gotKrakenBTC, initialBTC-volume)
	}
	krakenUSDT := w.Balance("kraken", "USDT")
	proceeds := volume * bid
	if krakenUSDT != initialUSDT+proceeds {
		t.Errorf("kraken USDT: got %v, want %v", krakenUSDT, initialUSDT+proceeds)
	}
}

// TestSuccessfulExecutionSavesTrade verifies a Trade record is persisted with correct NetProfit.
func TestSuccessfulExecutionSavesTrade(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50300.0
	volume := 0.01

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 1000.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, volume)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	trade := trades[0]

	// Gross profit = (bid - ask) * volume
	grossProfit := (bid - ask) * volume
	gotNet, _ := trade.NetProfit.Float64()
	if gotNet < 0 {
		t.Errorf("NetProfit should be positive, got %v", gotNet)
	}
	gotGross, _ := trade.GrossProfit.Float64()
	if gotGross != grossProfit {
		t.Errorf("GrossProfit: got %v, want %v", gotGross, grossProfit)
	}
}

// TestSuccessfulExecutionUpdatesOpportunityStatus verifies the opportunity is marked Executed.
func TestSuccessfulExecutionUpdatesOpportunityStatus(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 1000.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusExecuted {
		t.Errorf("opportunity status should be Executed, got %v", saved.Status)
	}
}

// TestExecute_BuyPriceIsVWAP asserts Trade.BuyPrice equals the VWAP from the walk.
// With seededDepthCfg (N=1, qty=0.1, ask=50000), single level covers target → vwap=bboAsk.
// spec D4
func TestExecute_BuyPriceIsVWAP(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50300.0

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 1000.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, seededDepthCfg(1))
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	// Single level, full fill → vwap == bboAsk
	bp, _ := trades[0].BuyPrice.Float64()
	if bp != ask {
		t.Errorf("BuyPrice (vwap): got %v, want %v", bp, ask)
	}
}

// TestExecute_PartialFillFlag verifies Trade.PartialFill=true + RequestedVolume when
// available liquidity < requested.
// spec D5
func TestExecute_PartialFillFlag(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0
	bid := 50300.0

	// depth cfg: N=1, qty=0.005 → max available = 0.005 BTC per side
	// wallet has enough USDT; opp MaxVolume=0.01 → partial fill expected on buy
	partialCfg := depth.Config{
		N:         1,
		StepPct:   0.0001,
		MinQtyBTC: 0.005,
		MaxQtyBTC: 0.005,
		Rand:      rand.New(rand.NewSource(42)),
	}

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 1000.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, partialCfg)
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	trade := trades[0]
	if !trade.PartialFill {
		t.Errorf("PartialFill: got false, want true")
	}
	rv, _ := trade.RequestedVolume.Float64()
	if rv != 0.01 {
		t.Errorf("RequestedVolume: got %v, want 0.01", rv)
	}
	vol, _ := trade.Volume.Float64()
	if vol >= 0.01 {
		t.Errorf("Volume should be less than RequestedVolume, got %v", vol)
	}
}

// TestExecute_ReversalUsesVWAPCost asserts that when the sell leg fails, the wallet is
// credited back the exact VWAP-accumulated buy cost, not bboAsk*volume. Uses a 3-level
// depth config so vwapBuy != bboAsk — a bug that credits BBO would leave a balance gap.
// spec D6
func TestExecute_ReversalUsesVWAPCost(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	bboAsk := 50000.0

	// kraken has 0 BTC → sell-side Debit(BTC) will fail → reversal triggered.
	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 1000.0, "BTC": 0.0},
	)

	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, bboAsk, now),
		"kraken":  makeUpdate("kraken", 50300.0, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	// Multi-level depth: N=3, 0.005 BTC per level, step 0.01% → ask levels at:
	//   L0 50000.00 × 0.005
	//   L1 50005.00 × 0.005
	//   L2 50010.00 × 0.005
	// Target 0.012 BTC walks all 3 levels with 0.002 taken from L2.
	// vwapBuy = (0.005*50000 + 0.005*50005 + 0.002*50010) / 0.012 = 50003.75
	// cost_vwap = 0.012 * 50003.75 = 600.045
	// cost_bbo  = 0.012 * 50000.00 = 600.000   (the buggy value)
	const targetVol = 0.012
	const expectedVWAP = 50003.75
	const expectedCost = targetVol * expectedVWAP

	opp := makeOpp("binance", "kraken", bboAsk, 50300.0, targetVol)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, multiLevelDepthCfg(1, 0.005))
	err := ex.Execute(opp)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance (sell-side reversal), got %v", err)
	}

	// After reversal: binance USDT must be back to 1000 EXACTLY.
	// If reversal credited bboAsk*vol (600.000) instead of vwap*vol (600.045),
	// USDT would be 999.955 — the test would catch it.
	restoredUSDT := w.Balance("binance", "USDT")
	const tolerance = 1e-9
	if math.Abs(restoredUSDT-1000.0) > tolerance {
		t.Errorf("binance USDT after reversal: got %.6f, want 1000.0 (VWAP=%.4f, expectedCost=%.6f)",
			restoredUSDT, expectedVWAP, expectedCost)
	}
	restoredBTC := w.Balance("binance", "BTC")
	if math.Abs(restoredBTC) > tolerance {
		t.Errorf("binance BTC after reversal: got %v, want 0", restoredBTC)
	}
}

// TestExecute_AsymmetricFillAborts verifies that when sellFilled < buyFilled,
// the result is Skipped and no net BTC change remains.
// design: atomic-or-nothing
//
// Seed 0 with [min=0.005, max=0.025]: ask level qty ≈ 0.0239, bid level qty ≈ 0.0099.
// MaxVolume=0.02 → buyFilled=0.02 (ask covers it), sellFilled≈0.0099 (bid doesn't) → abort.
func TestExecute_AsymmetricFillAborts(t *testing.T) {
	now := time.Now()
	clk := fixedClock{t: now}

	ask := 50000.0

	// seed=0: ask level qty≈0.024 > 0.02 target, bid level qty≈0.0099 < 0.02 target
	// → buyFilled=0.02 (full), sellFilled≈0.0099 < buyFilled → abort
	asymCfg := depth.Config{
		N:         1,
		StepPct:   0.0001,
		MinQtyBTC: 0.005,
		MaxQtyBTC: 0.025,
		Rand:      rand.New(rand.NewSource(0)),
	}

	w := wallet.NewMultiWallet(
		[]string{"binance", "kraken"},
		map[string]float64{"USDT": 2000.0, "BTC": 1.0},
	)
	st := store.NewStore(t.TempDir())

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", 50300.0, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, 50300.0, 0.02)
	st.Save(*opp)

	initialBinanceBTC := w.Balance("binance", "BTC")
	initialKrakenBTC := w.Balance("kraken", "BTC")

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold, asymCfg)
	err := ex.Execute(opp)

	// Asymmetric fill: sell fills less than buy → skipped
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance on asymmetric fill, got %v", err)
	}
	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusSkipped {
		t.Errorf("status: got %v, want Skipped", saved.Status)
	}
	// No net BTC change.
	if w.Balance("binance", "BTC") != initialBinanceBTC {
		t.Errorf("binance BTC changed after abort: got %v, want %v", w.Balance("binance", "BTC"), initialBinanceBTC)
	}
	if w.Balance("kraken", "BTC") != initialKrakenBTC {
		t.Errorf("kraken BTC changed after abort: got %v, want %v", w.Balance("kraken", "BTC"), initialKrakenBTC)
	}
}
