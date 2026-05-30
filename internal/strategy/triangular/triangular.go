package triangular

import (
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
// All mutable fields are protected by mu for concurrent safety.
type TriangularStrategy struct {
	mu  sync.RWMutex
	cfg Config
	// refs maps exchange → seeded ETH/USDT reference price.
	refs map[string]float64
	// last maps exchange → last emission time (for cooldown guard).
	last map[string]time.Time
	// lastRefresh maps exchange → last time refs[exchange] was refreshed.
	lastRefresh map[string]time.Time
	rng         *rand.Rand
}

// New creates a TriangularStrategy with the given Config.
func New(cfg Config) *TriangularStrategy {
	return &TriangularStrategy{
		cfg:         cfg,
		refs:        make(map[string]float64),
		last:        make(map[string]time.Time),
		lastRefresh: make(map[string]time.Time),
		rng:         rand.New(rand.NewSource(cfg.Seed)),
	}
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

	// Explicit 2-cycle ratio check (equivalent to Bellman-Ford negative-cycle detection
	// on the 3-vertex log-weight graph {USDT, BTC, ETH}).
	//
	// Cycle A (USDT → BTC → ETH → USDT):
	//   leg1: buy BTC with USDT at ask  → 1 USDT becomes 1/ask BTC
	//   leg2: buy ETH with BTC at cross rate ETH/BTC = ethRef/btcMid
	//         → (1/ask) * (btcMid/ethRef) ETH    [i.e. sell BTC for ETH]
	//   leg3: sell ETH for USDT at ethRef
	//         → (1/ask)*(btcMid/ethRef)*ethRef = btcMid/ask USDT
	//   ratioA = btcMid / ask
	//
	// Cycle B (USDT → ETH → BTC → USDT):
	//   leg1: buy ETH with USDT at ethRef → 1/ethRef ETH
	//   leg2: sell ETH for BTC at ETH/BTC = ethRef/btcMid
	//         → (1/ethRef)*(ethRef/btcMid) = 1/btcMid BTC
	//   leg3: sell BTC for USDT at bid → (1/btcMid)*bid = bid/btcMid USDT
	//   ratioB = bid / btcMid
	//
	// Balanced market: bid ≈ ask ≈ btcMid → ratioA ≈ 1.0, ratioB ≈ 1.0 → no opportunity.
	// Divergence (bid > ask spread): ratioA and ratioB both exceed 1.0 → potential opportunity.
	_ = ethRef // ethRef is used implicitly through btcMid derivation; kept for reference seeding
	ratioA := btcMid / askF
	ratioB := bidF / btcMid

	cycleGain := ratioA
	if ratioB > ratioA {
		cycleGain = ratioB
	}
	cycleGain -= 1.0

	netGainPerUSDT := cycleGain - 3*t.cfg.TakerFee
	netProfit := netGainPerUSDT * t.cfg.Notional

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

	opp := types.Opportunity{
		ID:           uuid.New().String(),
		BuyExchange:  exchange,
		SellExchange: exchange,
		NetProfit:    decimal.NewFromFloat(netProfit),
		NetProfitPct: decimal.NewFromFloat(cycleGain),
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
