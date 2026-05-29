package executor

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
)

// Sentinel errors returned by Execute.
var (
	// ErrStalePrice is returned when the current market price for either exchange is too old.
	ErrStalePrice = errors.New("stale price")
	// ErrInsufficientBalance is returned when the available USDT balance is zero,
	// resulting in a computable volume of zero.
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// Executor simulates trade execution: validates freshness, computes volume, updates wallets,
// and persists the trade record.
type Executor struct {
	wallet             *wallet.MultiWallet
	store              *store.Store
	snapshotFn         func() map[string]types.PriceUpdate
	clock              types.Clock
	stalenessThreshold time.Duration
}

// NewExecutor creates a new Executor.
func NewExecutor(
	w *wallet.MultiWallet,
	st *store.Store,
	snapshotFn func() map[string]types.PriceUpdate,
	clock types.Clock,
	stalenessThreshold time.Duration,
) *Executor {
	return &Executor{
		wallet:             w,
		store:              st,
		snapshotFn:         snapshotFn,
		clock:              clock,
		stalenessThreshold: stalenessThreshold,
	}
}

// Execute attempts to execute an arbitrage opportunity.
//
// It:
//  1. Verifies both exchange prices are fresh (within stalenessThreshold).
//  2. Computes volume = min(opp.MaxVolume, walletBalance/currentAsk).
//  3. Debits USDT and credits BTC on the buy exchange.
//  4. Debits BTC and credits USDT on the sell exchange.
//  5. Persists a Trade record and marks the opportunity Executed.
//
// Returns ErrStalePrice when either price is stale (status → Expired).
// Returns ErrInsufficientBalance when computed volume is zero (status → Skipped).
func (e *Executor) Execute(opp *types.Opportunity) error {
	snapshot := e.snapshotFn()
	now := e.clock.Now()

	// Verify buy-side freshness.
	buyPrice, ok := snapshot[opp.BuyExchange]
	if !ok || now.Sub(buyPrice.ReceivedAt) > e.stalenessThreshold {
		opp.Status = types.StatusExpired
		e.store.Save(*opp)
		return ErrStalePrice
	}

	// Verify sell-side freshness.
	sellPrice, ok := snapshot[opp.SellExchange]
	if !ok || now.Sub(sellPrice.ReceivedAt) > e.stalenessThreshold {
		opp.Status = types.StatusExpired
		e.store.Save(*opp)
		return ErrStalePrice
	}

	currentAsk, _ := buyPrice.Ask.Float64()
	currentBid, _ := sellPrice.Bid.Float64()

	// Compute volume.
	usdtBalance := e.wallet.Balance(opp.BuyExchange, "USDT")
	maxVolF, _ := opp.MaxVolume.Float64()

	maxByBalance := usdtBalance / currentAsk
	volume := math.Min(maxVolF, maxByBalance)

	if volume <= 0 {
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}

	// Execute buy side: debit USDT, credit BTC.
	cost := volume * currentAsk
	if err := e.wallet.Debit(opp.BuyExchange, "USDT", cost); err != nil {
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}
	e.wallet.Credit(opp.BuyExchange, "BTC", volume)

	// Execute sell side: debit BTC, credit USDT.
	if err := e.wallet.Debit(opp.SellExchange, "BTC", volume); err != nil {
		// Reverse the buy side.
		e.wallet.Debit(opp.BuyExchange, "BTC", volume)
		e.wallet.Credit(opp.BuyExchange, "USDT", cost)
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}
	proceeds := volume * currentBid
	e.wallet.Credit(opp.SellExchange, "USDT", proceeds)

	// Compute profit.
	grossProfit := (currentBid - currentAsk) * volume
	// Fees are tracked externally by the engine; executor records gross as net for simulation.
	netProfit := grossProfit

	// Persist trade record.
	trade := types.Trade{
		ID:            uuid.New().String(),
		OpportunityID: opp.ID,
		BuyExchange:   opp.BuyExchange,
		SellExchange:  opp.SellExchange,
		BuyPrice:      decimal.NewFromFloat(currentAsk),
		SellPrice:     decimal.NewFromFloat(currentBid),
		Volume:        decimal.NewFromFloat(volume),
		GrossProfit:   decimal.NewFromFloat(grossProfit),
		Fees:          decimal.Zero,
		NetProfit:     decimal.NewFromFloat(netProfit),
		Slippage:      decimal.Zero,
		ExecutedAt:    now,
	}
	e.store.SaveTrade(trade)

	// Mark opportunity executed.
	opp.Status = types.StatusExecuted
	e.store.Save(*opp)

	return nil
}
