package backtest

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/funding"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/triangular"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// StrategyFactory is a function that produces a fresh strategy instance.
// seed is the RNG seed from BacktestSpec; factory implementations MUST use it.
type StrategyFactory func(seed int64) engine.StrategyIface

// NewSpatialFactory returns a factory that creates fresh SpatialStrategy instances
// with an empty SpreadModel map on each call. The live strategy instance is never
// referenced.
func NewSpatialFactory(cfg spatial.Config) StrategyFactory {
	return func(_ int64) engine.StrategyIface {
		return spatial.New(cfg, nil)
	}
}

// NewTriangularFactory returns a factory that creates fresh TriangularStrategy
// instances, seeding the PRNG with the provided seed for determinism.
func NewTriangularFactory(cfg triangular.Config) StrategyFactory {
	return func(seed int64) engine.StrategyIface {
		c := cfg
		c.Seed = seed
		return triangular.New(c)
	}
}

// NewFundingReplayFactory returns a factory that wraps pre-recorded funding rates
// in a thin StrategyIface implementation. Start() is a no-op; no goroutine is
// ever launched. Detect resolves the most-recent rate at-or-before now per exchange.
func NewFundingReplayFactory(cfg funding.Config, rates []recorder.FundingRate) StrategyFactory {
	// Pre-sort and index rates by exchange for O(log n) lookup.
	byExchange := make(map[string][]recorder.FundingRate)
	for _, r := range rates {
		byExchange[r.Exchange] = append(byExchange[r.Exchange], r)
	}
	for ex := range byExchange {
		sort.Slice(byExchange[ex], func(i, j int) bool {
			return byExchange[ex][i].At.Before(byExchange[ex][j].At)
		})
	}

	return func(_ int64) engine.StrategyIface {
		// Copy the map reference — slices are read-only so this is safe.
		return &fundingReplayStrategy{
			cfg:        cfg,
			byExchange: byExchange,
			lastEmit:   make(map[string]time.Time),
		}
	}
}

// fundingReplayStrategy satisfies engine.StrategyIface using pre-recorded rates.
// It mirrors the detection logic of FundingStrategy but reads from the injected
// slice rather than a live polling goroutine.
type fundingReplayStrategy struct {
	mu         sync.Mutex
	cfg        funding.Config
	byExchange map[string][]recorder.FundingRate
	lastEmit   map[string]time.Time
}

func (f *fundingReplayStrategy) Name() string { return "funding" }

// Detect scans all known exchanges for their most-recent rate at-or-before now,
// then emits if the differential between the highest and lowest venues exceeds
// cfg.Threshold (same logic as the live FundingStrategy).
func (f *fundingReplayStrategy) Detect(
	_ types.PriceUpdate,
	_ map[string]types.PriceUpdate,
	now time.Time,
) []types.Opportunity {
	f.mu.Lock()
	defer f.mu.Unlock()

	currentRates := make(map[string]float64)
	for ex, rs := range f.byExchange {
		r := latestAtOrBefore(rs, now)
		if r != nil {
			currentRates[ex] = r.Rate
		}
	}

	if len(currentRates) < 2 {
		return nil
	}

	var maxEx, minEx string
	var maxRate, minRate float64
	first := true
	for ex, r := range currentRates {
		if first {
			maxEx = ex
			minEx = ex
			maxRate = r
			minRate = r
			first = false
			continue
		}
		if r > maxRate {
			maxRate = r
			maxEx = ex
		}
		if r < minRate {
			minRate = r
			minEx = ex
		}
	}

	diff := maxRate - minRate
	if diff < f.cfg.Threshold {
		return nil
	}

	pairKey := minEx + "->" + maxEx
	if last, ok := f.lastEmit[pairKey]; ok {
		if now.Sub(last) < f.cfg.EmitCooldown {
			return nil
		}
	}
	f.lastEmit[pairKey] = now

	netProfit := diff * f.cfg.Notional
	return []types.Opportunity{{
		ID:           uuid.New().String(),
		BuyExchange:  minEx,
		SellExchange: maxEx,
		NetProfit:    decimal.NewFromFloat(netProfit),
		NetProfitPct: decimal.NewFromFloat(diff),
		DetectedAt:   now,
		Status:       types.StatusDetected,
		Strategy:     "funding",
	}}
}

// latestAtOrBefore returns the most-recent FundingRate whose At is <= t,
// or nil if none exists. Expects rs sorted ascending by At.
func latestAtOrBefore(rs []recorder.FundingRate, t time.Time) *recorder.FundingRate {
	idx := sort.Search(len(rs), func(i int) bool {
		return rs[i].At.After(t)
	})
	if idx == 0 {
		return nil
	}
	return &rs[idx-1]
}

// Ensure fundingReplayStrategy does not inadvertently implement strategy.Starter.
// (It must not — Start() must never be called by the runner.)
var _ engine.StrategyIface = (*fundingReplayStrategy)(nil)

// secondsPerYear is the annualization constant used by the Sharpe ratio formula.
// Defined here so metrics.go can reference it without a separate file.
const secondsPerYear = 31_536_000.0
