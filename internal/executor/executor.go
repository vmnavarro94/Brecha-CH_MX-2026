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

// TradeReturnReporter is called by Execute after a successful spatial trade to record
// the realized net return fraction back into the strategy's Kelly estimator.
// SpatialStrategy satisfies this interface structurally (no cyclic import needed).
type TradeReturnReporter interface {
	RecordTradeReturn(buyEx, sellEx string, netPct float64)
}

// bookSource provides L2 order book data keyed by exchange name.
// Aggregator implements this interface; fakes are used in tests.
type bookSource interface {
	Book(exchange string) types.OrderBook
}

// noopBookSource is the default when no real L2 source is configured.
// It always returns an empty book, which causes the executor to use synthetic depth.
type noopBookSource struct{}

func (noopBookSource) Book(_ string) types.OrderBook { return types.OrderBook{} }

// Executor simulates trade execution: validates freshness, walks the synthetic order book,
// computes VWAP, updates wallets, and persists the trade record.
type Executor struct {
	wallet               *wallet.MultiWallet
	store                *store.Store
	snapshotFn           func() map[string]types.PriceUpdate
	clock                types.Clock
	stalenessThreshold   time.Duration
	depthCfg             depth.Config
	bookSource           bookSource
	tradeReturnReporter  TradeReturnReporter
}

// NewExecutor creates a new Executor with depth.Config for synthetic book generation.
// Uses a no-op book source (always falls back to synthetic depth).
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
		bookSource:         noopBookSource{},
	}
}

// NewExecutorWithBookSource creates an Executor that uses real L2 depth when available,
// falling back to synthetic depth when the book for an exchange is empty.
func NewExecutorWithBookSource(
	w *wallet.MultiWallet,
	st *store.Store,
	snapshotFn func() map[string]types.PriceUpdate,
	clock types.Clock,
	stalenessThreshold time.Duration,
	depthCfg depth.Config,
	bs bookSource,
) *Executor {
	return &Executor{
		wallet:             w,
		store:              st,
		snapshotFn:         snapshotFn,
		clock:              clock,
		stalenessThreshold: stalenessThreshold,
		depthCfg:           depthCfg,
		bookSource:         bs,
	}
}

// WithTradeReturnReporter sets the reporter that will be called after each successful
// spatial trade. Returns the executor for method chaining.
func (e *Executor) WithTradeReturnReporter(r TradeReturnReporter) *Executor {
	e.tradeReturnReporter = r
	return e
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
	askLevels := e.getAskLevels(opp.BuyExchange, bboAsk)
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
	bidLevels := e.getBidLevels(opp.SellExchange, bboBid)
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
		Strategy:        opp.Strategy,
	}
	e.store.SaveTrade(trade)

	// Post-trade callback: report realized net pct back to the strategy for Kelly sizing.
	// Only triggered for spatial opportunities (ADR-8, ADR-9).
	if opp.Strategy == "spatial" && e.tradeReturnReporter != nil {
		actualNetPct := (vwapSellF - vwapBuyF) / vwapBuyF
		e.tradeReturnReporter.RecordTradeReturn(opp.BuyExchange, opp.SellExchange, actualNetPct)
	}

	// Mark opportunity executed.
	opp.Status = types.StatusExecuted
	e.store.Save(*opp)

	return nil
}

// getAskLevels returns real L2 ask levels from bookSource when available,
// falling back to synthetic depth when the book is empty. Spec: D3.
func (e *Executor) getAskLevels(exchange string, bboAsk decimal.Decimal) []depth.Level {
	ob := e.bookSource.Book(exchange)
	if len(ob.Asks) > 0 {
		return toDepthLevels(ob.Asks)
	}
	return depth.AskLevels(bboAsk, e.depthCfg)
}

// getBidLevels returns real L2 bid levels from bookSource when available,
// falling back to synthetic depth when the book is empty. Spec: D3.
func (e *Executor) getBidLevels(exchange string, bboBid decimal.Decimal) []depth.Level {
	ob := e.bookSource.Book(exchange)
	if len(ob.Bids) > 0 {
		return toDepthLevels(ob.Bids)
	}
	return depth.BidLevels(bboBid, e.depthCfg)
}

// toDepthLevels converts []types.OrderBookLevel to []depth.Level for VWAP walking.
func toDepthLevels(levels []types.OrderBookLevel) []depth.Level {
	out := make([]depth.Level, len(levels))
	for i, l := range levels {
		out[i] = depth.Level{Price: l.Price, Qty: l.Qty}
	}
	return out
}
