package spatial

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/sizing"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Config holds all runtime-tunable parameters for SpatialStrategy.
type Config struct {
	Fees                   map[string]FeeConfig
	MinNetProfitPct        float64
	MaxPositionUSDT        float64
	StalenessThreshold     time.Duration
	ImbalancePenaltyWeight float64 // default 0.15; set to 0 to disable

	// Adaptive threshold: adaptiveMin = BaseMinNetProfitPct + AdaptiveCoeff * spreadModel.Std()
	// When BaseMinNetProfitPct is zero and MinNetProfitPct is non-zero, constructor copies
	// MinNetProfitPct into BaseMinNetProfitPct for backward compatibility.
	BaseMinNetProfitPct float64
	AdaptiveCoeff       float64

	// Kelly sizing: per-pair estimator parameters.
	KellyMinSamples int
	KellyFraction   float64

	// Correlation penalty: recent-trades ring buffer parameters.
	CorrPenaltyWeight float64
	CorrWindowN       int
}

// SpatialStrategy detects cross-exchange spatial arbitrage opportunities.
// All mutable configuration fields are protected by cfgMu so setters and Detect
// can run concurrently without data races.
type SpatialStrategy struct {
	cfgMu  sync.RWMutex
	cfg    Config
	models map[string]*model.SpreadModel

	// Kelly sizing: per-pair estimators (key = "buyEx-sellEx").
	muKelly        sync.RWMutex
	kellyEstimators map[string]*sizing.KellyEstimator

	// Correlation penalty: ring buffer of recent trades.
	recentTrades *recentTradeRing
}

// New creates a SpatialStrategy with the given config and spread models.
// models may be nil; if nil or the pair key is absent, fallback scoring is used.
// If cfg.ImbalancePenaltyWeight is zero (unset), it defaults to 0.15.
// If cfg.BaseMinNetProfitPct is zero but cfg.MinNetProfitPct is non-zero,
// BaseMinNetProfitPct is set from MinNetProfitPct (backward compatibility).
// If cfg.KellyMinSamples is zero, it defaults to 10.
// If cfg.KellyFraction is zero, it defaults to 0.25.
// If cfg.CorrWindowN is zero, it defaults to 50.
func New(cfg Config, models map[string]*model.SpreadModel) *SpatialStrategy {
	if cfg.ImbalancePenaltyWeight == 0 {
		cfg.ImbalancePenaltyWeight = 0.15
	}
	// Backward compat: copy MinNetProfitPct into BaseMinNetProfitPct when only the old field is set.
	if cfg.BaseMinNetProfitPct == 0 && cfg.MinNetProfitPct > 0 {
		cfg.BaseMinNetProfitPct = cfg.MinNetProfitPct
	}
	if cfg.KellyMinSamples == 0 {
		cfg.KellyMinSamples = 10
	}
	if cfg.KellyFraction == 0 {
		cfg.KellyFraction = 0.25
	}
	corrWindowN := cfg.CorrWindowN
	if corrWindowN == 0 {
		corrWindowN = 50
	}
	return &SpatialStrategy{
		cfg:             cfg,
		models:          models,
		kellyEstimators: make(map[string]*sizing.KellyEstimator),
		recentTrades:    newRing(corrWindowN),
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

		// Adaptive threshold: adaptiveMin = BaseMinNetProfitPct + AdaptiveCoeff * spreadModel.Std()
		var spreadStd float64
		if sm, ok := s.models[pairKey(update.Exchange, sellEx)]; ok {
			spreadStd = sm.Std()
		}
		adaptiveMin := cfg.BaseMinNetProfitPct + cfg.AdaptiveCoeff*spreadStd
		if netPct < adaptiveMin {
			continue
		}

		// Kelly sizing: use per-pair estimator fraction when available and warmed up.
		buyEx := update.Exchange
		effectiveFraction := 1.0
		s.muKelly.RLock()
		if est, ok := s.kellyEstimators[pairKey(buyEx, sellEx)]; ok {
			if f := est.Fraction(); f > 0 {
				effectiveFraction = f
			}
		}
		s.muKelly.RUnlock()
		baseVolume := effectiveFraction * cfg.MaxPositionUSDT / buyAsk

		// Correlation penalty: reduce volume for recently-traded exchange pairs.
		matches := s.recentTrades.CountSameExchange(buyEx, sellEx)
		corrWindowN := float64(cfg.CorrWindowN)
		if corrWindowN == 0 {
			corrWindowN = 50
		}
		penalty := 1.0 - cfg.CorrPenaltyWeight*float64(matches)/corrWindowN
		if penalty < 0.1 {
			penalty = 0.1
		}
		maxVolume := baseVolume * penalty

		zScore, score := s.computeScore(update.Exchange, sellEx, netPct)

		// Imbalance penalty: penalises ask-heavy sell-side books (harder to sell).
		// penalty = weight * max(0, -imbalance) where imbalance = (bid-ask)/(bid+ask)
		// Nil-book guard: if both sizes are zero, penalty = 0.
		sellBidF, _ := sellPrice.BidSize.Float64()
		sellAskF, _ := sellPrice.AskSize.Float64()
		if sellBidF+sellAskF > 0 {
			imbalance := (sellBidF - sellAskF) / (sellBidF + sellAskF)
			if imbalance < 0 {
				score -= cfg.ImbalancePenaltyWeight * (-imbalance)
			}
		}

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
			MaxVolume:    decimal.NewFromFloat(maxVolume),
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
// Also updates BaseMinNetProfitPct for backward compatibility.
func (s *SpatialStrategy) SetMinNetProfitPct(v float64) {
	s.cfgMu.Lock()
	s.cfg.MinNetProfitPct = v
	s.cfg.BaseMinNetProfitPct = v
	s.cfgMu.Unlock()
}

// RecordTradeReturn updates the per-pair Kelly estimator and the recent-trades ring buffer
// for the given exchange pair with the realized net return fraction.
// Concurrency-safe: write-locks the Kelly map for create-if-missing, then releases before
// calling estimator.Record (which has its own lock).
func (s *SpatialStrategy) RecordTradeReturn(buyEx, sellEx string, netPct float64) {
	key := pairKey(buyEx, sellEx)

	// Create estimator if absent (write lock for map mutation).
	s.muKelly.Lock()
	est, ok := s.kellyEstimators[key]
	if !ok {
		s.cfgMu.RLock()
		minN := s.cfg.KellyMinSamples
		fracCap := s.cfg.KellyFraction
		s.cfgMu.RUnlock()
		est = sizing.New(minN, fracCap)
		s.kellyEstimators[key] = est
	}
	s.muKelly.Unlock()

	// Record the return (estimator has its own RWMutex).
	est.Record(netPct)

	// Append to the ring buffer.
	s.recentTrades.Push(recentTradeEntry{BuyEx: buyEx, SellEx: sellEx})
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

// SetImbalancePenaltyWeight updates the imbalance penalty weight under the config lock.
// Set to 0.0 to effectively disable the penalty.
func (s *SpatialStrategy) SetImbalancePenaltyWeight(v float64) {
	s.cfgMu.Lock()
	s.cfg.ImbalancePenaltyWeight = v
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
