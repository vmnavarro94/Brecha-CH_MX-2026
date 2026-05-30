# Proposal: multi-strategy-framework

## Intent

Refactor the monolithic `engine.Engine` into a thin coordinator that drives a pluggable `strategy.Strategy` interface, with the current spatial-arbitrage logic extracted into a self-contained `internal/strategy/spatial/` package, so future strategies (triangular, funding-rate) can be added without touching engine internals and so per-strategy P&L attribution becomes possible end-to-end.

## Scope (in)

- **New package `internal/strategy/`**: contains only the `Strategy` interface and shared shapes (no implementations).
- **New package `internal/strategy/spatial/`**: owns `SpatialStrategy`, `FeeConfig`, `Config` (MinNetProfitPct + MaxPositionUSDT + Fees map), the `map[string]*model.SpreadModel` registry, cost calculation, scoring (`netPct*0.6 + sigmoid(z)*0.4`), and the `SpreadStats()` accessor for the publisher.
- **Engine refactor** in `internal/engine/engine.go`: becomes a coordinator holding `[]strategy.Strategy`, the opportunity heap, the latency tracker, `OpportunityTTL`, `StalenessThreshold`, `Clock`, and `snapshotFn`. `ProcessUpdate` fans out to each registered strategy and merges returned `[]types.Opportunity` into the heap.
- **Type extension** in `internal/types/types.go`: add `Strategy string \`json:"strategy,omitempty"\`` to both `Opportunity` and `Trade`. `omitempty` preserves backward compatibility for existing SQLite rows and JSON payloads.
- **Executor propagation** in `internal/executor/executor.go`: mechanical copy `trade.Strategy = opp.Strategy`. No other logic change.
- **main.go rewiring** in `cmd/server/main.go`:
  - Construct `*spatial.SpatialStrategy` first (with fees, MinNetProfitPct, MaxPositionUSDT, models map).
  - Construct `*engine.Engine` with `[]strategy.Strategy{spatial}` plus coordinator config (TTL, staleness, snapshotFn, clock).
  - `patchConfigFn` holds both refs: calls TTL/staleness setters on engine, fees/MinNetProfitPct/MaxPositionUSDT setters on spatial.
  - `publishSpreadStats` reads from `spatial.SpreadStats()` instead of a local map.
- **New endpoint `/api/pnl-by-strategy`** in `internal/server/api.go`: aggregates `store` trades grouped by `Strategy` field, returns `{ strategy: string, trades: int, gross: decimal, fees: decimal, net: decimal }[]`.
- **Frontend `StrategyPnL` panel** under `web/src/components/`: dedicated panel rendered in `App.tsx`, consuming the new endpoint, treated as a separate categorical axis from per-pair P&L.

## Scope (out)

- **New strategies** (Triangular, FundingRate) — these are Day 2 work; this change only delivers the framework + the first implementation (Spatial).
- **Backtest engine** — Day 3 work; out of scope.
- **Per-strategy risk parameter overrides beyond what Spatial currently exposes** — risk parameters (`MinNetProfitPct`, `MaxPositionUSDT`, fees) stay scoped to Spatial as today; no global router or multi-strategy risk arbiter.
- **Engine concurrency changes** — `ProcessUpdate` stays single-goroutine; no fan-out goroutines per strategy, no channels, no `Run(ctx)` stream model.
- **Server `FeeInfo` type unification** — `internal/server` keeps its own `FeeInfo` / `FeeInfoPatch` shapes; mapper functions translate to/from `spatial.FeeConfig`. Zero shared types across the boundary (matches existing pattern).
- **DB schema migration** — `Strategy` rides on the existing JSON payload column; old rows deserialise to empty string and the frontend renders an "unknown" fallback.

## Public contract

### Interface

```go
// internal/strategy/strategy.go
package strategy

type Strategy interface {
    Name() string
    Detect(update types.PriceUpdate, snapshot map[string]map[string]*types.PriceUpdate, now time.Time) []types.Opportunity
}
```

The snapshot shape mirrors what `engine` already passes to its private detection step; no new aggregate type required. `now` is passed explicitly so strategies stay clock-injectable without owning a `Clock`.

### Spatial package

```go
// internal/strategy/spatial/spatial.go
package spatial

type FeeConfig struct {
    TakerFee          float64
    SlippageFactor    float64
    WithdrawalBTC     float64
    NetworkLatencyBps float64
}

type Config struct {
    Fees             map[string]FeeConfig
    MinNetProfitPct  float64
    MaxPositionUSDT  float64
}

type SpatialStrategy struct { /* ... */ }

func New(cfg Config) *SpatialStrategy
func (s *SpatialStrategy) Name() string                    // returns "spatial"
func (s *SpatialStrategy) Detect(...) []types.Opportunity  // implements strategy.Strategy

// typed setters used by main.go patchConfigFn
func (s *SpatialStrategy) SetFees(map[string]FeeConfig)
func (s *SpatialStrategy) SetMinNetProfitPct(float64)
func (s *SpatialStrategy) SetMaxPositionUSDT(float64)

// publisher accessor
func (s *SpatialStrategy) SpreadStats() map[string]model.SpreadStats
```

### Engine

```go
// internal/engine/engine.go (post-refactor)
type Config struct {
    OpportunityTTL     time.Duration
    StalenessThreshold time.Duration
}

func NewEngine(
    cfg Config,
    strategies []strategy.Strategy,
    snapshotFn func() map[string]map[string]*types.PriceUpdate,
    clock types.Clock,
) *Engine

func (e *Engine) ProcessUpdate(types.PriceUpdate)
func (e *Engine) DequeueTop() (*types.Opportunity, bool)
func (e *Engine) SetClock(types.Clock)
func (e *Engine) SetStalenessThreshold(time.Duration)
func (e *Engine) SetOpportunityTTL(time.Duration)
func (e *Engine) LatencyStats() ...  // unchanged
```

Removed from engine: `FeeConfig`, `Fees`, `MinNetProfitPct`, `MaxPositionUSDT`, `models`, `SetFees`, `SetMinNetProfitPct`, `SetMaxPositionUSDT`, `feeFor`, `computeScore`.

### Types

```go
type Opportunity struct {
    // ... existing fields ...
    Strategy string `json:"strategy,omitempty"`
}

type Trade struct {
    // ... existing fields ...
    Strategy string `json:"strategy,omitempty"`
}
```

### Store / API surface

- `store.pairStats` aggregation is unchanged. A new optional `strategyStats` accumulator (or an ad-hoc SQL aggregation over the JSON payload) feeds the new endpoint.
- New endpoint: `GET /api/pnl-by-strategy` returning `[]StrategyPnLRow`.

## Approach

**Mechanical migration, preserving exact semantics.**

1. **Extract verbatim**: move `feeFor`, `computeScore`, the inner loop body of `ProcessUpdate` (iterating snapshot counterparties), the spread-model update, and the staleness check into `SpatialStrategy.Detect`. No behavioural change, no decimal arithmetic change, no ordering change.
2. **Engine shrinks to coordinator**: post-refactor, `engine.go` logic is approximately:
   - record latency-tracker start
   - fetch snapshot via `snapshotFn`
   - check `update.IsStale(StalenessThreshold)` once (coordinator concern, shared by all strategies)
   - for each `s := range strategies`, call `s.Detect(update, snapshot, clock.Now())`, append returned opps (with `opp.Strategy = s.Name()` enforced) to heap
   - record latency-tracker end
   - Target: ~40 lines of logic excluding type defs and trivial setters.
3. **Setters live on the type that owns the state**: fees/profit/position setters → SpatialStrategy. TTL/staleness setters → Engine. No generic `UpdateConfig(json.RawMessage)`; main.go's `patchConfigFn` dispatches typed setters explicitly.
4. **main.go construction order**: spatial first, engine second. Engine receives `[]strategy.Strategy{spatial}`. `publishSpreadStats` closure captures `spatial` and calls `spatial.SpreadStats()`.
5. **Test migration**: all 12 tests in `internal/engine/engine_test.go` covering spatial detection, scoring, fees, profit threshold, staleness skip, and Welford warm-up move verbatim to `internal/strategy/spatial/spatial_test.go`, with `engine.NewEngine(engine.Config{...})` calls replaced by `spatial.New(spatial.Config{...})` + direct `Detect` invocation. Engine retains a smaller test suite focused on heap ordering, TTL expiry, and fan-out across multiple strategies (using a fake strategy stub).
6. **Executor**: one line added in `Execute`: `trade.Strategy = opp.Strategy`. Existing executor tests unchanged; one new test asserts propagation.
7. **API + frontend**: additive. New handler reads `store.Trades()` (or a dedicated `TradesByStrategy()` accessor if the linear scan is too expensive), groups by `Strategy`, returns aggregate rows. Frontend `StrategyPnL` panel is a sibling of `PerPairPnL`, not a column inside it — strategy attribution and exchange-pair attribution are independent categorical axes and conflating them would force a cross-product table.

## Risks

- **Test migration volume (HIGH effort, LOW logic risk)**: 12 engine tests reference `engine.Config`, `engine.FeeConfig`, `engine.NewEngine`. Migration is mechanical (rename package, swap constructor) but touches every test. Mitigation: migrate in one commit, run `go test ./...` to confirm byte-equivalence of behaviour.
- **SQLite backward compatibility (LOW)**: old trade rows have no `Strategy` field in the JSON payload. `omitempty` + Go's default empty-string deserialisation handles read; frontend needs a display fallback like `"unknown"` or `"spatial (legacy)"`.
- **patchConfigFn split (MEDIUM cognitive load)**: the existing ~60-line closure must now call setters on two distinct objects. Risk: future contributor adds a new setter to the wrong type. Mitigation: comment block at the top of `patchConfigFn` documenting the ownership split.
- **Spread model accessor (LOW)**: `publishSpreadStats` currently reads a map directly; after the refactor it calls `spatial.SpreadStats()`. The accessor must return a snapshot (copy) to avoid races with the engine processing goroutine. Mitigation: implement `SpreadStats()` with an explicit copy under the existing mutex.
- **Hot-path performance (LOW)**: one extra interface dispatch and one slice allocation per `ProcessUpdate` per strategy. Negligible for a single strategy; worth a benchmark before registering 5+ strategies in Day 2. Mitigation: add a benchmark in the spatial test file to lock in the baseline.
- **Frontend "strategy" field absence (LOW)**: trades from old DB rows show no strategy; the new panel will display an "unknown" bucket. Acceptable for demo; flag in proposal.

## Success Criteria

1. **All 109 existing tests pass after migration** (including the 12 spatial-detection tests now living in `internal/strategy/spatial/spatial_test.go`).
2. **`internal/engine/engine.go` is under 60 lines of logic** (excluding type definitions, trivial setters that moved out, and the heap implementation file if split).
3. **`GET /api/pnl-by-strategy` returns aggregated P&L per strategy name**, with at least one row (`{"strategy": "spatial", ...}`) once trades exist.
4. **`StrategyPnL` panel renders in the frontend with at least 1 row** ("spatial") when the demo has executed trades.
5. **Existing demo behaviour is byte-equivalent**: replaying the same WebSocket frames through the refactored stack must produce identical opportunities and trades (same IDs, prices, fees, scores) as the pre-refactor stack. This is the primary correctness gate.
