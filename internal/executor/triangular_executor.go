package executor

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// TriangularExecutor records synthetic triangular-arbitrage trades without touching wallets.
// This executor is used for opp.Strategy == "triangular" paths.
//
// Fee model (per Design ADR-1):
//   fees        = 3 * takerFee * notional   (three legs, each leg costs takerFee * notional USDT)
//   grossProfit = opp.NetProfit + fees       (cycle gain before fees)
//   netProfit   = opp.NetProfit              (already net-of-fees per TriangularStrategy)
//   volume      = notional                  (USDT notional for the cycle)
type TriangularExecutor struct {
	store    *store.Store
	clock    types.Clock
	takerFee float64
	notional float64
}

// NewTriangularExecutor creates a TriangularExecutor with the given store, clock, fee, and notional.
func NewTriangularExecutor(st *store.Store, clk types.Clock, takerFee, notional float64) *TriangularExecutor {
	return &TriangularExecutor{
		store:    st,
		clock:    clk,
		takerFee: takerFee,
		notional: notional,
	}
}

// Execute records a Trade for the given triangular opportunity and marks it as executed.
// No wallet.Debit or wallet.Credit calls are made (intra-exchange round-trip, no net wallet change).
func (te *TriangularExecutor) Execute(opp *types.Opportunity) error {
	now := te.clock.Now()

	fees := 3 * te.takerFee * te.notional
	grossProfit, _ := opp.NetProfit.Float64()
	grossProfit += fees

	trade := types.Trade{
		ID:              uuid.New().String(),
		OpportunityID:   opp.ID,
		BuyExchange:     opp.BuyExchange,
		SellExchange:    opp.SellExchange,
		BuyPrice:        decimal.Zero,
		SellPrice:       decimal.Zero,
		Volume:          decimal.NewFromFloat(te.notional),
		GrossProfit:     decimal.NewFromFloat(grossProfit),
		Fees:            decimal.NewFromFloat(fees),
		NetProfit:       opp.NetProfit,
		Slippage:        decimal.Zero,
		ExecutedAt:      now,
		RequestedVolume: decimal.NewFromFloat(te.notional),
		PartialFill:     false,
		Strategy:        "triangular",
	}

	te.store.SaveTrade(trade)

	opp.Status = types.StatusExecuted
	te.store.Save(*opp)

	return nil
}
