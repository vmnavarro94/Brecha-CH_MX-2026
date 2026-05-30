# Exploration: multi-strategy-framework

## Current State

`Engine.ProcessUpdate` in `internal/engine/engine.go` fuses 4 concerns in ~80 lines:

1. **Spatial detection** — iterates snapshot counterparties, checks staleness, computes gross spread
2. **Cost model** — `feeFor()` resolves taker + slippage + withdrawal + network-latency bps per leg; subtracts all four from gross
3. **Spread model update** — `sm.Update()` on every pair unconditionally before profit check
4. **Statistical scoring** — `computeScore()` blends `netPct*0.6 + sigmoid(z)*0.4` when Welford model is ready

Engine also owns `FeeConfig`, `Config`, 6 flat setters, `models map[string]*model.SpreadModel`.

Only `cmd/server/main.go` imports `internal/engine`. Coupling is shallow but concentrated — `patchConfigFn` calls `eng.SetFees`/`SetMinNetProfitPct`/`SetMaxPositionUSDT`/`SetStalenessThreshold`, and `runProcessingLoop` holds `*engine.Engine` directly.

## Affected Areas

- `internal/engine/engine.go` — primary refactor; becomes thin coordinator (~30 lines actual logic)
- `internal/engine/engine_test.go` — 12 tests reference `engine.Config`, `engine.FeeConfig`, `engine.NewEngine`; migrate to spatial strategy package
- `internal/types/types.go` — add `Strategy string` field to `Opportunity` and `Trade` (json omitempty for backward compat)
- `cmd/server/main.go` — `patchConfigFn` callers, `spreadStatsFn`, `engineFees` construction — rewire to spatial strategy
- `internal/server/api.go` — `FeeInfo`/`FeeInfoPatch` already exists; future `/api/pnl-by-strategy` additive
- `internal/store/store.go` — SQLite JSON payload; `Strategy` omitempty → old rows deserialise to empty string (zero-risk)
- `internal/executor/executor.go` — propagate `opp.Strategy` into `trade.Strategy`

## Approaches

| # | Description | Pros | Cons | Effort |
|---|---|---|---|---|
| **A** | Pure `Detect(update, snapshot, now) []Opportunity` | Minimal interface, no ctx plumbing, all 12 tests migrate verbatim, easy to extend | main.go calls strategy setters, not engine setters | Medium |
| B | Context-carrying `Detect(DetectCtx, ...)` | Clock+config explicit without mutable strategy state | StrategyConfig = union or `interface{}`, extra alloc per tick, no benefit at this scale | Medium-High |
| C | Stream-based `Run(ctx, in chan, out chan)` | Natural for timer-driven strategies (funding rate) | Fan-in concurrency, TTL complexity, overkill for Day 1 | High |

## Recommendation

**Approach A** with new package `internal/strategy/` + sub-package `internal/strategy/spatial/`.

Engine retains: strategy registry, heap management, latency tracker, `DequeueTop`, `SetClock`. Nothing else.

`FeeConfig`, `Config.MinNetProfitPct`, `Config.MaxPositionUSDT`, spread models, cost calc, scoring → all move to `SpatialStrategy`. `engine.Config` keeps only `OpportunityTTL` + `StalenessThreshold` (coordinator-level).

main.go holds `*spatial.SpatialStrategy` to call typed setters in `patchConfigFn`. No generic `UpdateConfig(json.RawMessage)` — typed setters are simpler and testable.

## Open Questions for Proposal

1. **FeeConfig home**: stays in `internal/strategy/spatial/`, moves to `internal/types/`, or new `internal/cost/`? Server's `FeeInfo` already mirrors it; third parallel definition is a smell.
2. **Spread model ownership**: SpatialStrategy owns its model map and exposes `SpreadStats() map[string]model.SpreadStats`, or main.go keeps the map and passes it in (current pattern, zero churn)?
3. **patchConfigFn wiring**: main.go needs both `*engine.Engine` (for TTL/staleness) and `*spatial.SpatialStrategy` (for fees/profit thresholds). Right split, or does engine retain all setters and delegate?
4. **Strategy field population in executor**: `Execute` receives `*types.Opportunity` which has `opp.Strategy`; trade just gets `trade.Strategy = opp.Strategy`. Confirm no other logic change.
5. **Frontend pnl-by-strategy**: additive new panel or column in PerPairPnL?

## Risks

- **Test migration (HIGH effort, LOW logic risk)**: 12 engine tests reference package-level types that change name. Mechanical migration.
- **SQLite backward compat (LOW)**: Old rows deserialise `Strategy` as empty string. Frontend needs display fallback.
- **patchConfigFn split (MEDIUM cognitive)**: 60-line closure must call setters on two objects (`eng` and `spatial`).
- **Spread model wiring (LOW)**: `publishSpreadStats` reads models map directly. One accessor call change.
- **Hot-path performance (LOW)**: One extra interface dispatch + one slice alloc per ProcessUpdate per strategy. Negligible; worth a benchmark before adding 5+ strategies.

## Ready for Proposal

Yes. Approach is clear, scope is bounded, open questions are concrete and answerable.
