package executor

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// FundingExecutor records synthetic funding-rate trades without touching wallets.
// This executor is used for opp.Strategy == "funding" paths. It records a Trade with:
//   - Volume = 1.0 (notional normalized; real sizing is handled by FundingStrategy.Config.Notional)
//   - PartialFill = false
//   - Fees = 0 (funding arbitrage has no direct taker-fee per Design ADR-4)
//   - GrossProfit = NetProfit = opp.NetProfit
type FundingExecutor struct {
	store *store.Store
	clock types.Clock
}

// NewFundingExecutor creates a FundingExecutor backed by the given store and clock.
func NewFundingExecutor(st *store.Store, clk types.Clock) *FundingExecutor {
	return &FundingExecutor{store: st, clock: clk}
}

// Execute records a Trade for the given funding opportunity and marks it as executed.
// No wallet.Debit or wallet.Credit calls are made (per ADR-3 / spec FE1).
func (fe *FundingExecutor) Execute(opp *types.Opportunity) error {
	now := fe.clock.Now()

	trade := types.Trade{
		ID:          uuid.New().String(),
		OpportunityID: opp.ID,
		BuyExchange:  opp.BuyExchange,
		SellExchange: opp.SellExchange,
		BuyPrice:     decimal.Zero,
		SellPrice:    decimal.Zero,
		Volume:       decimal.NewFromFloat(1.0),
		GrossProfit:  opp.NetProfit,
		Fees:         decimal.Zero,
		NetProfit:    opp.NetProfit,
		Slippage:     decimal.Zero,
		ExecutedAt:   now,
		RequestedVolume: decimal.NewFromFloat(1.0),
		PartialFill:  false,
		Strategy:     "funding",
	}

	fe.store.SaveTrade(trade)

	opp.Status = types.StatusExecuted
	// Persist the updated opportunity status.
	fe.store.Save(*opp)

	return nil
}
