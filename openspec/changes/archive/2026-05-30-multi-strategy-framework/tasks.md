# Tasks: multi-strategy-framework

Branch: `feat/funding-rate-arbitrage`
TDD mode: STRICT — RED first, then GREEN, then commit.
Test runner: `cd /home/vnav/coding-challenge-mexico && go test ./...`
Baseline: 109 tests passing.

---

## Group A — Types extension
_Satisfies: M6 (Strategy field in Opportunity + Trade)_
_Sequential._

**A1** — RED: Write `TestStrategyFieldExists` in a new file `internal/types/types_strategy_test.go`. The test creates an `Opportunity{}` and a `Trade{}`, assigns `.Strategy = "spatial"`, serializes each to JSON, and asserts the payload contains `"strategy":"spatial"`. This test MUST fail to compile because the `Strategy` field does not exist yet.

**A2** — GREEN: Add `Strategy string \`json:"strategy,omitempty"\`` to `types.Opportunity` and `types.Trade` in `internal/types/types.go`. Run `go test ./...` — all 109 + 2 new tests pass (zero-value backward compat: existing tests that construct these structs without `Strategy` are unaffected).

**A3** — Commit: `feat(types): add Strategy field to Opportunity and Trade`

---

## Group B — Strategy interface
_Satisfies: M1 (Strategy interface contract)_
_Sequential after A3. B1+B2 can be written together since interface is small._

**B1** — RED: Write `internal/strategy/strategy_test.go`. Define a local `mockStrategy` struct that has `Name() string` and `Detect(types.PriceUpdate, map[string]types.PriceUpdate, time.Time) []types.Opportunity`. Assert `var _ strategy.Strategy = mockStrategy{}` — compile error because the package does not exist yet.

**B2** — GREEN: Create `internal/strategy/strategy.go` with the exact interface from the design:

```go
package strategy

import (
    "time"
    "github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type Strategy interface {
    Name() string
    Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
}
```

Run `go test ./...` — all tests pass.

**B3** — Commit: `feat(strategy): add Strategy interface`

---

## Group C — SpatialStrategy (new package)
_Satisfies: M2 (byte-equivalent behavior), M3 (stamps opp.Strategy), M5 (race-safe setters)_
_Sequential after B3._

**C1** — Scaffold: Create `internal/strategy/spatial/` directory. Create `internal/strategy/spatial/spatial.go` with the struct, constructor, and `Name()` stub only (no Detect). Create `internal/strategy/spatial/fees.go` with `FeeConfig` type and `feeFor` function (verbatim copy from `engine.go`). Run `go test ./...` — still green (nothing calls Detect yet).

**C2** — RED: Create `internal/strategy/spatial/spatial_test.go`. Migrate ALL 12 detection-scenario tests from `internal/engine/engine_test.go`:
- `TestDetectCorrectBuySellPair`
- `TestNetProfitFormula`
- `TestNetProfitFormula_WithdrawalCost`
- `TestNetProfitFormula_NetworkLatencyCost`
- `TestSubThresholdSpreadDiscarded`
- `TestScoreWithModelReady`
- `TestScoreFallbackModelNotReady`
- `TestStaleExchangeNoOpportunity`

Plus add NEW tests required by spec:
- `TestSpatial_StampsStrategyField` — asserts every opp returned has `Strategy == "spatial"` (M3)
- `TestSpatial_SpreadModelUpdatedBelowThreshold` — below-profit update still updates model (M2 scenario 2)
- `TestSpatial_SetMinNetProfitPct_TakesEffect` — set threshold then detect; marginal spread that was rejected is now accepted (M5 scenario 1)
- `TestSpatial_SetFees_TakesEffect` — change taker_fee; verify cost calculation uses new fee (M5 scenario 2)
- `TestSpatial_Race` — calls `SetFees` and `Detect` concurrently 100 times; pass `-race` to verify no data race (M5 scenario 3)

Adapt constructors to use `spatial.Config` and `spatial.FeeConfig` types instead of `engine.Config`/`engine.FeeConfig`. These tests MUST fail with "undefined: spatial.New" or similar.

**C3** — GREEN: Implement `SpatialStrategy.Detect` in `spatial.go`. This is a VERBATIM move of the detection inner loop from `engine.ProcessUpdate`. Three mechanical changes only:
1. Reads `s.cfg` under `s.cfgMu.RLock()`
2. Returns `[]types.Opportunity` instead of calling `heap.Push`
3. Stamps `Strategy: "spatial"` on each opportunity before appending

Implement `SpreadStats() map[string]model.SpreadStats` — copy models map under RLock, build stats.
Implement typed setters: `SetMinNetProfitPct`, `SetMaxPositionUSDT`, `SetStaleness`, `SetFees` — each acquires `cfgMu.Lock()`.

Run `go test ./internal/strategy/spatial/... -race` — all 13 tests pass, no races.

**C4** — Run `go test ./...` — all 109 + new spatial tests pass. (Engine tests still pass because engine.go is unchanged at this point.)

**C5** — Commit: `feat(strategy/spatial): implement SpatialStrategy with full detection logic`

---

## Group D — Engine thin coordinator
_Satisfies: Engine coordinator requirement (60-line budget, no fee/model logic)_
_Sequential after C5._

**D1** — RED: Add coordinator-only tests to `internal/engine/engine_test.go` (or a new file `engine_coordinator_test.go`). These tests use a `fakeStrategy` stub that implements `strategy.Strategy`:
- `TestEngine_FanOutAcrossStrategies` — two strategies each returning 1 opp; after ProcessUpdate, DequeueTop returns 2 (fan-out across strategies, spec M1 scenario 3)
- `TestEngine_EmptyStrategiesNoOpp` — engine with zero strategies; ProcessUpdate produces nothing in heap (spec M1 scenario 1)
- `TestEngine_MultipleOppsFromOneStrategy` — one strategy returns 2 opps; heap has 2 entries (spec M1 scenario 2)
- `TestEngine_NoOverwriteStrategyField` — strategy stamps opp.Strategy="spatial"; after heap round-trip, opp.Strategy is still "spatial" (ADR-1)

These tests MUST fail because `NewEngine` still takes the old signature.

**D2** — Extract heap: Move `scoredOpportunity` struct and `oppHeap` type with all 5 interface methods from `engine.go` to `internal/engine/heap.go`. Run `go test ./...` — still green (package-internal move).

**D3** — Refactor `engine.go`:
- Remove `FeeConfig` type (now in `spatial/fees.go`)
- Strip `Config` down to `Config{ OpportunityTTL time.Duration }` only
- Remove `models` field from `Engine` struct
- Remove `cfgMu`/`cfg` from Engine (TTL now accessed directly; read DequeueTop carefully — it only needs `OpportunityTTL`, which is immutable after construction per design)
- Remove `computeScore`, `sigmoid`, `pairKey`, `feeFor`
- Remove `SetMinNetProfitPct`, `SetMaxPositionUSDT`, `SetStalenessThreshold`, `SetFees`
- Add `strategies []strategy.Strategy` field
- Change `NewEngine` signature to: `func NewEngine(snapshotFn func() map[string]types.PriceUpdate, clock types.Clock, cfg Config, strategies []strategy.Strategy) *Engine`
- Replace `ProcessUpdate` body with the fan-out loop from the design (≤10 lines)
- `DequeueTop` unchanged except it no longer reads cfg via RLock (TTL is immutable — store directly in cfg, remove mutex)
- Logic line budget: verify ≤60 lines for NewEngine + ProcessUpdate + DequeueTop + LatencyStats + SetClock + ProcessedCount

Run `go test ./internal/engine/... ` — all coordinator tests pass. The 12 detection tests that were in engine_test.go are gone (they now live in spatial_test.go).

**D4** — Verify: `go test ./...` — all tests pass. Spatial tests pass independently. Engine coordinator tests pass.

**D5** — Commit: `refactor(engine): thin coordinator, remove fee/model logic, fan-out to strategies`

---

## Group E — main.go rewiring
_Satisfies: integration wiring after engine and spatial packages are stable_
_Sequential after D5._

**E1** — Update `cmd/server/main.go`:
1. Add import `spatial "github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"` and `"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy"`
2. Build `spatialCfg := spatial.Config{Fees: ..., MinNetProfitPct: ..., MaxPositionUSDT: ..., Staleness: cfg.StalenessThreshold}` — convert `exchangeFees` to `map[string]spatial.FeeConfig` (same fields, different type)
3. Construct `spat := spatial.New(spatialCfg, spreadModels)`
4. Change `engine.NewEngine(...)` call to new signature: `engine.NewEngine(agg.Snapshot, clk, engine.Config{OpportunityTTL: cfg.OpportunityTTL}, []strategy.Strategy{spat})`
5. In `patchConfigFn`: route `SetMinNetProfitPct`, `SetMaxPositionUSDT`, `SetStalenessThreshold` calls to `spat.Set*` instead of `eng.Set*`. Route fee patch to `spat.SetFees` instead of `eng.SetFees`.
6. Change `spreadStatsFn` to close over `spat.SpreadStats()` instead of reading `spreadModels` directly.
7. In `publishSpreadStats`, call `spat.SpreadStats()` instead of iterating `spreadModels` map inline (or update the passed arg).
8. Remove `spreadModels` parameter from `runProcessingLoop` (no longer needed there — only spatial owns it). Update `publishSpreadStats` to accept `func() map[string]model.SpreadStats` instead of the raw map, closing over `spat.SpreadStats`.
9. Remove `engineFees` variable and `engine.FeeConfig` type references.

**E2** — Build check: `go build ./...` — must compile with zero errors.

**E3** — Commit: `refactor(main): wire SpatialStrategy into engine, reroute config setters`

---

## Group F — Executor strategy propagation
_Satisfies: M6 (executor propagates opp.Strategy to trade.Strategy)_
_Sequential after A3, can run in parallel with C, D, E if branch state allows. In practice: after E3._

**F1** — RED: Add `TestExecute_PropagatesStrategy` to `internal/executor/executor_test.go`. Build a test opportunity with `Strategy: "spatial"`. Call `Execute`. Retrieve the saved trade. Assert `trade.Strategy == "spatial"`. Test MUST fail because `Trade` construction in `executor.go` does not set `Strategy`.

**F2** — GREEN: In `internal/executor/executor.go`, at the `trade := types.Trade{...}` literal (line ~171), add `Strategy: opp.Strategy`.

**F3** — Run `go test ./internal/executor/...` — new test passes.

**F4** — Commit: `feat(executor): propagate opp.Strategy to trade.Strategy`

---

## Group G — /api/pnl-by-strategy endpoint
_Satisfies: M7 (pnl-by-strategy aggregation)_
_Sequential after F4 (needs Trade.Strategy field from A2)._

**G1** — RED: Add to `internal/server/api_test.go`:
- `TestAPIPnLByStrategy_Empty` — no trades; response `{"strategies":[]}` with 200
- `TestAPIPnLByStrategy_SingleStrategy` — 3 trades with Strategy="spatial"; assert 1 row, correct sums (spec M7 scenario 1)
- `TestAPIPnLByStrategy_TwoStrategiesSortedDesc` — 2 "spatial" trades (sum=10) + 1 "triangular" trade (sum=20); "triangular" row first (spec M7 scenario 2)
- `TestAPIPnLByStrategy_LegacyEmptyStrategyGroupedAsUnknown` — 1 trade with Strategy=""; row has strategy="unknown" (spec M7 scenario 3)

Tests MUST fail with 404 (route not registered yet).

**G2** — GREEN: Add to `internal/server/api.go`:

```go
type strategyStats struct {
    Strategy    string  `json:"strategy"`
    TotalPnL    float64 `json:"total_pnl"`
    TradeCount  int     `json:"trade_count"`
    WinRate     float64 `json:"win_rate"`
    TotalVolume float64 `json:"total_volume"`
}

func (h *apiHandler) handlePnLByStrategy(w http.ResponseWriter, r *http.Request) { ... }
```

Logic mirrors `handlePnLByPair` exactly: group by `t.Strategy` (bucket `""` as `"unknown"`), sum NetProfit + Volume, count wins (NetProfit.IsPositive()), compute win rate, sort desc by TotalPnL. Register route: `h.mux.HandleFunc("/api/pnl-by-strategy", h.handlePnLByStrategy)` in `NewAPIHandler`.

**G3** — Run `go test ./internal/server/...` — all 4 new tests pass, existing server tests still pass.

**G4** — Commit: `feat(api): add GET /api/pnl-by-strategy endpoint`

---

## Group H — Frontend StrategyPnL panel
_Satisfies: M8 (StrategyPnL panel)_
_Sequential after G4 (needs the endpoint to be defined). Parallel to G if API shape is agreed first._

**H1** — Extend `web/src/store/marketStore.ts`: add `strategyPnL: StrategyPnLRow[]` slice to state, add `fetchStrategyPnL()` action that calls `/api/pnl-by-strategy` and updates the slice. Define `StrategyPnLRow` interface (matches `strategyStats` JSON fields). Add polling on same interval as other fetches.

**H2** — Create `web/src/components/StrategyPnL.tsx`. Mirror `PerPairPnL` component structure: reads from Zustand store slice, renders a table/list with columns "Strategy", "Total PnL", "Trades", "Win Rate". Empty state: `"No trades attributed to a strategy yet."` when `strategyPnL.length === 0`.

**H3** — Wire into `web/src/App.tsx` as a sibling of `PerPairPnL` (not a column inside it, per spec M8).

**H4** — TypeScript + build check: `cd /home/vnav/coding-challenge-mexico/web && npx tsc --noEmit && npm run build` — must pass with zero type errors and zero build errors.

**H5** — Commit: `feat(web): add StrategyPnL panel for per-strategy P&L breakdown`

---

## Group I — Final validation
_Satisfies: M10 (no regression, full suite green)_
_Sequential after H5._

**I1** — Run `go test ./... -race` — all tests pass, zero races. Confirm test count >= 109 + (new tests across spatial, engine, executor, server).

**I2** — Line budget check: count logic lines in `internal/engine/engine.go` (excluding type defs and heap.go content). Must be ≤ 60.

**I3** — Verify `engine.go` contains no references to: `FeeConfig`, `feeFor`, `computeScore`, `MinNetProfitPct`, `MaxPositionUSDT` (satisfies spec scenario "Engine has no fee or scoring logic").

**I4** — Commit (if any fixup needed): `test(integration): final green gate multi-strategy-framework`

---

## Parallelism map

```
A1→A2→A3
         ↓
         B1→B2→B3
                  ↓
                  C1→C2→C3→C4→C5
                                   ↓
                                   D1→D2→D3→D4→D5
                                                   ↓
                                                   E1→E2→E3
                                                            ↓
                                                            F1→F2→F3→F4
                                                                       ↓
                                                                       G1→G2→G3→G4
                                                                                  ↓
                                                                                  H1→H2→H3→H4→H5
                                                                                                 ↓
                                                                                                 I1→I2→I3
```

All groups are strictly sequential because each depends on the compiled package state of the prior group. The only partial parallelism: H (frontend) could start as soon as G4 lands and be developed concurrently in a separate terminal session, but the TypeScript build check at H4 must run after G4 is done (it doesn't link to Go code, just to the agreed JSON shape).

---

## Review Workload Forecast

| Metric | Estimate |
|---|---|
| New files | 5 (strategy.go, spatial.go, fees.go, spatial_test.go, StrategyPnL.tsx) |
| Modified files | 8 (types.go, engine.go, heap.go new, engine_test.go shrunk, executor.go, api.go, main.go, App.tsx + marketStore.ts) |
| Estimated lines changed | ~450–520 |
| New tests added | ~22 (12 migrated spatial + 5 new spatial + 4 new coordinator engine + 1 executor + 4 api) |
| 400-line budget risk | Medium-High |
| Chained PRs recommended | Yes |
| Decision needed before apply | Yes |

**Recommended PR split:**

- **PR 1**: Groups A + B + C (types + interface + SpatialStrategy) — ~200 lines, purely additive, zero breakage risk.
- **PR 2**: Groups D + E (engine refactor + main rewiring) — ~150 lines, breaking change to NewEngine signature; requires PR 1 merged first.
- **PR 3**: Groups F + G + H + I (executor, API endpoint, frontend, final gate) — ~150 lines + frontend.
