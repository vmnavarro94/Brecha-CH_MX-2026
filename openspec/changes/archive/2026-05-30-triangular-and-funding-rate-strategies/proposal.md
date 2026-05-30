# Proposal: triangular-and-funding-rate-strategies

## Intent

Deliver three arbitrage strategies (spatial, triangular, funding) running in parallel on a single engine, each routed through its own executor path, with end-to-end P&L attribution visible in the StrategyPnL panel within a 60-second demo window.

## Scope (in)

- `internal/strategy/triangular/` — `TriangularStrategy` implementing Bellman-Ford negative-cycle detection over `{USDT, BTC, ETH}` per exchange, fed by a synthetic ETH/USDT reference price plus per-exchange ±0.1% noise. Zero connector or aggregator changes.
- `internal/strategy/funding/` — `FundingStrategy` with a side goroutine that generates synthetic funding rates every 30s for Binance, Bybit, OKX. Internal cache under RWMutex. `Detect()` reads cache and emits opportunities when `|funding_A - funding_B| > threshold` AND `now - lastEmit > 5s`.
- `internal/strategy/` — new `Starter` interface: `Start(ctx context.Context) error`. Both new strategies may implement it; FundingStrategy MUST.
- `internal/executor/funding_executor.go` — new `FundingExecutor` that records funding-differential trades without wallet debit/credit. Stamps `Strategy = "funding"` on the resulting Trade.
- `cmd/server/main.go` — register triangular + funding strategies in the engine slice. Wire `Starter.Start(ctx)` via type assertion at startup. In `runProcessingLoop`, dispatch on `opp.Strategy`: `"funding"` → `FundingExecutor`, else → existing `SpotExecutor`.
- Demo seeding — synthetic ETH/USDT reference initialized at startup from a hardcoded value (0.025 BTC per ETH inverse, i.e. seed ETH/USDT ≈ derived from BTC/USDT × hardcoded ETH/BTC). Refresh every 30s via synthetic walk, no REST. `DEMO_FUNDING_DIFFERENTIAL=0.015` default (1.5 bps) for funding seed.

## Scope (out)

- Real REST/WebSocket polling of funding rate endpoints (Binance Futures, Bybit, OKX). Day 2 uses synthetic generation only.
- Multi-symbol aggregator/connector refactor (Option T3 from exploration). `PriceUpdate` stays keyed by exchange. Triangular operates on synthetic ETH derivations.
- Time-accruing P&L for funding (8h settlement, position lifecycle). Single-shot model: one opportunity → one trade with lump-sum NetProfit.
- Per-strategy runtime toggle via `/api/config`. Strategies are statically registered at startup. Acceptable for a 60s demo.
- Changes to the existing `Strategy.Detect` signature. Both new strategies conform to the current event-driven interface; funding ignores the `update` argument and reads its internal cache.

## Public contract

**Triangular package** (`internal/strategy/triangular`):

```go
type Config struct {
    TakerFee      float64
    MinNetProfit  float64
    Notional      float64
    NoisePct      float64 // ±0.1% per-exchange
    RefreshEvery  time.Duration
    SeedETHperBTC float64 // hardcoded, e.g. 0.025
}

type TriangularStrategy struct { /* ... */ }

func New(cfg Config) *TriangularStrategy
func (s *TriangularStrategy) Name() string // "triangular"
func (s *TriangularStrategy) Detect(update PriceUpdate, snapshot map[string]PriceUpdate, now time.Time) []Opportunity
func (s *TriangularStrategy) SetTakerFee(f float64)
func (s *TriangularStrategy) SetMinNetProfit(p float64)
```

**Funding package** (`internal/strategy/funding`):

```go
type Config struct {
    Threshold        float64       // min differential to emit
    Cooldown         time.Duration // lastEmit guard, e.g. 5s
    PollInterval     time.Duration // 30s
    Notional         float64
    DemoDifferential float64       // seed differential, e.g. 0.015
    Venues           []string      // ["binance", "bybit", "okx"]
}

type FundingStrategy struct { /* ... */ }

func New(cfg Config) *FundingStrategy
func (s *FundingStrategy) Name() string // "funding"
func (s *FundingStrategy) Detect(update PriceUpdate, snapshot map[string]PriceUpdate, now time.Time) []Opportunity
func (s *FundingStrategy) Start(ctx context.Context) error // implements Starter
func (s *FundingStrategy) SetThreshold(t float64)
```

**Strategy lifecycle** (`internal/strategy/strategy.go`):

```go
type Starter interface {
    Start(ctx context.Context) error
}
```

**Funding executor** (`internal/executor/funding_executor.go`):

```go
type FundingExecutor struct { /* store, clock, logger */ }

func NewFundingExecutor(store *store.Store, clk clock.Clock) *FundingExecutor
func (e *FundingExecutor) Execute(opp Opportunity) (*Trade, error)
```

JSON shape: `Opportunity` and `Trade` are unchanged. The `Strategy` field already exists and differentiates routing. `BuyExchange` on a funding opportunity = venue with lower funding (long perp leg). `SellExchange` = venue with higher funding (short perp leg). `NetProfit = (funding_short - funding_long) × notional × normalization_factor`.

## Approach

**Triangular (Bellman-Ford on log-pricing).** Each exchange has a 3-vertex graph `{USDT, BTC, ETH}` and 6 directed edges weighted by `-log(rate)`. A negative cycle in this graph is a profitable arbitrage. For each `PriceUpdate` carrying a fresh BTC/USDT BBO from any exchange, the strategy derives ETH/USDT for that exchange using `BTC/USDT_mid × current_ETH/BTC_reference × (1 + per-exchange noise)`. ETH/BTC reference is a synthetic walk seeded at startup (0.025 default), updated every 30s with bounded random drift. The strategy then runs Bellman-Ford on the resulting 3-node graph and emits one opportunity per detected negative cycle whose `cycle_gain - 3 × taker_fee > MinNetProfit`. Cost model is single-venue (no withdrawal, no inter-exchange latency). Opportunity carries `BuyExchange = SellExchange = the_venue`, `Strategy = "triangular"`, `NetProfit = (cycle_gain - 3 × taker_fee) × notional`.

**Funding (side-goroutine poller).** On `Start(ctx)`, the strategy launches a goroutine that ticks every `PollInterval` (30s). Each tick generates synthetic funding rates per configured venue. Seeded so that at least one pair has `|differential| > Threshold` at t=0 (so opportunities appear in the demo's first minute). Rates are written into `map[string]float64` under RWMutex. `Detect()` is called per upstream price update but ignores `update`; on each call it scans all venue pairs, finds the maximum differential, and emits an opportunity only if both `differential > Threshold` AND `now - lastEmitAt[pair] > Cooldown`. Opportunity has `BuyExchange = lower_funding_venue`, `SellExchange = higher_funding_venue`, `Strategy = "funding"`, `NetProfit = (funding_high - funding_low) × Notional × 1.0` (single-shot, no 8h normalization).

**Executor dispatch.** `runProcessingLoop` in `cmd/server/main.go` currently calls `spotExec.Execute(opp)` unconditionally. Change to:

```go
var trade *Trade
var err error
switch opp.Strategy {
case "funding":
    trade, err = fundingExec.Execute(opp)
default:
    trade, err = spotExec.Execute(opp)
}
```

Spot path unchanged: existing `MultiWallet` debit/credit semantics preserved for spatial AND triangular (triangular is a single-venue 3-leg, but for Day 2 demo it can use the spot executor since the net effect on the wallet is a roundtrip — alternative: triangular emits opportunities that bypass wallet impact; if so, triangular also routes to a dedicated executor, decided in design phase). Funding path goes through `FundingExecutor` which records the trade in the store with the synthetic NetProfit and zero wallet impact.

**Startup wiring.** After constructing each strategy and before `engine.NewEngine`, iterate the strategy slice and call `Start(ctx)` on those that implement `Starter`:

```go
for _, st := range strategies {
    if starter, ok := st.(strategy.Starter); ok {
        if err := starter.Start(ctx); err != nil {
            log.Fatal(err)
        }
    }
}
```

## Risks

1. **Executor incompatibility for funding** (carried from explore) — Mitigated by dedicated `FundingExecutor` recording synthetic P&L without wallet impact. Trade-off: introduces dispatch logic in `runProcessingLoop`, a new structural seam.
2. **Synthetic triangular prices** (carried) — ETH/USDT is derived, not streamed. Per-exchange noise injection (±0.1%) creates the divergence needed for Bellman-Ford to find negative cycles, but the UI may display these synthetic ETH prices, which could confuse demo observers. Mitigation: clearly label as "synthetic" in any UI surfacing, or omit ETH prices from the UI for Day 2.
3. **Executor dispatch as new structural risk** — `runProcessingLoop` now branches on `opp.Strategy`. Future strategies require either extending this switch or moving to a strategy→executor registry. For 3 strategies it's fine; beyond that it becomes a smell.
4. **Funding strategy emits on every price update** (carried) — `lastEmitAt` cooldown is mandatory. Defined at 5s; the design phase will validate this against the upstream update rate (~50 updates/sec from 10 exchanges).
5. **Engine heap flooding** (carried) — Three strategies × 50 updates/sec = 150 Detect calls/sec. Bellman-Ford on a 3-node graph is O(V·E) = O(18); funding Detect is O(venue_pairs) = O(3). No throughput concern at this scale.
6. **PriceUpdate keyed by exchange only** (carried) — Day 2 explicitly avoids fixing this. Triangular is demo scaffolding via synthetic derivation. Documented constraint, not a regression.
7. **No runtime toggle** (carried) — Static registration only. If triangular produces noisy false positives the only recourse is restart. Acceptable for 60s demo.
8. **Triangular wallet semantics** — Open question deferred to design: does triangular use SpotExecutor (with debit/credit of intermediate legs) or a dedicated executor? Roundtrip-on-one-venue net effect on the wallet may be acceptable, but the multi-leg debit sequence is non-trivial. Design phase decides.

## Success criteria

- All 3 strategies (spatial, triangular, funding) are registered in `cmd/server/main.go` and produce at least one opportunity each within 60 seconds of bot start in DemoMode.
- `StrategyPnL` panel in the web UI shows 3 rows (`spatial`, `triangular`, `funding`) with non-zero trade counts.
- Spatial trades continue to flow through `SpotExecutor` with `MultiWallet` debit/credit — existing behavior unchanged. All baseline integration tests for spatial pass.
- Funding trades flow through `FundingExecutor` with zero wallet impact and synthetic P&L recorded in the store.
- Triangular Bellman-Ford produces an opportunity when a known synthetic cycle has a positive net log-gain after fees.
- All 127 baseline tests pass after migration. New tests cover: triangular cycle detection on a fixture graph, funding differential emission, `lastEmitAt` cooldown enforcement, executor dispatch on `opp.Strategy`.
- No new external Go dependencies. All implementation uses stdlib + existing project modules.
