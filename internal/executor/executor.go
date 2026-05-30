package executor

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/depth"
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

// Executor simulates trade execution: validates freshness, walks the synthetic order book,
// computes VWAP, updates wallets, and persists the trade record.
type Executor struct {
	wallet             *wallet.MultiWallet
	store              *store.Store
	snapshotFn         func() map[string]types.PriceUpdate
	clock              types.Clock
	stalenessThreshold time.Duration
	depthCfg           depth.Config
}

// NewExecutor creates a new Executor with depth.Config for synthetic book generation.
func NewExecutor(
	w *wallet.MultiWallet,
	st *store.Store,
	snapshotFn func() map[string]types.PriceUpdate,
	clock types.Clock,
	stalenessThreshold time.Duration,
	depthCfg depth.Config,
) *Executor {
	return &Executor{
		wallet:             w,
		store:              st,
		snapshotFn:         snapshotFn,
		clock:              clock,
		stalenessThreshold: stalenessThreshold,
		depthCfg:           depthCfg,
	}
}

// Execute attempts to execute an arbitrage opportunity.
//
// It:
//  1. Verifies both exchange prices are fresh (within stalenessThreshold).
//  2. Computes volume = min(opp.MaxVolume, walletBalance/currentAsk).
//  3. Walks the synthetic ask book for the buy leg (VWAP-priced).
//  4. Debits USDT and credits BTC on the buy exchange.
//  5. Walks the synthetic bid book for the sell leg (VWAP-priced).
//  6. If sell fill < buy fill → atomically reverses the buy using exact VWAP cost.
//  7. Persists a Trade record and marks the opportunity Executed.
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

	bboAsk := buyPrice.Ask
	bboBid := sellPrice.Bid
	currentAskF, _ := bboAsk.Float64()

	// Compute volume.
	usdtBalance := e.wallet.Balance(opp.BuyExchange, "USDT")
	maxVolF, _ := opp.MaxVolume.Float64()

	maxByBalance := usdtBalance / currentAskF
	volumeF := math.Min(maxVolF, maxByBalance)

	if volumeF <= 0 {
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}

	targetVolume := decimal.NewFromFloat(volumeF)

	// --- Buy leg: walk the ask book ---
	askLevels := depth.AskLevels(bboAsk, e.depthCfg)
	buyFilled, vwapBuy, buyPartial := depth.Walk(askLevels, targetVolume)

	if buyFilled.IsZero() {
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}

	buyFilledF, _ := buyFilled.Float64()
	vwapBuyF, _ := vwapBuy.Float64()
	costBuy := buyFilled.Mul(vwapBuy)
	costBuyF, _ := costBuy.Float64()

	// Debit USDT, credit BTC on buy exchange.
	if err := e.wallet.Debit(opp.BuyExchange, "USDT", costBuyF); err != nil {
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}
	e.wallet.Credit(opp.BuyExchange, "BTC", buyFilledF)

	// --- Sell leg: walk the bid book ---
	bidLevels := depth.BidLevels(bboBid, e.depthCfg)
	sellFilled, vwapSell, _ := depth.Walk(bidLevels, buyFilled)
	sellFilledF, _ := sellFilled.Float64()

	// Atomic-or-nothing: if sell side can't fill what buy side filled → reverse.
	if sellFilled.LessThan(buyFilled) {
		// Reverse the buy side using VWAP cost (not BBO * volume).
		e.wallet.Debit(opp.BuyExchange, "BTC", buyFilledF)
		e.wallet.Credit(opp.BuyExchange, "USDT", costBuyF)
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}

	// Debit BTC, credit USDT on sell exchange.
	if err := e.wallet.Debit(opp.SellExchange, "BTC", sellFilledF); err != nil {
		// Reverse the buy side using VWAP cost.
		e.wallet.Debit(opp.BuyExchange, "BTC", buyFilledF)
		e.wallet.Credit(opp.BuyExchange, "USDT", costBuyF)
		opp.Status = types.StatusSkipped
		e.store.Save(*opp)
		return ErrInsufficientBalance
	}
	vwapSellF, _ := vwapSell.Float64()
	proceeds := sellFilledF * vwapSellF
	e.wallet.Credit(opp.SellExchange, "USDT", proceeds)

	// --- Compute profit and slippage ---
	grossProfit := (vwapSellF - vwapBuyF) * sellFilledF
	netProfit := grossProfit

	bboAskF, _ := bboAsk.Float64()
	bboBidF, _ := bboBid.Float64()
	slippage := (vwapBuyF - bboAskF) + (bboBidF - vwapSellF)

	// Partial fill detection: use the original requested volume.
	partialFill := buyPartial
	requestedVolume := targetVolume

	// Persist trade record.
	trade := types.Trade{
		ID:              uuid.New().String(),
		OpportunityID:   opp.ID,
		BuyExchange:     opp.BuyExchange,
		SellExchange:    opp.SellExchange,
		BuyPrice:        vwapBuy,
		SellPrice:       vwapSell,
		Volume:          sellFilled,
		GrossProfit:     decimal.NewFromFloat(grossProfit),
		Fees:            decimal.Zero,
		NetProfit:       decimal.NewFromFloat(netProfit),
		Slippage:        decimal.NewFromFloat(slippage),
		ExecutedAt:      now,
		RequestedVolume: requestedVolume,
		PartialFill:     partialFill,
	}
	e.store.SaveTrade(trade)

	// Mark opportunity executed.
	opp.Status = types.StatusExecuted
	e.store.Save(*opp)

	return nil
}
