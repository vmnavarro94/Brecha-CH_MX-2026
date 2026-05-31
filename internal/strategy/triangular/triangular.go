package triangular

import (
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Config holds all runtime-tunable parameters for TriangularStrategy.
// The 3-node graph is {USDT, BTC, ETH}; strategy detects negative cycles using
// an explicit 2-cycle ratio check (equivalent to Bellman-Ford on the log-weight graph).
type Config struct {
	// Exchanges is the list of exchange names to run detection against.
	Exchanges []string
	// TakerFee is the per-leg taker fee fraction (e.g. 0.001 = 0.1%).
	// 3 legs per cycle → cost = 3*TakerFee.
	TakerFee float64
	// NoiseRange controls per-exchange ETH/USDT seed offset: ±NoiseRange (e.g. 0.001 = 0.1%).
	NoiseRange float64
	// SeedRefPrice is the base ETH/USDT reference price used at seeding time.
	SeedRefPrice float64
	// RefreshEvery defines how often the ETH/USDT reference drifts (lazy refresh in Detect).
	RefreshEvery time.Duration
	// Notional is the USDT trade size used to convert cycleGain to dollar profit.
	Notional float64
	// MinNetProfit is the minimum profit (in USDT) below which no opportunity is emitted.
	MinNetProfit float64
	// EmitCooldown is the minimum time between consecutive emissions for the same exchange.
	EmitCooldown time.Duration
	// Seed is used to initialize the per-strategy PRNG for deterministic tests.
	Seed int64
}

// TriangularStrategy detects triangular arbitrage opportunities on a single exchange.
// It checks both directed 3-cycles of the USDT/BTC/ETH graph and emits if the
// net gain after 3*TakerFee exceeds MinNetProfit.
//
// THREE INDEPENDENT PRICES are required for triangular arb. The BTC/USDT pair
// comes from the live WS feed; ETH/USDT and BTC/ETH are seeded per exchange
// with independent noise. Without an independent BTC/ETH market price, the
// cycle math collapses to BTC bid-ask spread (which is not triangular arb).
//
// All mutable fields are protected by mu for concurrent safety.
type TriangularStrategy struct {
	mu  sync.RWMutex
	cfg Config
	// refs maps exchange → seeded ETH/USDT reference price.
	refs map[string]float64
	// ethBtc maps exchange → seeded BTC/ETH market cross rate (price of 1 ETH in BTC).
	// This is independent of ethRef/btcMid (the implied cross). Divergence between
	// market and implied is the arbitrage signal.
	ethBtc map[string]float64
	// last maps exchange → last emission time (for cooldown guard).
	last map[string]time.Time
	// lastRefresh maps exchange → last time refs[exchange] was refreshed.
	lastRefresh map[string]time.Time
	// models maps exchange → SpreadModel trained on the per-tick cycleGain so the
	// detector can attach a z-score and a normalized score to each opportunity.
	models map[string]*model.SpreadModel
	rng    *rand.Rand
}

// New creates a TriangularStrategy with the given Config.
func New(cfg Config) *TriangularStrategy {
	return &TriangularStrategy{
		cfg:         cfg,
		refs:        make(map[string]float64),
		ethBtc:      make(map[string]float64),
		last:        make(map[string]time.Time),
		lastRefresh: make(map[string]time.Time),
		models:      make(map[string]*model.SpreadModel),
		rng:         rand.New(rand.NewSource(cfg.Seed)),
	}
}

// SeedRefs forces both the ETH/USDT reference and the BTC/ETH market cross rate
// for an exchange. Test helper for deterministic math; production uses lazy
// initialization with PRNG noise on first Detect call.
func (t *TriangularStrategy) SeedRefs(exchange string, ethRef, ethBtcMarket float64) {
	t.mu.Lock()
	t.refs[exchange] = ethRef
	t.ethBtc[exchange] = ethBtcMarket
	t.mu.Unlock()
}

// Name returns the stable strategy identifier.
func (t *TriangularStrategy) Name() string { return "triangular" }

// Detect examines an incoming price update and returns triangular arbitrage opportunities.
// Only BTC/USDT updates trigger the cycle check (the BTC/USDT rate anchors the graph).
// Algorithm (explicit 2-cycle ratio check, equivalent to Bellman-Ford on the log-weight graph):
//
//	ratioA (USDT→BTC→ETH→USDT) ≈ btcMid / ethRef  (simplified for 3-leg cycle)
//	ratioB (USDT→ETH→BTC→USDT) ≈ ethRef / btcMid
//	cycleGain = max(ratioA, ratioB) - 1
//	netProfit = (cycleGain - 3*TakerFee) * Notional
func (t *TriangularStrategy) Detect(
	update types.PriceUpdate,
	_ map[string]types.PriceUpdate,
	now time.Time,
) []types.Opportunity {
	t.mu.Lock()
	defer t.mu.Unlock()

	exchange := update.Exchange

	// Seed ETH/USDT reference lazily on first call per exchange.
	if _, ok := t.refs[exchange]; !ok {
		noise := (t.rng.Float64()*2 - 1) * t.cfg.NoiseRange
		t.refs[exchange] = t.cfg.SeedRefPrice * (1 + noise)
		t.lastRefresh[exchange] = now
	}

	// Lazy refresh: apply a random walk to the ETH reference when RefreshEvery has elapsed.
	if t.cfg.RefreshEvery > 0 {
		if now.Sub(t.lastRefresh[exchange]) >= t.cfg.RefreshEvery {
			drift := (t.rng.Float64()*2 - 1) * t.cfg.NoiseRange
			t.refs[exchange] *= (1 + drift)
			t.lastRefresh[exchange] = now
		}
	}

	askF, _ := update.Ask.Float64()
	bidF, _ := update.Bid.Float64()
	if askF <= 0 || bidF <= 0 {
		return nil
	}

	btcMid := (askF + bidF) / 2.0
	ethRef := t.refs[exchange]

	// Seed ethBtcMarket lazily — independent of refs[exchange]. Derived from current
	// btcMid + own PRNG noise. Without this independent observation the cycle math
	// collapses to BTC bid-ask spread, which is not triangular arbitrage.
	if _, ok := t.ethBtc[exchange]; !ok {
		implied := ethRef / btcMid
		crossNoise := (t.rng.Float64()*2 - 1) * t.cfg.NoiseRange
		t.ethBtc[exchange] = implied * (1 + crossNoise)
	}
	// Lazy refresh: ethBtcMarket drifts on the same schedule as refs.
	if t.cfg.RefreshEvery > 0 && now.Sub(t.lastRefresh[exchange]) >= t.cfg.RefreshEvery {
		drift := (t.rng.Float64()*2 - 1) * t.cfg.NoiseRange
		t.ethBtc[exchange] *= (1 + drift)
	}
	ethBtcMarket := t.ethBtc[exchange]

	// Triangular arb requires 3 INDEPENDENT prices:
	//   BTC/USDT  from WS feed (ask, bid, btcMid)
	//   ETH/USDT  from refs[exchange]
	//   BTC/ETH   from ethBtc[exchange] (MARKET cross, NOT the ethRef/btcMid implied)
	// The arbitrage signal is divergence between ethBtcMarket and the implied
	// ethRef/btcMid. When they match, both cycle ratios collapse to ≈ 1.0.
	//
	// Cycle A (USDT → BTC → ETH → USDT):
	//   1 USDT → 1/ask BTC → (1/ask)/ethBtcMarket ETH → (1/ask)*ethRef/ethBtcMarket USDT
	//   ratioA = ethRef / (ask * ethBtcMarket)
	//
	// Cycle B (USDT → ETH → BTC → USDT):
	//   1 USDT → 1/ethRef ETH → (ethBtcMarket/ethRef) BTC → (ethBtcMarket * bid)/ethRef USDT
	//   ratioB = (ethBtcMarket * bid) / ethRef
	ratioA := ethRef / (askF * ethBtcMarket)
	ratioB := (ethBtcMarket * bidF) / ethRef

	cycleGain := ratioA
	if ratioB > ratioA {
		cycleGain = ratioB
	}
	cycleGain -= 1.0

	netGainPerUSDT := cycleGain - 3*t.cfg.TakerFee
	netProfit := netGainPerUSDT * t.cfg.Notional

	// Train the per-exchange cycleGain model on every tick — including unprofitable
	// ones — so the distribution reflects what "normal" looks like for this venue.
	sm, ok := t.models[exchange]
	if !ok {
		sm = model.NewSpreadModel()
		t.models[exchange] = sm
	}
	sm.Update(cycleGain)

	if netProfit <= t.cfg.MinNetProfit {
		return nil
	}

	// Cooldown guard per exchange.
	if last, ok := t.last[exchange]; ok {
		if now.Sub(last) < t.cfg.EmitCooldown {
			return nil
		}
	}

	t.last[exchange] = now

	zScore, score := model.ScoreSignal(sm, cycleGain, netGainPerUSDT)

	opp := types.Opportunity{
		ID:           uuid.New().String(),
		BuyExchange:  exchange,
		SellExchange: exchange,
		NetProfit:    decimal.NewFromFloat(netProfit),
		NetProfitPct: decimal.NewFromFloat(cycleGain),
		ZScore:       decimal.NewFromFloat(zScore),
		Score:        decimal.NewFromFloat(score),
		DetectedAt:   now,
		Status:       types.StatusDetected,
		Strategy:     "triangular",
	}

	return []types.Opportunity{opp}
}

// SetTakerFee updates the taker fee fraction. Safe for concurrent use.
func (t *TriangularStrategy) SetTakerFee(v float64) {
	t.mu.Lock()
	t.cfg.TakerFee = v
	t.mu.Unlock()
}

// SetNoiseRange updates the ETH/USDT noise range. Safe for concurrent use.
func (t *TriangularStrategy) SetNoiseRange(v float64) {
	t.mu.Lock()
	t.cfg.NoiseRange = v
	t.mu.Unlock()
}
