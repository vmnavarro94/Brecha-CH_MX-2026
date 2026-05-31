package funding

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Config holds all runtime-tunable parameters for FundingStrategy.
type Config struct {
	// Exchanges is the list of exchange names to generate synthetic funding rates for.
	Exchanges []string
	// Threshold is the minimum absolute differential between any two venues to emit an opp.
	Threshold float64
	// PollInterval is how often the side goroutine refreshes the rate cache.
	PollInterval time.Duration
	// EmitCooldown is the minimum time between consecutive emissions for the same venue pair.
	EmitCooldown time.Duration
	// Notional is the position size used to convert differential to dollar profit.
	Notional float64
	// BaseDifferential controls the amplitude of the synthetic rate oscillation.
	// Peak-to-peak range ≈ 2*BaseDifferential.
	BaseDifferential float64
	// Seed initializes the PRNG for deterministic tests.
	Seed int64
}

// FundingStrategy monitors synthetic per-exchange funding rates and emits opportunities
// when the differential between any two venues exceeds Threshold.
//
// It implements strategy.Starter: Start(ctx) must be called before Detect will return results.
// The side goroutine seeds rates at t=0 then polls every PollInterval.
type FundingStrategy struct {
	mu       sync.Mutex
	cfg      Config
	rates    map[string]float64 // exchange → current synthetic funding rate
	lastEmit map[string]time.Time
	// models maps "minEx->maxEx" → SpreadModel trained on the per-tick rate differential
	// so emitted opportunities can carry a meaningful z-score and normalized score.
	models  map[string]*model.SpreadModel
	started bool
	rng     *rand.Rand
	ticks   uint64
}

// New creates a FundingStrategy with the given Config.
func New(cfg Config) *FundingStrategy {
	return &FundingStrategy{
		cfg:      cfg,
		rates:    make(map[string]float64),
		lastEmit: make(map[string]time.Time),
		models:   make(map[string]*model.SpreadModel),
		rng:      rand.New(rand.NewSource(cfg.Seed)),
	}
}

// Name returns the stable strategy identifier.
func (f *FundingStrategy) Name() string { return "funding" }

// Start launches the background polling goroutine. It seeds the rate cache immediately
// at t=0 and then refreshes every PollInterval. The goroutine exits cleanly when ctx is
// cancelled. Calling Start more than once is a no-op (guarded by started flag).
func (f *FundingStrategy) Start(ctx context.Context) error {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return nil
	}
	f.started = true
	f.mu.Unlock()

	go func() {
		// Seed immediately at t=0.
		f.tick()

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(f.cfg.PollInterval):
				f.tick()
			}
		}
	}()

	return nil
}

// tick advances the synthetic wave generator and updates the rates cache.
// Called from the background goroutine and must acquire the mutex.
func (f *FundingStrategy) tick() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ticks++
	for i, ex := range f.cfg.Exchanges {
		wave := math.Sin(float64(f.ticks)/3.0+float64(i)) * f.cfg.BaseDifferential
		jitter := (f.rng.Float64()*2 - 1) * f.cfg.BaseDifferential * 0.5
		f.rates[ex] = wave + jitter
	}
}

// Detect scans the current rate cache and emits an opportunity if the differential
// between the highest- and lowest-rate venues exceeds Threshold and the cooldown has elapsed.
// Returns nil if Start has not been called (empty cache) or if conditions are not met.
func (f *FundingStrategy) Detect(
	_ types.PriceUpdate,
	_ map[string]types.PriceUpdate,
	now time.Time,
) []types.Opportunity {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.rates) == 0 {
		return nil
	}

	// Snapshot rates to find max and min venue.
	var maxEx, minEx string
	var maxRate, minRate float64
	first := true
	for ex, r := range f.rates {
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

	// Ordered pair key so (bybit,binance) and (binance,bybit) don't produce separate cooldowns.
	pairKey := minEx + "->" + maxEx

	// Train the per-pair rate-differential model on every tick — including ones below
	// Threshold — so the distribution reflects the full "normal" range for this pair.
	sm, ok := f.models[pairKey]
	if !ok {
		sm = model.NewSpreadModel()
		f.models[pairKey] = sm
	}
	sm.Update(diff)

	if diff < f.cfg.Threshold {
		return nil
	}

	if last, ok := f.lastEmit[pairKey]; ok {
		if now.Sub(last) < f.cfg.EmitCooldown {
			return nil
		}
	}

	f.lastEmit[pairKey] = now

	netProfit := diff * f.cfg.Notional
	zScore, score := model.ScoreSignal(sm, diff, diff)

	opp := types.Opportunity{
		ID:           uuid.New().String(),
		BuyExchange:  minEx,
		SellExchange: maxEx,
		NetProfit:    decimal.NewFromFloat(netProfit),
		NetProfitPct: decimal.NewFromFloat(diff),
		ZScore:       decimal.NewFromFloat(zScore),
		Score:        decimal.NewFromFloat(score),
		DetectedAt:   now,
		Status:       types.StatusDetected,
		Strategy:     "funding",
	}

	return []types.Opportunity{opp}
}

// InjectRates directly sets the funding rates for testing purposes.
// This allows tests to bypass the Start goroutine and inject known rate values.
func (f *FundingStrategy) InjectRates(rates map[string]float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ex, r := range rates {
		f.rates[ex] = r
	}
}
