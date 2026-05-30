# Tasks: coding-challenge-mexico

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 900-1200 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1: model+wallet+store, PR 2: engine+risk+executor, PR 3: server (WS+REST), PR 4: frontend |
| Delivery strategy | frequent commits to main, no coauthored attribution |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Pure logic foundations (model, wallet, store) | PR 1 | No I/O deps; fully testable in isolation |
| 2 | Engine + risk + executor (business rules) | PR 2 | Depends on Unit 1 types; mocks wallet/store |
| 3 | WebSocket + REST server | PR 3 | Depends on all backend packages |
| 4 | Frontend (web/) | PR 4 | Depends on WS server contract |

---

## Phase 1: Spread Model (internal/model/) — Unit 1

- [ ] 1.1 Write test file `internal/model/spread_test.go` — cover: first sample init, Welford convergence vs batch, ring buffer eviction at 500, IsReady false at 99 / true at 100, z-score formula, zero-variance guard. TDD: write test first, run `go test ./internal/model/...` (RED), then implement.
  - Package: `internal/model`
  - Files: `internal/model/spread_test.go`
  - Depends on: nothing (pure math)

- [ ] 1.2 Implement `internal/model/spread.go` — SpreadModel with Welford state, ring buffer (cap 500), MinSamples=100, ZScore(), IsReady(), Update(). Run tests GREEN.
  - Package: `internal/model`
  - Files: `internal/model/spread.go`
  - Depends on: 1.1

---

## Phase 2: Wallet Manager (internal/wallet/) — Unit 1

- [ ] 2.1 Write test file `internal/wallet/wallet_test.go` — cover: concurrent 100-goroutine Credit, Debit atomicity on insufficient balance, Debit success updates balance, Balance read. Run with `-race`. TDD: RED first.
  - Package: `internal/wallet`
  - Files: `internal/wallet/wallet_test.go`
  - Depends on: nothing

- [ ] 2.2 Implement `internal/wallet/wallet.go` — Wallet with sync.RWMutex, Credit(), Debit() returning error on insufficient funds, Balance(). Run tests GREEN with `-race`.
  - Package: `internal/wallet`
  - Files: `internal/wallet/wallet.go`
  - Depends on: 2.1

---

## Phase 3: Opportunity Store (internal/store/) — Unit 1

- [ ] 3.1 Write test file `internal/store/store_test.go` — cover: Save + GetByID, QueryByStatus returning correct subset, concurrent 50-goroutine inserts with `-race`. TDD: RED first.
  - Package: `internal/store`
  - Files: `internal/store/store_test.go`
  - Depends on: `internal/types/types.go` (already exists)

- [ ] 3.2 Implement `internal/store/store.go` — in-memory store with sync.RWMutex, Save(Opportunity), SaveTrade(Trade), GetByID(), QueryByStatus(), AllTrades(). Run tests GREEN with `-race`.
  - Package: `internal/store`
  - Files: `internal/store/store.go`
  - Depends on: 3.1

---

## Phase 4: Arbitrage Engine (internal/engine/) — Unit 2

- [ ] 4.1 Write test file `internal/engine/engine_test.go` — cover: O(N) detection emits Opportunity with correct NetProfit formula, sub-threshold spread discarded, Score formula with model ready, Score fallback when model not ready, heap ordering (highest score first), TTL eviction with injected clock. TDD: RED first.
  - Package: `internal/engine`
  - Files: `internal/engine/engine_test.go`
  - Depends on: 1.2 (SpreadModel), internal/types

- [ ] 4.2 Implement `internal/engine/engine.go` — Engine with container/heap priority queue, ProcessUpdate(PriceUpdate), detectOpportunities(), Score(), ticker at ExecutionInterval=100ms, OpportunityTTL=500ms eviction. Run tests GREEN.
  - Package: `internal/engine`
  - Files: `internal/engine/engine.go`
  - Depends on: 4.1

---

## Phase 5: Risk Manager (internal/risk/) — Unit 2

- [ ] 5.1 Write test file `internal/risk/risk_test.go` — cover: NetProfitPct below threshold returns false, at threshold returns true, MaxPositionUSDT exceeded returns false, Active->Watching->Paused transition on 5 consecutive losses, recovery resets counter, Paused blocks all approvals, pause expiry re-activates. Inject clock. TDD: RED first.
  - Package: `internal/risk`
  - Files: `internal/risk/risk_test.go`
  - Depends on: internal/types

- [ ] 5.2 Implement `internal/risk/risk.go` — RiskManager with Evaluate(Opportunity) bool, CircuitBreaker state machine (Active/Watching/Paused), RecordTradeResult(netPct float64), injected clock interface for testability. Run tests GREEN.
  - Package: `internal/risk`
  - Files: `internal/risk/risk.go`
  - Depends on: 5.1

---

## Phase 6: Execution Simulator (internal/executor/) — Unit 2

- [ ] 6.1 Write test file `internal/executor/executor_test.go` — cover: stale price (>2000ms) blocks execution and marks expired, fresh price proceeds, Volume = min(MaxVolume, balance/ask), zero volume aborts with skipped, successful trade debits buy side and credits sell side, insufficient balance caught before debit, Trade record persisted to store. TDD: RED first.
  - Package: `internal/executor`
  - Files: `internal/executor/executor_test.go`
  - Depends on: 2.2 (wallet), 3.2 (store), internal/types

- [ ] 6.2 Implement `internal/executor/executor.go` — Executor with Execute(Opportunity) error, price freshness check via injected clock, volume computation, atomic wallet debit/credit, Trade record creation and store persistence. Run tests GREEN.
  - Package: `internal/executor`
  - Files: `internal/executor/executor.go`
  - Depends on: 6.1

---

## Phase 7: WebSocket + REST Server (internal/server/) — Unit 3

- [ ] 7.1 Write test file `internal/server/ws_test.go` — cover: throttled price_update batches 10 updates into 1 message per 250ms, immediate opportunity event sent in same loop cycle, slow client dropped without blocking fast client. Use mock net.Conn with buffered channels. TDD: RED first.
  - Package: `internal/server`
  - Files: `internal/server/ws_test.go`
  - Depends on: all Phase 1-6 packages

- [ ] 7.2 Implement `internal/server/ws.go` — WebSocket hub with per-client goroutines, throttler (250ms ticker for price_update/spread_stats), immediate broadcast for opportunity/trade/circuit_breaker/pnl events, non-blocking send (drop slow clients).
  - Package: `internal/server`
  - Files: `internal/server/ws.go`
  - Depends on: 7.1

- [ ] 7.3 Write test file `internal/server/api_test.go` — cover all 5 endpoints using httptest.Server: /api/status reflects circuit breaker state, /api/trades pagination offset, /api/opportunities filter by status, /api/pnl shape, /api/spreads shape. TDD: RED first.
  - Package: `internal/server`
  - Files: `internal/server/api_test.go`
  - Depends on: 3.2 (store), 5.2 (risk/CB state)

- [ ] 7.4 Implement `internal/server/api.go` — HTTP handlers for 5 endpoints using net/http, pagination helper, JSON marshaling, wired to store and risk manager.
  - Package: `internal/server`
  - Files: `internal/server/api.go`
  - Depends on: 7.3

- [ ] 7.5 Wire `cmd/server/main.go` — replace stub: instantiate wallet, store, SpreadModel, Engine, RiskManager, Executor, WS hub, HTTP mux; connect feed aggregator output to engine; start all goroutines. No new test (main is integration glue; existing package tests cover behavior).
  - Package: `cmd/server`
  - Files: `cmd/server/main.go` (modify)
  - Depends on: 7.2, 7.4

---

## Phase 8: Frontend (web/) — Unit 4

- [ ] 8.1 Bootstrap frontend: init Vite+React+TypeScript project in `web/`, add Zustand, Recharts, WebSocket client scaffolding. No test yet (scaffolding only).
  - Package: `web/`
  - Files: `web/package.json`, `web/vite.config.ts`, `web/src/main.tsx`
  - Depends on: 7.2 (WS server contract finalized)

- [ ] 8.2 Write test `web/src/hooks/useMarketSocket.test.ts` — cover: auto-reconnect after close with backoff, price_update routes to price store slice only, opportunity routes to opportunity slice only. Mock WebSocket via vi.fn(). TDD: RED first.
  - Package: `web/src/hooks`
  - Files: `web/src/hooks/useMarketSocket.test.ts`
  - Depends on: 8.1

- [ ] 8.3 Implement `web/src/hooks/useMarketSocket.ts` — connect to WS, parse events by type, dispatch to Zustand store slices, reconnect with exponential backoff. Run tests GREEN.
  - Package: `web/src/hooks`
  - Files: `web/src/hooks/useMarketSocket.ts`, `web/src/store/marketStore.ts`
  - Depends on: 8.2

- [ ] 8.4 Write tests `web/src/components/*.test.tsx` — cover: SpreadChart renders ±1σ / ±2σ reference lines given mean+stddev, StatusBar shows red "Circuit Breaker: PAUSED" on paused event, PriceTable renders BBO rows, OpportunityFeed sorts by score, TradeHistory shows P&L per row. TDD: RED first.
  - Package: `web/src/components`
  - Files: `web/src/components/SpreadChart.test.tsx`, `web/src/components/StatusBar.test.tsx`, (+ 3 others)
  - Depends on: 8.3 (store shape known)

- [ ] 8.5 Implement all 6 components: PriceTable, OpportunityFeed, TradeHistory, PnLChart, SpreadChart (with sigma bands), StatusBar (circuit breaker indicator). Run tests GREEN.
  - Package: `web/src/components`
  - Files: `web/src/components/{PriceTable,OpportunityFeed,TradeHistory,PnLChart,SpreadChart,StatusBar}.tsx`
  - Depends on: 8.4

---

## Phase 9: Final Integration Verification

- [ ] 9.1 Run full test suite `go test -race ./...` — all Go packages must pass with zero race conditions.
  - Depends on: all Phase 1-7 tasks

- [ ] 9.2 Run frontend tests `cd web && npm test` — all component and hook tests must pass.
  - Depends on: 8.5

- [ ] 9.3 Smoke-test end-to-end: start server, open browser, verify live price updates, spread chart, and opportunity feed appear without console errors.
  - Depends on: 9.1, 9.2
