# Design: multi-strategy-framework

## 1. Architectural approach

The current `engine.Engine` is a monolith that owns four orthogonal concerns at once: snapshot fan-out, spatial arbitrage detection, fee-aware cost math, and an opportunity heap with a TTL. The chosen approach is to split those concerns along the seam that already exists implicitly: the coordinator (heap, latency, fan-out, TTL) versus the strategy (detection + costs + scoring + spread models). We introduce a small `strategy.Strategy` interface and move the entire spatial path behind it into a self-contained package. The engine becomes a thin coordinator that knows nothing about fees, prices, or spread models, only that it owns a list of strategies and merges their outputs into a single priority queue.

This is a Strategy pattern. We deliberately do NOT introduce a registry, dependency injection container, or plugin system. Strategies are constructed in `main.go` and passed into the engine as a slice — explicit wiring, no reflection, no init() side effects. The Strategy interface stays small (two methods) because every additional method becomes a contract every future strategy must satisfy.

Concurrency model is unchanged. `ProcessUpdate` is still single-goroutine. Per-strategy state (config, spread models, fees) is protected by a `sync.RWMutex` owned by each strategy, mirroring the current `cfgMu` on Engine. The engine itself becomes simpler concurrency-wise because it no longer mutates fee tables or spread models.

## 2. Package layout

```
internal/strategy/
  strategy.go          interface Strategy { Name(); Detect(...) }
internal/strategy/spatial/
  spatial.go           SpatialStrategy + Config + setters + Detect
  fees.go              FeeConfig type + feeFor helper + defaults
  spatial_test.go      migrated from engine_test.go (12 detection scenarios)
internal/engine/
  engine.go            Engine coordinator, ~60 lines of logic
  heap.go              oppHeap + scoredOpportunity (extracted for clarity)
  engine_test.go       shrunk to coordinator-only tests
  latency.go           unchanged
```

Splitting `heap.go` out of `engine.go` keeps the 60-line logic budget honest. The heap is a mechanical container/heap implementation; it does not belong inline with coordinator logic.

## 3. Strategy interface

```go
package strategy

import (
    "time"
    "github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Strategy detects arbitrage opportunities given a single price update and the
// current cross-exchange snapshot. Implementations are responsible for stamping
// their own Name() into every returned opportunity's Strategy field.
type Strategy interface {
    Name() string
    Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
}
```

Two methods, both required. No optional hooks, no lifecycle callbacks, no error returns. Detect is expected to be allocation-light and panic-free; errors are signalled by returning an empty slice. `now` is passed in (not read from a clock) so strategies remain deterministic in tests.

## 4. SpatialStrategy

```go
package spatial

type Config struct {
    Fees            map[string]FeeConfig
    MinNetProfitPct float64
    MaxPositionUSDT float64
    Staleness       time.Duration
}

type FeeConfig struct {
    TakerFee          float64
    SlippageFactor    float64
    WithdrawalBTC     float64
    NetworkLatencyBps float64
}

type SpatialStrategy struct {
    cfgMu  sync.RWMutex
    cfg    Config
    models map[string]*model.SpreadModel
}

func New(cfg Config, models map[string]*model.SpreadModel) *SpatialStrategy
func (s *SpatialStrategy) Name() string { return "spatial" }
func (s *SpatialStrategy) Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
func (s *SpatialStrategy) SpreadStats() map[string]model.SpreadStats
func (s *SpatialStrategy) SetMinNetProfitPct(v float64)
func (s *SpatialStrategy) SetMaxPositionUSDT(v float64)
func (s *SpatialStrategy) SetStaleness(d time.Duration)
func (s *SpatialStrategy) SetFees(fees map[string]FeeConfig)
```

The spread model map moves wholesale from engine to SpatialStrategy. `SpreadStats()` is the public accessor used by `publishSpreadStats` in main.go; it returns a copy of the stats under read lock, NOT the raw model map, so external callers cannot race with Detect.

Detect is a verbatim move of the current ProcessUpdate inner loop, with three mechanical changes: (a) it reads its own `cfg` under `cfgMu.RLock()` instead of the engine's, (b) it returns `[]types.Opportunity` instead of pushing onto a heap, and (c) every returned opportunity has `Strategy: "spatial"` stamped before the slice append. No decimal arithmetic, ordering, threshold comparison, or scoring formula changes. Byte-equivalence with the pre-refactor behaviour is the primary correctness gate.

## 5. Engine refactor

```go
package engine

type Config struct {
    OpportunityTTL     time.Duration
}

type Engine struct {
    strategies []strategy.Strategy
    snapshotFn func() map[string]types.PriceUpdate
    clock      types.Clock
    cfg        Config
    pq         oppHeap
    latency    *LatencyTracker
    processed  atomic.Uint64
}

func NewEngine(snapshotFn func() map[string]types.PriceUpdate, clock types.Clock, cfg Config, strategies []strategy.Strategy) *Engine

func (e *Engine) ProcessUpdate(u types.PriceUpdate) {
    start := time.Now()
    snapshot := e.snapshotFn()
    now := e.clock.Now()
    for _, s := range e.strategies {
        for _, opp := range s.Detect(u, snapshot, now) {
            heap.Push(&e.pq, &scoredOpportunity{opp: opp})
        }
    }
    e.latency.Record(time.Since(start))
    e.processed.Add(1)
}

func (e *Engine) DequeueTop() (*types.Opportunity, bool)
func (e *Engine) LatencyStats() (p50us, p99us float64, samples int)
func (e *Engine) ProcessedCount() uint64
func (e *Engine) SetClock(clk types.Clock)
```

Note: `StalenessThreshold` is removed from Engine.Config and moved to SpatialStrategy.Config. The staleness check is intrinsic to spatial detection (it gates the cross-exchange compare), not a coordinator concern. The engine no longer owns the concept of "stale" at all.

`DequeueTop` keeps its current TTL logic verbatim (reads `e.cfg.OpportunityTTL`, evicts expired entries until a non-expired one is found). No mutex is needed on `cfg.OpportunityTTL` because nothing mutates it post-construction in this iteration; if hot-tuning becomes a requirement later, we add it then.

Logic-line budget for `engine.go` (excluding type defs, heap methods moved to heap.go, and trivial getters/setters): ProcessUpdate (~10 lines) + DequeueTop (~12 lines) + LatencyStats (~8 lines) + ProcessedCount (~3 lines) + SetClock (~3 lines) + NewEngine (~12 lines) ≈ 48 lines. Comfortably under 60.

## 6. main.go rewiring

Construction order changes: build spatial first, then engine with strategies slice.

```go
spatialFees := make(map[string]spatial.FeeConfig, len(exchangeFees))
for name, f := range exchangeFees {
    spatialFees[name] = spatial.FeeConfig{TakerFee: f.TakerFee, ...}
}
spat := spatial.New(spatial.Config{
    Fees:            spatialFees,
    MinNetProfitPct: cfg.MinNetProfitPct,
    MaxPositionUSDT: cfg.MaxPositionUSDT,
    Staleness:       cfg.StalenessThreshold,
}, spreadModels)

eng := engine.NewEngine(
    agg.Snapshot,
    clk,
    engine.Config{OpportunityTTL: cfg.OpportunityTTL},
    []strategy.Strategy{spat},
)
```

In `patchConfigFn`, setter routing splits along ownership lines:

| Patch field | Old call | New call |
|---|---|---|
| MinNetProfitPct | eng.SetMinNetProfitPct | spat.SetMinNetProfitPct |
| MaxPositionUSDT | eng.SetMaxPositionUSDT | spat.SetMaxPositionUSDT |
| StalenessThresholdMs | eng.SetStalenessThreshold | spat.SetStaleness |
| Fees / DemoMode fee swap | eng.SetFees | spat.SetFees |

The closure captures both `eng` and `spat`. A comment block at the top of patchConfigFn documents the split: "Coordinator-level settings (OpportunityTTL) live on eng. Spatial detection parameters (fees, thresholds, position size, staleness) live on spat. Risk parameters live on rm."

`publishSpreadStats` (the function backing `/api/spreads`) closes over `spat.SpreadStats()` instead of iterating the standalone `spreadModels` map directly. The `spreadModels` local variable still exists (it's the canonical owner of the model objects), but it is passed only to `spatial.New` and never read again from main.

## 7. /api/pnl-by-strategy handler

Mirrors `handlePnLByPair` exactly. Same aggregation shape, same JSON envelope convention, same sort order.

```go
type strategyStats struct {
    Strategy    string  `json:"strategy"`
    TotalPnL    float64 `json:"total_pnl"`
    TradeCount  int     `json:"trade_count"`
    WinRate     float64 `json:"win_rate"`
    TotalVolume float64 `json:"total_volume"`
}

func (h *apiHandler) handlePnLByStrategy(w http.ResponseWriter, r *http.Request) {
    trades := h.store.AllTrades()
    type agg struct { net, vol decimal.Decimal; count, wins int }
    by := make(map[string]*agg)
    for _, t := range trades {
        key := t.Strategy
        if key == "" { key = "unknown" }
        a, ok := by[key]
        if !ok { a = &agg{}; by[key] = a }
        a.net = a.net.Add(t.NetProfit)
        a.vol = a.vol.Add(t.Volume)
        a.count++
        if t.NetProfit.IsPositive() { a.wins++ }
    }
    result := make([]strategyStats, 0, len(by))
    for k, a := range by {
        netF, _ := a.net.Float64()
        volF, _ := a.vol.Float64()
        wr := 0.0
        if a.count > 0 { wr = float64(a.wins) / float64(a.count) }
        result = append(result, strategyStats{Strategy: k, TotalPnL: netF, TradeCount: a.count, WinRate: wr, TotalVolume: volF})
    }
    sort.Slice(result, func(i, j int) bool { return result[i].TotalPnL > result[j].TotalPnL })
    writeJSON(w, http.StatusOK, map[string]interface{}{"strategies": result})
}
```

Route is registered in the existing route setup alongside `/api/pnl-by-pair`.

## 8. Frontend StrategyPnL panel

File: `web/src/components/StrategyPnL.tsx`. Mirrors `PerPairPnL` shape with three rendered columns (Strategy, Total P&L, Trade Count) and a Win Rate cell. Data source: extend the Zustand store to expose `strategyPnL` polled from `/api/pnl-by-strategy` on the same interval as per-pair P&L. The panel is added as a sibling of `PerPairPnL` in `App.tsx`, not embedded inside it.

Empty state copy (English, per project default): "No trades attributed to a strategy yet."

No frontend test is added; the project does not have a frontend test harness for similar panels (PerPairPnL has none).

## 9. Migration order

The migration is staged so that the codebase compiles and tests pass at every intermediate step. Each step is a candidate commit boundary.

1. Add `Strategy string \`json:"strategy,omitempty"\`` to `types.Opportunity` and `types.Trade`. Non-breaking: omitempty preserves backward compat for existing SQLite JSON payloads.
2. Create `internal/strategy/strategy.go` with the Strategy interface. No consumers yet, but the package compiles.
3. Create `internal/strategy/spatial/` with `spatial.go`, `fees.go`. Copy detection logic verbatim from current engine.go. Run the migrated `spatial_test.go` (renamed types from engine_test.go) and confirm green.
4. Extract `internal/engine/heap.go` from `engine.go`. Mechanical move; tests stay green.
5. Refactor `engine.go`: remove FeeConfig, models, computeScore, feeFor, sigmoid, pairKey, fee/profit/position/staleness setters. ProcessUpdate becomes the thin coordinator shown in section 5.
6. Update `engine_test.go`: keep only coordinator concerns (latency, dequeue-TTL, ProcessedCount, multi-strategy fan-out via a fake strategy stub). Detection scenarios already live in spatial_test.go from step 3.
7. Update `cmd/server/main.go`: construct spatial first, then engine with strategies slice, rewire patchConfigFn setters, update publishSpreadStats closure.
8. Update `internal/executor/executor.go`: add `Strategy: opp.Strategy` to the Trade literal around line 171.
9. Add `/api/pnl-by-strategy` handler and route + handler test in `internal/server/`.
10. Add frontend StrategyPnL component + store wiring + App.tsx integration.

Each step ends with `go test ./...` (and `-race` after step 7). Strict TDD applies to apply phase; tests for each unit are written before the implementation in that step.

## 10. ADR-style decisions

### ADR-1: Strategy stamps opp.Strategy itself; engine does not overwrite

Decision: SpatialStrategy.Detect assigns `Strategy: "spatial"` on every Opportunity it returns. The engine treats `opp.Strategy` as opaque and does NOT call `s.Name()` to overwrite it after the fact.

Rationale: Pushing the responsibility to the strategy keeps the interface minimal (two methods, no implicit contracts on what the engine inspects). It also lets future strategies emit different strategy names per opportunity if they ever need to (e.g., a triangular strategy could stamp `"triangular-ab"` vs `"triangular-cyclic"`). The alternative (engine calls `s.Name()` and force-stamps) couples engine to a per-strategy identity assumption.

Validation: We add a debug-build assertion (build tag `engineassert`) that panics if a strategy returns an opportunity with `Strategy == ""`. In production builds the check is a no-op (zero cost). This is acceptable to defer to Day 2 — for now we accept that a misbehaving strategy could produce trades with empty Strategy, which the API handler groups under `"unknown"` (already required by Requirement M7).

Rejected: Engine force-stamp via `s.Name()`. Pros: foolproof. Cons: adds coupling, makes future per-opportunity sub-strategy labels impossible.

### ADR-2: Win rate formula = trades with NetProfit.IsPositive() / total trades

Decision: `wins = count(t where t.NetProfit.IsPositive()); win_rate = wins / total`.

Rationale: This matches the existing `/api/pnl` formula (api.go line ~202-205) and `/api/pnl-by-pair` formula (api.go line ~243-244) exactly. Consistency across all three endpoints is more important than an arguably more correct "wins includes zero-PnL trades" definition. Zero-PnL trades in practice are vanishingly rare for floating-point net profit and represent break-even rather than win, so excluding them from the win bucket is defensible.

Rejected: `NetProfit.GreaterThanOrEqual(decimal.Zero)`. Inconsistent with existing endpoints; not worth diverging.

### ADR-3: SpreadModel ownership moves to SpatialStrategy

Decision: The `map[string]*model.SpreadModel` registry moves from engine to SpatialStrategy as a private field. Public access is via `SpatialStrategy.SpreadStats() map[string]model.SpreadStats`, which returns a copy.

Rationale: Spread models are an artifact of spatial detection (z-score scoring blend). They have no meaning to the engine and no future strategy will share them (triangular needs a different model, funding rate needs none). Moving them keeps the engine free of model lifecycle concerns. `SpreadStats()` returning a copy under read lock prevents external readers (the `/api/spreads` handler) from racing with Detect writes.

Rejected: Keep models on engine, pass to strategies via Detect. Pros: shared if multiple strategies want the same models. Cons: leaks spatial concerns back into the engine; no real-world need for sharing.

### ADR-4: Engine.go logic budget = 60 lines, type defs and heap excluded

Decision: The 60-line budget for `engine.go` excludes (a) type definitions (Engine struct, Config struct), (b) the heap interface methods (moved to `heap.go`), and (c) trivial getters/setters (`SetClock`, `ProcessedCount`). It includes: NewEngine, ProcessUpdate, DequeueTop, LatencyStats.

Rationale: Measuring logic lines is the meaningful signal — "is the coordinator small enough to read in one sitting?" Type definitions and heap boilerplate inflate the count without adding behaviour. The budget is a code-review heuristic, not a CI gate. Estimated actual count post-refactor: ~48 lines, comfortably under budget.

Rejected: Total file LOC budget. Easy to game by moving types to a separate file without simplifying logic.

### ADR-5: Split heap into its own file

Decision: `oppHeap` and `scoredOpportunity` move to `internal/engine/heap.go`.

Rationale: The heap is a mechanical container/heap implementation with five interface methods. Inlining it with coordinator logic obscures the actual logic and makes the 60-line budget harder to keep honest. Splitting it costs zero compile time and zero runtime.

Rejected: Keep inline. Saves one file; obscures logic-to-boilerplate ratio.

## 11. Test strategy

Strict TDD applies during apply phase. Each unit gets a failing test before its implementation:

- `internal/strategy/spatial/spatial_test.go`: takes all 12 detection scenarios from current engine_test.go verbatim, renames constructor calls. Tests covered: TestDetectCorrectBuySellPair, TestNetProfitFormula, TestNetProfitFormula_WithdrawalCost, TestNetProfitFormula_NetworkLatencyCost, TestSubThresholdSpreadDiscarded, TestStaleCounterpartySkipped, TestScoreBlending, TestComputeScore_*, TestSetMinNetProfitPct_*, TestSetFees_*, TestSetMaxPositionUSDT_*.
- `internal/engine/engine_test.go`: shrunk to coordinator scope: TestEngine_LatencyStats_Cold, TestEngine_LatencyStats_Hot, TestEngine_DequeueTopRespectsTTL, TestEngine_ProcessedCount, TestEngine_FanOutAcrossStrategies (new, uses fake strategy stub), TestEngine_EmptyStrategiesNoOpp (new), TestEngine_NoOverwriteStrategyField (new — fake strategy returns opp with Strategy="fake", engine must not mutate).
- `internal/executor/executor_test.go`: add TestExecute_PropagatesStrategy (opp.Strategy="spatial" → trade.Strategy="spatial").
- `internal/server/api_test.go`: add TestAPIPnLByStrategy_*: empty (no trades), single strategy with 3 trades, two strategies sorted desc, legacy trade with empty Strategy bucketed as "unknown".
- Frontend: no test added (consistent with existing panels).
- Full sweep: `go test ./... -race` is the final gate before declaring the apply phase complete.

## 12. Risks and assumptions

- Hot-path performance: one interface dispatch + one slice allocation per ProcessUpdate per strategy. At one strategy and ~1000 updates/sec this is ~1μs/call overhead, negligible. Add a benchmark in `internal/engine/engine_test.go` before Day 2 (triangular strategy) so we have a baseline.
- Migration parity: the byte-equivalence claim depends on the detection logic moving verbatim. The risk is silent drift during the move (e.g., accidentally re-ordering snapshot iteration). Mitigation: side-by-side diff during apply, plus the existing 12 detection tests catch any semantic divergence.
- patchConfigFn cognitive load: the closure now calls setters on two objects (eng and spat) instead of one. Mitigation: explicit comment block at the top documenting ownership. Acceptable for a single-author codebase.
- opp.Strategy enforcement: ADR-1 defers the debug-build assertion to Day 2. The risk is a future strategy author forgetting to stamp Strategy, leading to trades grouped under "unknown" silently. Mitigation: spatial_test.go explicitly asserts opp.Strategy == "spatial" on every returned opportunity, establishing the convention by example.
- Frontend store wiring: adding a polled endpoint to the Zustand store touches App.tsx and the store file. Risk is low because PerPairPnL already follows the exact pattern.
- DB schema: no migration needed. The Strategy field rides on the existing JSON payload column; old rows decode with `Strategy == ""` which the API maps to `"unknown"`. Confirmed by spec Requirement M6 scenario.

## 13. Out of scope (explicit)

- Triangular and funding-rate strategies (Day 2).
- Backtest engine (Day 3).
- Per-strategy risk overrides.
- Engine concurrency changes (still single-goroutine ProcessUpdate).
- Server `FeeInfo` type unification with `spatial.FeeConfig`. The server keeps `FeeInfo`; main.go translates between them, same as today.
- DB schema migration.
