# Delta Spec: multi-strategy-framework

## Capability Map

| Domain | Type |
|---|---|
| `internal/strategy/` — Strategy interface | NEW |
| `internal/strategy/spatial/` — SpatialStrategy | NEW |
| `internal/engine/` — Engine coordinator | MODIFIED |
| `internal/executor/` — Strategy propagation | MODIFIED |
| `internal/types/` — Opportunity and Trade | MODIFIED |
| `internal/server/` — REST API | MODIFIED |
| `web/` — StrategyPnL panel | NEW |

---

## NEW: Strategy Interface (`internal/strategy/`)

### Requirement M1: Strategy interface contract

The system MUST define a `strategy.Strategy` interface with exactly two methods: `Name() string` and `Detect(update, snapshot, now) []types.Opportunity`. Any type implementing this interface MUST be registerable with the engine without modifying engine internals.

#### Scenario: Empty Detect result produces no heap entries

- GIVEN a registered strategy whose Detect always returns `[]types.Opportunity{}`
- WHEN Engine.ProcessUpdate is called
- THEN no opportunity is added to the heap

#### Scenario: Two opportunities returned from Detect reach the heap

- GIVEN a registered strategy whose Detect returns 2 opportunities
- WHEN Engine.ProcessUpdate is called
- THEN both opportunities are present in the heap

#### Scenario: Fan-out across two registered strategies

- GIVEN two strategies registered on the same engine, each returning 1 opportunity
- WHEN Engine.ProcessUpdate is called once
- THEN 2 opportunities are in the heap (one per strategy)

---

## NEW: SpatialStrategy (`internal/strategy/spatial/`)

### Requirement M2: SpatialStrategy preserves byte-equivalent behavior

SpatialStrategy.Detect MUST produce results that are numerically identical to the pre-refactor engine behavior when given the same WS frames and config. No decimal arithmetic, ordering, or threshold comparisons MAY change during the extraction.

#### Scenario: Same input produces same trade

- GIVEN the same PriceUpdate sequence and FeeConfig that produced trade T pre-refactor
- WHEN the same sequence is processed through the refactored stack (SpatialStrategy + thin engine)
- THEN the resulting trade has identical NetProfit, Score, BuyExchange, and SellExchange as T

#### Scenario: Spread model updated regardless of profit

- GIVEN a PriceUpdate where both directions are below MinNetProfitPct
- WHEN Detect is called
- THEN the SpreadModel for that counterparty pair is still updated with the new sample

#### Scenario: Statistical scoring when model is ready

- GIVEN a spread model with IsReady=true, z_score=Z, net_pct=N, max_net_pct=M
- WHEN Score is computed inside Detect
- THEN Score = (N/M)*0.6 + sigmoid(Z)*0.4

### Requirement M3: SpatialStrategy stamps opportunities with strategy name

Every opportunity returned by SpatialStrategy.Detect MUST have `Strategy == "spatial"`. The engine MUST NOT overwrite this value.

#### Scenario: Detect stamps Strategy field

- GIVEN SpatialStrategy.Detect produces one opportunity
- WHEN the opportunity is inspected
- THEN opp.Strategy == "spatial"

### Requirement M5: SpatialStrategy typed setters are race-safe

SpatialStrategy MUST expose `SetMinNetProfitPct(float64)`, `SetFees(map[string]FeeConfig)`, and `SetMaxPositionUSDT(float64)`. Each setter MUST acquire a write lock before mutating internal state. Detect MUST acquire a read lock while reading the same fields.

#### Scenario: SetMinNetProfitPct takes effect on next Detect

- GIVEN SpatialStrategy configured with MinNetProfitPct=0.002
- WHEN SetMinNetProfitPct(0.001) is called and Detect is invoked with a spread that yields net_pct=0.0015
- THEN the opportunity is returned (threshold is now 0.001, not 0.002)

#### Scenario: SetFees takes effect on next Detect

- GIVEN fees configured with taker_fee=0.001
- WHEN SetFees is called with taker_fee=0.002 and Detect runs
- THEN cost calculations use taker_fee=0.002

#### Scenario: Concurrent setter and Detect do not race

- GIVEN SetFees is called from goroutine A while Detect is running in goroutine B
- WHEN both complete
- THEN the Go race detector reports no data race

---

## MODIFIED: Arbitrage Engine (`internal/engine/`)

### Requirement: Engine is a thin coordinator

(Previously: Engine owned FeeConfig, SpreadModels, computeScore, and feeFor.)

The engine MUST hold only: `[]strategy.Strategy`, the opportunity max-heap, the LatencyTracker, `OpportunityTTL`, `StalenessThreshold`, `Clock`, and `snapshotFn`. The engine MUST NOT contain fee-model logic, spread-model logic, or scoring formulas. Engine logic (excluding type definitions, trivial getters/setters, and heap file if extracted) MUST NOT exceed 60 lines.

#### Scenario: Engine has no fee or scoring logic

- GIVEN the engine source file is inspected post-refactor
- WHEN checked for references to FeeConfig, feeFor, computeScore, MinNetProfitPct, MaxPositionUSDT
- THEN none are found

#### Scenario: Latency tracking still captures p50/p99

- GIVEN Engine.ProcessUpdate is called N times
- WHEN Engine.LatencyStats() is called
- THEN p50 and p99 are populated and samples == N (up to ring buffer capacity)

#### Scenario: ProcessedCount increments per call

- GIVEN Engine.ProcessedCount == K
- WHEN ProcessUpdate is called once
- THEN ProcessedCount == K+1

#### Scenario: DequeueTop respects TTL

- GIVEN an opportunity was added with DetectedAt = now - 600ms and OpportunityTTL = 500ms
- WHEN DequeueTop is called
- THEN the opportunity is evicted as expired, not returned as active

---

## MODIFIED: Types (`internal/types/`)

### Requirement M6: Strategy field propagated through Opportunity and Trade

(Previously: Opportunity and Trade had no Strategy field.)

`types.Opportunity` and `types.Trade` MUST each contain `Strategy string \`json:"strategy,omitempty"\``. The executor MUST copy `opp.Strategy` into `trade.Strategy` before persisting.

#### Scenario: Executor propagates strategy name

- GIVEN an opportunity with Strategy="spatial" enters the executor
- WHEN execution completes successfully
- THEN trade.Strategy == "spatial"

#### Scenario: Trade JSON includes strategy field

- GIVEN a trade with Strategy="spatial"
- WHEN serialized to JSON
- THEN the payload contains `"strategy":"spatial"`

#### Scenario: Old trade with no strategy field deserializes cleanly

- GIVEN a SQLite row whose JSON payload has no "strategy" key
- WHEN decoded into types.Trade
- THEN Trade.Strategy == "" with no error

---

## MODIFIED: REST API (`internal/server/`)

### Requirement: Six Endpoints

(Previously: endpoints were GET /api/status, /api/trades, /api/opportunities, /api/pnl, /api/spreads.)

The server MUST expose the following HTTP endpoints:

| Method | Path | Response |
|---|---|---|
| GET | /api/status | system state, circuit breaker state, uptime |
| GET | /api/trades | paginated Trade list |
| GET | /api/opportunities | paginated Opportunity list filterable by status |
| GET | /api/pnl | cumulative and per-trade P&L summary |
| GET | /api/spreads | current spread stats per pair |
| GET | /api/pnl-by-strategy | `[]StrategyPnLRow` aggregated by strategy name |

#### Scenario: /api/status reflects circuit breaker state

- GIVEN the circuit breaker is Paused
- WHEN GET /api/status is called
- THEN the response JSON includes `"circuit_breaker": "paused"`

#### Scenario: /api/trades returns paginated results

- GIVEN 25 trades in the store
- WHEN GET /api/trades?page=2&limit=10
- THEN 10 trades are returned starting from offset 10

#### Scenario: /api/opportunities filters by status

- GIVEN 5 detected and 3 executed opportunities
- WHEN GET /api/opportunities?status=executed
- THEN exactly 3 are returned

### Requirement M7: /api/pnl-by-strategy aggregates by strategy name

The endpoint MUST group all trades by `Trade.Strategy`, sum `NetProfit`, count trades, and return rows sorted by `total_pnl` descending. A trade with `Strategy==""` MUST be grouped under key `"unknown"`.

#### Scenario: Three trades same strategy

- GIVEN 3 trades each with Strategy="spatial" and NetProfit values P1, P2, P3
- WHEN GET /api/pnl-by-strategy is called
- THEN response contains 1 row: `{strategy:"spatial", trade_count:3, total_pnl:P1+P2+P3}`

#### Scenario: Two strategies sorted by total_pnl desc

- GIVEN 2 trades with Strategy="spatial" (sum=10) and 1 trade with Strategy="triangular" (sum=20)
- WHEN GET /api/pnl-by-strategy is called
- THEN response has 2 rows, "triangular" first (total_pnl=20), "spatial" second (total_pnl=10)

#### Scenario: Legacy trade with empty strategy grouped as unknown

- GIVEN 1 trade with Strategy=""
- WHEN GET /api/pnl-by-strategy is called
- THEN response contains 1 row with strategy="unknown"

---

## NEW: StrategyPnL Frontend Panel (`web/src/components/`)

### Requirement M8: StrategyPnL panel renders per-strategy rows

The frontend MUST include a `StrategyPnL` panel that reads from the `/api/pnl-by-strategy` endpoint (or the equivalent Zustand store slice), and renders one row per strategy showing total P&L, trade count, and win rate. The panel MUST be a sibling of the per-pair P&L panel, not a column inside it.

#### Scenario: Panel renders row for existing strategy

- GIVEN the store contains trades with strategy="spatial"
- WHEN StrategyPnL renders
- THEN a row for "spatial" is visible with total P&L and trade count populated

#### Scenario: Empty state message when no trades

- GIVEN the store has zero trades
- WHEN StrategyPnL renders
- THEN an empty-state message is displayed and no rows are rendered

---

## Cross-Cutting: No Regression

### Requirement M10: All existing tests pass after migration

The refactored codebase MUST pass all 109 tests that existed before this change. The 12 engine tests covering spatial detection, scoring, and fees MUST be migrated to `internal/strategy/spatial/spatial_test.go` and continue to pass there.

#### Scenario: Full test suite green

- GIVEN the multi-strategy-framework change is fully applied
- WHEN `go test ./...` is run
- THEN all 109 tests pass with no failures or data races

#### Scenario: engine.go logic budget

- GIVEN the refactored engine.go
- WHEN the file is inspected (excluding type definitions, trivial setters, and a separate heap file)
- THEN the logic line count does not exceed 60

**Test strategy**: `go test ./... -race` after all changes.
