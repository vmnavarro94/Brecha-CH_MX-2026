# Design: triangular-and-funding-rate-strategies

## Overview

Adds two new strategies (triangular, funding) alongside the existing spatial strategy, a `Starter` interface for strategies that need a side goroutine, and two new executors that record synthetic-P&L trades without wallet mutations. Routing in `runProcessingLoop` becomes a `switch` over `opp.Strategy`. No engine, aggregator, or connector changes.

## Architecture approach

- **Pattern**: extend the existing pluggable strategy registry. Each new strategy implements `strategy.Strategy`; one of them (`funding`) additionally implements a new `strategy.Starter` interface to own its lifecycle.
- **Boundaries**: strategies remain pure detectors (no wallet, no store). Executors translate `Opportunity` into recorded `Trade` rows in the store. The engine remains a thin coordinator.
- **Determinism**: both new strategies seed all randomness from configurable `Seed` values; tests run with a fixed clock and a fixed seed.

## Package layout

```
internal/strategy/
  strategy.go              (existing) — Strategy interface
  starter.go               (NEW)      — Starter interface
internal/strategy/triangular/
  triangular.go            (NEW)      — TriangularStrategy + Config + cycle math
  triangular_test.go       (NEW)
internal/strategy/funding/
  funding.go               (NEW)      — FundingStrategy + Config + poller
  funding_test.go          (NEW)
internal/executor/
  executor.go              (existing) — spot path, unchanged signature
  funding_executor.go      (NEW)      — FundingExecutor
  funding_executor_test.go (NEW)
  triangular_executor.go   (NEW)      — TriangularExecutor
  triangular_executor_test.go (NEW)
cmd/server/main.go         (MODIFIED) — register strategies, start Starters, dispatch executor by opp.Strategy
```

## Starter interface

```go
// internal/strategy/starter.go
package strategy

import "context"

// Starter is implemented by strategies that own a background goroutine.
// Start MUST return promptly after launching the goroutine. The goroutine
// MUST exit when ctx.Done() is closed.
type Starter interface {
    Start(ctx context.Context) error
}
```

`FundingStrategy` implements `Starter`. `SpatialStrategy` and `TriangularStrategy` do not. `cmd/server/main.go` iterates the strategy slice, type-asserts to `strategy.Starter`, and calls `Start(ctx)` on matches before the engine runs.

## TriangularStrategy

### Public contract

```go
package triangular

type TriangularStrategy struct {
    mu       sync.RWMutex
    cfg      Config
    refs     map[string]float64    // exchange -> ETH/USDT reference at seeding
    last     map[string]time.Time  // exchange -> lastEmitAt (cooldown gate)
    rng      *rand.Rand            // seeded
}

type Config struct {
    Exchanges       []string      // ["binance","bybit","okx","cryptocom","mexc"] in practice
    TakerFee        float64       // applied 3x per cycle (e.g. 0.0008)
    NoiseRange      float64       // ±range applied to SeedRefPrice per exchange (e.g. 0.001 = 0.1%)
    SeedRefPrice    float64       // 2000.0 USDT per ETH
    RefreshEvery    time.Duration // 30s random-walk refresh
    Notional        float64       // USDT amount per leg (used for cost-model deduction)
    MinNetProfit    float64       // gate to emit
    EmitCooldown    time.Duration // 5s per exchange
    Seed            int64
}

func New(cfg Config) *TriangularStrategy
func (t *TriangularStrategy) Name() string { return "triangular" }
func (t *TriangularStrategy) Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
func (t *TriangularStrategy) SetTakerFee(v float64)
func (t *TriangularStrategy) SetNoiseRange(v float64)
```

### Cycle algorithm — explicit 3-cycle ratio check

Bellman-Ford on a 3-vertex graph collapses to checking the two directed 3-cycles. We implement the explicit check (clearer, cheaper, easier to test) and document the equivalence to BF in code comments. The spec requirement TR2 ("Bellman-Ford negative-cycle detection") is satisfied semantically: a negative log-sum cycle is mathematically identical to a product-of-rates > 1.0 cycle. The comment in `triangular.go` MUST state this equivalence.

For each exchange in `cfg.Exchanges` where the snapshot has a fresh BTC/USDT update:

```
1. btcMid = (update.Ask + update.Bid) / 2
2. ethRef = refs[exchange]          // seeded ±0.1% per-exchange noise around 2000
3. ethBtc = ethRef / btcMid         // implied cross rate
4. Cycle A: USDT -> BTC -> ETH -> USDT
       ratioA = (1/update.Ask) * (btcMid/ethRef) * ethRef
              = btcMid / update.Ask
   Cycle B: USDT -> ETH -> BTC -> USDT
       ratioB = (1/ethRef) * (ethRef/btcMid) * update.Bid
              = update.Bid / btcMid
5. cycleGain = max(ratioA, ratioB) - 1.0          // fractional gain per USDT
6. netGainPerUSDT = cycleGain - 3 * cfg.TakerFee  // 3 legs * taker_fee
7. netProfit = netGainPerUSDT * cfg.Notional
8. Emit only if netProfit > cfg.MinNetProfit AND now - last[exchange] >= cfg.EmitCooldown
```

The `ETH/USDT` reference is held in-memory and seeded once per exchange at construction with `SeedRefPrice * (1 + (rng.Float64()*2 - 1) * NoiseRange)`. A background random walk (small step every `RefreshEvery`) keeps the reference drifting; implemented inline in `Detect` (lazy, no goroutine) using a stored `lastRefresh` timestamp — `TriangularStrategy` does NOT need to implement `Starter`.

### Opportunity construction

```go
return []types.Opportunity{{
    ID:           uuid.NewString(),
    Strategy:     "triangular",
    BuyExchange:  exchange,
    SellExchange: exchange,       // intra-exchange
    Symbol:       "BTC-USDT",     // anchor symbol; downstream UI groups by Strategy anyway
    BuyPrice:     decimal.NewFromFloat(update.Ask),
    SellPrice:    decimal.NewFromFloat(update.Bid),
    NetProfit:    decimal.NewFromFloat(netProfit),
    NetProfitPct: decimal.NewFromFloat(netGainPerUSDT),
    DetectedAt:   now,
}}
```

### Concurrency

All `refs`, `last`, and config reads/writes go through `mu`. Setters use `Lock()`; `Detect` uses `RLock()` for reads and a short `Lock()` segment when stamping `last[exchange]`.

## FundingStrategy

### Public contract

```go
package funding

type FundingStrategy struct {
    mu        sync.RWMutex
    cfg       Config
    rates     map[string]float64    // exchange -> current funding rate
    lastEmit  map[string]time.Time  // pair-key -> lastEmitAt
    started   bool
    rng       *rand.Rand            // seeded
    ticks     uint64                // monotonic poll counter
}

type Config struct {
    Exchanges        []string      // ["binance","bybit","okx"]
    Threshold        float64       // 0.00015 (1.5 bps)
    PollInterval     time.Duration // 30s
    EmitCooldown     time.Duration // 5s per venue pair
    Notional         float64       // single-shot notional (== MaxPositionUSDT)
    BaseDifferential float64       // 0.00025 mean differential between high and low
    Seed             int64
}

func New(cfg Config) *FundingStrategy
func (f *FundingStrategy) Name() string { return "funding" }
func (f *FundingStrategy) Start(ctx context.Context) error
func (f *FundingStrategy) Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
func (f *FundingStrategy) SetThreshold(v float64)
```

### Start semantics

`Start(ctx)` launches one goroutine. The goroutine seeds `rates` immediately (so the first `Detect` after Start has data) and ticks every `cfg.PollInterval`. On `<-ctx.Done()` the goroutine returns. `Start` returns nil after launching. Calling `Start` twice is a no-op (guarded by `started` under `mu`).

Without `Start`, `rates` stays nil and `Detect` returns an empty slice.

### Synthetic rate generator

```
On each tick (and at t=0):
    ticks++
    for i, ex := range cfg.Exchanges:
        // Two-component mix: oscillating + jittered random walk
        wave := math.Sin(float64(ticks)/3.0 + float64(i)) * (cfg.BaseDifferential)
        jitter := (rng.Float64()*2 - 1) * cfg.BaseDifferential * 0.5
        rates[ex] = wave + jitter
```

This produces deterministic oscillation around 0 with peak-to-peak differential ~= `2 * BaseDifferential`, comfortably exceeding `Threshold` periodically. Seeded `rng` makes tests reproducible.

### Detect semantics

```
1. RLock; copy rates snapshot; RUnlock
2. If len(rates) == 0: return nil
3. Find maxEx and minEx by rate value
4. diff = rates[maxEx] - rates[minEx]
5. If diff < cfg.Threshold: return nil
6. pairKey = minEx + "->" + maxEx
7. If now - lastEmit[pairKey] < cfg.EmitCooldown: return nil
8. Emit one Opportunity:
       Strategy:     "funding"
       BuyExchange:  minEx     // low/negative funding → long perp here
       SellExchange: maxEx     // high/positive funding → short perp here
       NetProfit:    decimal.NewFromFloat(diff * cfg.Notional)
       NetProfitPct: decimal.NewFromFloat(diff)
       DetectedAt:   now
9. Lock; lastEmit[pairKey] = now; Unlock
```

`Detect` ignores `update` and `snapshot` — funding is venue-property, not price-driven.

## Executors

### FundingExecutor

```go
package executor

type FundingExecutor struct {
    store *store.Store
    clock types.Clock
}

func NewFundingExecutor(st *store.Store, clk types.Clock) *FundingExecutor

func (e *FundingExecutor) Execute(opp *types.Opportunity) error {
    trade := &types.Trade{
        ID:              uuid.NewString(),
        OpportunityID:   opp.ID,
        Strategy:        "funding",
        BuyExchange:     opp.BuyExchange,
        SellExchange:    opp.SellExchange,
        Symbol:          opp.Symbol,
        Volume:          decimal.NewFromFloat(1.0),
        RequestedVolume: decimal.NewFromFloat(1.0),
        PartialFill:     false,
        BuyPrice:        opp.BuyPrice,
        SellPrice:       opp.SellPrice,
        GrossProfit:     opp.NetProfit,
        Fees:            decimal.Zero,
        Slippage:        decimal.Zero,
        NetProfit:       opp.NetProfit,
        ExecutedAt:      e.clock.Now(),
    }
    opp.Status = types.StatusExecuted
    return e.store.SaveTrade(trade)
}
```

No wallet operations: funding settlement is a synthetic single-shot P&L credit. No risk checks here — the engine's `DequeueTop` already gates by configured risk thresholds.

### TriangularExecutor

Identical shape to `FundingExecutor`, but `Strategy = "triangular"`, `Volume = decimal.NewFromFloat(cfg.Notional)` (USDT cycle notional, recorded for audit), and `Fees = decimal.NewFromFloat(3 * cfg.TakerFee * cfg.Notional)` so the recorded `GrossProfit = NetProfit + Fees` math stays internally consistent.

```go
trade := &types.Trade{
    ...
    Strategy:    "triangular",
    Volume:      decimal.NewFromFloat(notional),
    Fees:        decimal.NewFromFloat(3 * takerFee * notional),
    GrossProfit: opp.NetProfit.Add(decimal.NewFromFloat(3 * takerFee * notional)),
    NetProfit:   opp.NetProfit,
    ...
}
```

`TriangularExecutor` keeps a small `cfg` (or constructor args) holding `TakerFee` and `Notional` so the fee math is self-contained.

## main.go integration

```go
// internal/strategy registration
spat := spatial.New(spatialCfg, spreadModels)
tri  := triangular.New(triangular.Config{
    Exchanges:    exchangeIDs,
    TakerFee:     0.0008,
    NoiseRange:   0.001,
    SeedRefPrice: 2000.0,
    RefreshEvery: 30 * time.Second,
    Notional:     cfg.MaxPositionUSDT,
    MinNetProfit: 0.01,
    EmitCooldown: 5 * time.Second,
    Seed:         time.Now().UnixNano(),
})
fund := funding.New(funding.Config{
    Exchanges:        []string{"binance", "bybit", "okx"},
    Threshold:        0.00015,
    PollInterval:     30 * time.Second,
    EmitCooldown:     5 * time.Second,
    Notional:         cfg.MaxPositionUSDT,
    BaseDifferential: 0.00025,
    Seed:             time.Now().UnixNano(),
})

strategies := []strategy.Strategy{spat, tri, fund}

// Start Starter-implementing strategies
for _, s := range strategies {
    if starter, ok := s.(strategy.Starter); ok {
        if err := starter.Start(ctx); err != nil {
            return fmt.Errorf("strategy %s start: %w", s.Name(), err)
        }
    }
}

eng := engine.NewEngine(snapshotFn, clk, engine.Config{OpportunityTTL: cfg.OpportunityTTL}, strategies)

// Executors
spotExec       := executor.NewExecutor(walletMgr, store, ...)   // existing
fundingExec    := executor.NewFundingExecutor(store, clk)
triangularExec := executor.NewTriangularExecutor(store, clk, 0.0008, cfg.MaxPositionUSDT)

// In runProcessingLoop:
var execErr error
switch opp.Strategy {
case "funding":
    execErr = fundingExec.Execute(opp)
case "triangular":
    execErr = triangularExec.Execute(opp)
default:
    execErr = spotExec.Execute(opp)
}
```

## ADR-style decisions

### ADR-1: Triangular cost model

**Decision**: `net_cycle_gain = cycle_gain_per_USDT * notional - 3 * taker_fee * notional`. Each of the 3 legs deducts `taker_fee * notional` independently. No withdrawal fee, no network latency cost, no slippage model (intra-exchange).

**Rationale**: All three legs are intra-exchange spot trades; each consumes the taker side once. Single-venue cycle = no transfer cost. Matching the spec (TR2) `NetProfit = (cycle_gain - 3 * taker_fee) * notional`.

### ADR-2: Triangular algorithm — explicit 3-cycle ratio check, not generalized Bellman-Ford

**Decision**: Implement the two directed 3-cycles explicitly (`USDT->BTC->ETH->USDT` and `USDT->ETH->BTC->USDT`) and check `ratio > 1 + threshold_after_fees`. Document in code that this is mathematically equivalent to Bellman-Ford negative-cycle detection on the log-weighted 3-vertex graph.

**Rationale**: 3 vertices, 2 cycles. BF's expressive power for larger graphs is irrelevant here. Explicit check is shorter, faster, easier to test, and clearer to read in PR review. The spec phrasing "Bellman-Ford negative-cycle detection" describes the semantic, which is preserved.

### ADR-3: Dedicated TriangularExecutor (overrides FE2 hint)

**Decision**: Triangular opportunities route to a new `TriangularExecutor`, NOT to `SpotExecutor`. This OVERRIDES the spec FE2 text ("spatial and triangular both use SpotExecutor as default") and resolves the open question flagged in Proposal risk #8.

**Rationale**: `SpotExecutor` is built around two-venue Debit/Credit semantics (buy on A, sell on B, MultiWallet rebalances). Triangular is intra-exchange and round-trips USDT through BTC and ETH — no net wallet position change. Forcing it through `SpotExecutor` would require simulating 3 sequential trades or fabricating fake counterparty wallets. Cleaner: one synthetic trade with the 3-leg fee bundle.

## Data flow

```
PriceUpdate (from aggregator)
    │
    ▼
engine.ProcessUpdate
    │
    ├─→ spatial.Detect       → []Opportunity (Strategy="spatial")
    ├─→ triangular.Detect    → []Opportunity (Strategy="triangular")
    └─→ funding.Detect       → []Opportunity (Strategy="funding")
        │
        ▼
    engine heap (DequeueTop respects risk thresholds)
        │
        ▼
    runProcessingLoop
        │
        ▼
    switch opp.Strategy {
        case "funding":    → fundingExec.Execute    → store.SaveTrade
        case "triangular": → triangularExec.Execute → store.SaveTrade
        default:           → spotExec.Execute       → MultiWallet + store.SaveTrade
    }
        │
        ▼
    /api/pnl-by-strategy ← GROUP BY trade.strategy
        │
        ▼
    StrategyPnL panel (3 rows)
```

Funding cache is updated independently by the side goroutine started via `Starter`.

## Testing strategy

Strict TDD is active. Each public-facing behavior gets a failing test before implementation. Key test files:
- `internal/strategy/triangular/triangular_test.go`
- `internal/strategy/funding/funding_test.go`
- `internal/executor/funding_executor_test.go`
- `internal/executor/triangular_executor_test.go`
