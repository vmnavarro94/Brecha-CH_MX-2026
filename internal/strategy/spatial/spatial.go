package spatial

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Config holds all runtime-tunable parameters for SpatialStrategy.
type Config struct {
	Fees               map[string]FeeConfig
	MinNetProfitPct    float64
	MaxPositionUSDT    float64
	StalenessThreshold time.Duration
}

// SpatialStrategy detects cross-exchange spatial arbitrage opportunities.
// All mutable configuration fields are protected by cfgMu so setters and Detect
// can run concurrently without data races.
type SpatialStrategy struct {
	cfgMu  sync.RWMutex
	cfg    Config
	models map[string]*model.SpreadModel
}

// New creates a SpatialStrategy with the given config and spread models.
// models may be nil; if nil or the pair key is absent, fallback scoring is used.
func New(cfg Config, models map[string]*model.SpreadModel) *SpatialStrategy {
	return &SpatialStrategy{
		cfg:    cfg,
		models: models,
	}
}

// Name returns the stable strategy identifier.
func (s *SpatialStrategy) Name() string { return "spatial" }

// Detect examines one incoming price update against the current multi-exchange snapshot
// and returns all cross-exchange arbitrage opportunities found.
// Verbatim move of the engine inner loop with three mechanical changes:
//  1. reads own cfg under cfgMu.RLock instead of engine cfg
//  2. returns []Opportunity instead of heap.Push
//  3. stamps Strategy = "spatial" before append
func (s *SpatialStrategy) Detect(
	update types.PriceUpdate,
	snapshot map[string]types.PriceUpdate,
	now time.Time,
) []types.Opportunity {
	s.cfgMu.RLock()
	cfg := s.cfg
	s.cfgMu.RUnlock()

	var opps []types.Opportunity

	for sellEx, sellPrice := range snapshot {
		if sellEx == update.Exchange {
			continue
		}
		// Skip stale counterparty.
		age := now.Sub(sellPrice.ReceivedAt)
		if age > cfg.StalenessThreshold {
			continue
		}

		buyAsk, _ := update.Ask.Float64()
		sellBid, _ := sellPrice.Bid.Float64()

		// Always update the spread model for this pair with the raw cross-exchange
		// spread (before fees). This gives the model a true distribution of price
		// differences, including negative ones when the pair is not arbitrageable.
		if buyAsk > 0 {
			if sm, ok := s.models[pairKey(update.Exchange, sellEx)]; ok {
				sm.Update((sellBid - buyAsk) / buyAsk)
			}
		}

		gross := sellBid - buyAsk
		if gross <= 0 {
			continue
		}

		buyFee := feeFor(cfg.Fees, update.Exchange)
		sellFee := feeFor(cfg.Fees, sellEx)

		costBuyFee := buyAsk * buyFee.TakerFee
		costSellFee := sellBid * sellFee.TakerFee
		costSlippage := buyAsk * buyFee.SlippageFactor
		// Withdrawal cost: BTC must move from buyEx back to sellEx to repeat the cycle.
		// Modeled as the buyEx withdrawal fee (in BTC) priced at the buy price.
		costWithdrawal := buyFee.WithdrawalBTC * buyAsk
		// Network-latency cost: per-leg basis-points hit modelling the implicit
		// slippage from price drift during the WS network round-trip.
		// The sell-side component is scaled by ageFactor: 1 + ageMs/1000, capped at 3×.
		// Buy-side stays 1× (buy update is the trigger; age ≈ 0 by definition).
		sellAgeMs := float64(now.Sub(sellPrice.ReceivedAt).Milliseconds())
		ageFactor := 1.0 + sellAgeMs/1000.0
		if ageFactor > 3.0 {
			ageFactor = 3.0
		}
		costNetLatency := buyAsk*buyFee.NetworkLatencyBps/10000.0 +
			sellBid*sellFee.NetworkLatencyBps/10000.0*ageFactor
		netProfit := gross - costBuyFee - costSellFee - costSlippage - costWithdrawal - costNetLatency

		if netProfit <= 0 {
			continue
		}

		netPct := netProfit / buyAsk
		if netPct < cfg.MinNetProfitPct {
			continue
		}

		zScore, score := s.computeScore(update.Exchange, sellEx, netPct)

		opp := types.Opportunity{
			ID:           uuid.New().String(),
			BuyExchange:  update.Exchange,
			SellExchange: sellEx,
			BuyPrice:     update.Ask,
			SellPrice:    sellPrice.Bid,
			NetProfit:    decimal.NewFromFloat(netProfit),
			NetProfitPct: decimal.NewFromFloat(netPct),
			ZScore:       decimal.NewFromFloat(zScore),
			Score:        decimal.NewFromFloat(score),
			MaxVolume:    decimal.NewFromFloat(cfg.MaxPositionUSDT / buyAsk),
			DetectedAt:   now,
			Status:       types.StatusDetected,
			Strategy:     "spatial",
		}

		opps = append(opps, opp)
	}

	return opps
}

// SpreadStats returns a copy of the current spread statistics for all tracked pairs.
// Safe for concurrent use: acquires RLock before reading models.
func (s *SpatialStrategy) SpreadStats() map[string]model.SpreadStats {
	out := make(map[string]model.SpreadStats, len(s.models))
	for k, m := range s.models {
		out[k] = model.SpreadStats{
			Pair:    k,
			Mean:    m.Mean(),
			Std:     m.Std(),
			Samples: m.N(),
		}
	}
	return out
}

// SetMinNetProfitPct updates the minimum net profit threshold.
func (s *SpatialStrategy) SetMinNetProfitPct(v float64) {
	s.cfgMu.Lock()
	s.cfg.MinNetProfitPct = v
	s.cfgMu.Unlock()
}

// SetMaxPositionUSDT updates the maximum position size.
func (s *SpatialStrategy) SetMaxPositionUSDT(v float64) {
	s.cfgMu.Lock()
	s.cfg.MaxPositionUSDT = v
	s.cfgMu.Unlock()
}

// SetStaleness updates the price staleness cutoff.
func (s *SpatialStrategy) SetStaleness(d time.Duration) {
	s.cfgMu.Lock()
	s.cfg.StalenessThreshold = d
	s.cfgMu.Unlock()
}

// SetFees replaces the fee table atomically. The provided map must be a new allocation.
func (s *SpatialStrategy) SetFees(fees map[string]FeeConfig) {
	s.cfgMu.Lock()
	s.cfg.Fees = fees
	s.cfgMu.Unlock()
}

// computeScore returns (zScore, score).
// When the spread model for the pair is ready: score = net_pct*0.6 + sigmoid(z)*0.4
// Fallback (model absent or not ready): score = net_pct, zScore = 0
func (s *SpatialStrategy) computeScore(buyEx, sellEx string, netPct float64) (float64, float64) {
	if s.models == nil {
		return 0, netPct
	}
	key := pairKey(buyEx, sellEx)
	sm, ok := s.models[key]
	if !ok || !sm.IsReady() {
		return 0, netPct
	}
	z := sm.ZScore(netPct)
	score := netPct*0.6 + sigmoid(z)*0.4
	return z, score
}

// pairKey returns the canonical key for a buy/sell exchange pair.
func pairKey(buyEx, sellEx string) string {
	return fmt.Sprintf("%s-%s", buyEx, sellEx)
}

// sigmoid maps x to (0, 1). Used to normalize z-score contribution in scoring.
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
