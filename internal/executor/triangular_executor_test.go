package executor

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// TestTriangularExecutor_RecordsTradeWithoutWallet verifies that Execute persists a Trade
// without wallet mutations (ADR-3 / spec FE1 parallel).
func TestTriangularExecutor_RecordsTradeWithoutWallet(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	takerFee := 0.001
	notional := 1000.0
	exec := NewTriangularExecutor(st, clk, takerFee, notional)

	opp := &types.Opportunity{
		ID:           "opp-tri-1",
		BuyExchange:  "binance",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(0.7),
		Strategy:     "triangular",
		Status:       types.StatusDetected,
	}

	err := exec.Execute(opp)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) == 0 {
		t.Fatal("expected at least one persisted trade")
	}
}

// TestTriangularExecutor_NetProfitEqualsOppNetProfit verifies trade.NetProfit == opp.NetProfit.
func TestTriangularExecutor_NetProfitEqualsOppNetProfit(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	takerFee := 0.001
	notional := 1000.0
	exec := NewTriangularExecutor(st, clk, takerFee, notional)

	wantNetProfit := 0.7
	opp := &types.Opportunity{
		ID:           "opp-tri-2",
		BuyExchange:  "binance",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(wantNetProfit),
		Strategy:     "triangular",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) == 0 {
		t.Fatal("expected persisted trade")
	}
	gotNetProfit, _ := trades[0].NetProfit.Float64()
	if math.Abs(gotNetProfit-wantNetProfit) > 1e-9 {
		t.Errorf("NetProfit: got %v, want %v", gotNetProfit, wantNetProfit)
	}
}

// TestTriangularExecutor_StampsStrategy verifies trade.Strategy == "triangular".
func TestTriangularExecutor_StampsStrategy(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewTriangularExecutor(st, clk, 0.001, 1000.0)

	opp := &types.Opportunity{
		ID:           "opp-tri-3",
		BuyExchange:  "binance",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(0.7),
		Strategy:     "triangular",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) == 0 {
		t.Fatal("expected persisted trade")
	}
	if trades[0].Strategy != "triangular" {
		t.Errorf("Strategy: got %q, want %q", trades[0].Strategy, "triangular")
	}
}

// TestTriangularExecutor_FeeModel verifies:
//   - fees = 3 * takerFee * notional
//   - grossProfit = opp.NetProfit + fees
//   - netProfit = opp.NetProfit
//   - Volume = notional
func TestTriangularExecutor_FeeModel(t *testing.T) {
	takerFee := 0.001
	notional := 1000.0
	netProfitF := 0.7

	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewTriangularExecutor(st, clk, takerFee, notional)

	opp := &types.Opportunity{
		ID:           "opp-tri-4",
		BuyExchange:  "binance",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(netProfitF),
		Strategy:     "triangular",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) == 0 {
		t.Fatal("expected persisted trade")
	}
	tr := trades[0]

	wantFees := 3 * takerFee * notional
	wantGrossProfit := netProfitF + wantFees

	gotFees, _ := tr.Fees.Float64()
	if math.Abs(gotFees-wantFees) > 1e-9 {
		t.Errorf("Fees: got %v, want %v", gotFees, wantFees)
	}

	gotGrossProfit, _ := tr.GrossProfit.Float64()
	if math.Abs(gotGrossProfit-wantGrossProfit) > 1e-9 {
		t.Errorf("GrossProfit: got %v, want %v", gotGrossProfit, wantGrossProfit)
	}

	vol, _ := tr.Volume.Float64()
	if vol != notional {
		t.Errorf("Volume: got %v, want %v", vol, notional)
	}
}

// TestTriangularExecutor_SetsStatusExecuted verifies opp.Status is set to StatusExecuted.
func TestTriangularExecutor_SetsStatusExecuted(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewTriangularExecutor(st, clk, 0.001, 1000.0)

	opp := &types.Opportunity{
		ID:           "opp-tri-5",
		BuyExchange:  "binance",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(0.7),
		Strategy:     "triangular",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if opp.Status != types.StatusExecuted {
		t.Errorf("opp.Status: got %q, want %q", opp.Status, types.StatusExecuted)
	}
}
