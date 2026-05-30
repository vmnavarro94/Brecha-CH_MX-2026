package executor

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// TestFundingExecutor_RecordsTradeWithoutWallet verifies that Execute persists a Trade and
// does not require wallet operations (spec FE1: "no wallet mutation occurs").
func TestFundingExecutor_RecordsTradeWithoutWallet(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewFundingExecutor(st, clk)

	opp := &types.Opportunity{
		ID:           "opp-funding-1",
		BuyExchange:  "bybit",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(12.5),
		Strategy:     "funding",
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

// TestFundingExecutor_NetProfitEqualsOppNetProfit verifies trade.NetProfit == opp.NetProfit (spec FE1).
func TestFundingExecutor_NetProfitEqualsOppNetProfit(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewFundingExecutor(st, clk)

	wantNetProfit := 12.5
	opp := &types.Opportunity{
		ID:           "opp-funding-2",
		BuyExchange:  "bybit",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(wantNetProfit),
		Strategy:     "funding",
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
	if gotNetProfit != wantNetProfit {
		t.Errorf("NetProfit: got %v, want %v", gotNetProfit, wantNetProfit)
	}
}

// TestFundingExecutor_StampsStrategy verifies trade.Strategy == "funding".
func TestFundingExecutor_StampsStrategy(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewFundingExecutor(st, clk)

	opp := &types.Opportunity{
		ID:           "opp-funding-3",
		BuyExchange:  "bybit",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(5.0),
		Strategy:     "funding",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	trades := st.AllTrades()
	if len(trades) == 0 {
		t.Fatal("expected persisted trade")
	}
	if trades[0].Strategy != "funding" {
		t.Errorf("Strategy: got %q, want %q", trades[0].Strategy, "funding")
	}
}

// TestFundingExecutor_TradeFields verifies Volume=1.0, PartialFill=false, GrossProfit==NetProfit (spec FE1).
func TestFundingExecutor_TradeFields(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewFundingExecutor(st, clk)

	netProfit := 7.75
	opp := &types.Opportunity{
		ID:           "opp-funding-4",
		BuyExchange:  "bybit",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(netProfit),
		Strategy:     "funding",
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

	vol, _ := tr.Volume.Float64()
	if vol != 1.0 {
		t.Errorf("Volume: got %v, want 1.0", vol)
	}
	if tr.PartialFill {
		t.Error("PartialFill: got true, want false")
	}
	grossProfit, _ := tr.GrossProfit.Float64()
	if grossProfit != netProfit {
		t.Errorf("GrossProfit: got %v, want %v (should equal NetProfit for funding)", grossProfit, netProfit)
	}
}

// TestFundingExecutor_SetsStatusExecuted verifies that Execute sets opp.Status to StatusExecuted.
func TestFundingExecutor_SetsStatusExecuted(t *testing.T) {
	st := store.NewStore("")
	clk := fixedClock{t: time.Now()}
	exec := NewFundingExecutor(st, clk)

	opp := &types.Opportunity{
		ID:           "opp-funding-5",
		BuyExchange:  "bybit",
		SellExchange: "binance",
		NetProfit:    decimal.NewFromFloat(3.0),
		Strategy:     "funding",
		Status:       types.StatusDetected,
	}

	if err := exec.Execute(opp); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if opp.Status != types.StatusExecuted {
		t.Errorf("opp.Status: got %q, want %q", opp.Status, types.StatusExecuted)
	}
}
