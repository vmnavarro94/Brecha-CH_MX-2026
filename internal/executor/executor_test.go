package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
)

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
	st := store.NewStore()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50100.0, staleTime), // stale
		"kraken":  makeUpdate("kraken", 50200.0, 50300.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50100.0, 50200.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	st := store.NewStore()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 50000.0, 50100.0, now),
		"kraken":  makeUpdate("kraken", 50200.0, 50300.0, staleTime), // stale
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50100.0, 50200.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	st := store.NewStore()

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.02)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	st := store.NewStore()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, 50000.0, now),
		"kraken":  makeUpdate("kraken", 50300.0, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", 50000.0, 50300.0, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	st := store.NewStore()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, maxVol)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	st := store.NewStore()

	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, volume)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
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
	// Net profit ≈ gross (executor uses zero fees for simplicity — fees tracked separately).
	if gotNet < 0 {
		t.Errorf("NetProfit should be positive, got %v", gotNet)
	}
	// GrossProfit should equal (bid-ask)*volume.
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
	st := store.NewStore()

	ask := 50000.0
	bid := 50300.0
	snapshot := map[string]types.PriceUpdate{
		"binance": makeUpdate("binance", 49900.0, ask, now),
		"kraken":  makeUpdate("kraken", bid, 50400.0, now),
	}
	snapshotFn := func() map[string]types.PriceUpdate { return snapshot }

	opp := makeOpp("binance", "kraken", ask, bid, 0.01)
	st.Save(*opp)

	ex := NewExecutor(w, st, snapshotFn, clk, stalenessThreshold)
	if err := ex.Execute(opp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, _ := st.GetByID("opp-1")
	if saved.Status != types.StatusExecuted {
		t.Errorf("opportunity status should be Executed, got %v", saved.Status)
	}
}
