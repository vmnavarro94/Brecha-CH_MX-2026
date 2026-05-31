# Tasks: backtest-engine

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1 200 – 1 600 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 (recorder + clock + snapshot) → PR 2 (factories + metrics + runner) → PR 3 (store + REST + main wiring) → PR 4 (frontend) |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Recorder + ReplayClock + ReplaySnapshot | PR 1 | base: feat/funding-rate-arbitrage; ~300 lines; standalone, no engine deps |
| 2 | Factories + Metrics + Runner | PR 2 | base: PR 1 branch; ~450 lines; depends on recorder types |
| 3 | Store migration + REST handlers + main.go wiring | PR 3 | base: PR 2 branch; ~350 lines; depends on Runner |
| 4 | Frontend BacktestPanel | PR 4 | base: PR 3 branch; ~150 lines; TS-only; can target main if API is merged |

---

## Phase 1: Recorder package (spec R1, R2, R3)

- [ ] 1.1 RED — `internal/recorder/recorder_test.go`: write `TestRecorder_RecordsAndQueriesFrames` (single-frame round-trip + ordering of 100 frames); assert result length and Ts ordering.
- [ ] 1.2 Create `internal/recorder/recorder.go`: `Recorder` struct with `*sql.DB`; `New(dataDir string) (*Recorder, error)` — opens `frames.db`, sets WAL, sets `MaxOpenConns(1)`, creates `frames` table with schema `(id, ts, exchange, bid, ask, bid_size, ask_size, received_at)` and `idx_frames_ts`.
- [ ] 1.3 Implement `RecordFrame(p types.PriceUpdate) error` using a prepared INSERT; store decimals as TEXT, ts as Unix nano.
- [ ] 1.4 Implement `QueryFrames(from, to time.Time) ([]types.PriceUpdate, error)` with ORDER BY ts ASC.
- [ ] 1.5 GREEN — `TestRecorder_RecordsAndQueriesFrames` passes.
- [ ] 1.6 RED — `TestRecorder_FundingRates`: single rate round-trip + range ordering.
- [ ] 1.7 Create `funding_rates` table `(id, ts, exchange, rate)` with `idx_funding_ts` in `New`.
- [ ] 1.8 Implement `RecordFundingRate(exchange string, rate float64, at time.Time) error` and `QueryFundingRates(from, to time.Time) ([]FundingRate, error)`. Export `FundingRate` struct.
- [ ] 1.9 GREEN — `TestRecorder_FundingRates` passes.
- [ ] 1.10 RED — `TestRecorder_WALAndIndexes`: query `PRAGMA journal_mode` and `sqlite_master` to assert WAL active and both indexes present (spec R3 schema scenarios).
- [ ] 1.11 GREEN — `TestRecorder_WALAndIndexes` passes.
- [ ] 1.12 RED — `TestRecorder_TimeRangeFilter`: insert frames outside and inside range; assert only in-range frames returned (spec R1 ordering scenario boundary).
- [ ] 1.13 GREEN — `TestRecorder_TimeRangeFilter` passes.
- [ ] 1.14 RED — `TestRecorder_Close`: call `Close`, assert subsequent `RecordFrame` returns error.
- [ ] 1.15 Implement `Close() error` delegating to `r.db.Close()`.
- [ ] 1.16 GREEN — `TestRecorder_Close` passes; run `go test ./internal/recorder/...`.
- [ ] 1.17 Commit: `feat(recorder): WAL SQLite recorder for frames and funding rates`.

## Phase 2: ReplayClock + ReplaySnapshot (spec RR2)

- [ ] 2.1 RED — `internal/backtest/clock_test.go`: `TestReplayClock_RaceSafeSetAndNow` — concurrent goroutines call `Set` and `Now` under `-race`; assert final value equals last `Set`.
- [ ] 2.2 Create `internal/backtest/clock.go`: `ReplayClock` with `sync.RWMutex`; `Set(t time.Time)`, `Now() time.Time`. Must satisfy any clock interface used by `engine`.
- [ ] 2.3 GREEN — `TestReplayClock_RaceSafeSetAndNow` passes with `-race`.
- [ ] 2.4 RED — `internal/backtest/snapshot_test.go`: `TestReplaySnapshot_UpdateAndRead` — write then read back; concurrent reads under `-race`.
- [ ] 2.5 Create `internal/backtest/snapshot.go`: `ReplaySnapshot` with `sync.RWMutex`; `Update(p types.PriceUpdate)`; `Snapshot() map[string]types.PriceUpdate`.
- [ ] 2.6 GREEN — `TestReplaySnapshot_UpdateAndRead` passes with `-race`; run `go test ./internal/backtest/...`.
- [ ] 2.7 Commit: `feat(backtest): ReplayClock and ReplaySnapshot with RWMutex`.

## Phase 3: Strategy factories (spec RR1)

- [ ] 3.1 RED — `internal/backtest/factories_test.go`: `TestFactories_FreshSpatialInstancePerCall` — call factory twice; assert returned instances are distinct pointers; confirm no reference to live strategy.
- [ ] 3.2 Create `internal/backtest/factories.go`: define `StrategyFactory func(seed int64) engine.StrategyIface`; implement `NewSpatialFactory(cfg spatial.Config) StrategyFactory` calling `spatial.New(cfg, nil)`.
- [ ] 3.3 GREEN — `TestFactories_FreshSpatialInstancePerCall` passes.
- [ ] 3.4 RED — `TestFactories_FreshTriangularInstancePerCall`: call factory with same seed twice; assert instances are distinct; assert both produce identical first output (seed determinism, design risk R3).
- [ ] 3.5 Implement `NewTriangularFactory(cfg triangular.Config) StrategyFactory` — propagate `spec.Seed` to `triangular.New`; verify triangular constructor accepts seed; if not, add minimal seed-wiring shim.
- [ ] 3.6 GREEN — `TestFactories_FreshTriangularInstancePerCall` passes; seed determinism confirmed.
- [ ] 3.7 RED — `TestFactories_FundingReplayStrategy_ReadsFromInjectedRates`: inject 3 pre-recorded `FundingRate` entries; call `Detect` at `Now` == rate[1].At; assert rate[1].Rate returned; assert no goroutine started.
- [ ] 3.8 Implement `NewFundingReplayFactory(cfg funding.Config, rates []recorder.FundingRate) StrategyFactory` — thin `StrategyIface` wrapper holding rates indexed by exchange; `Detect` resolves most-recent rate at-or-before `now`; `Start()` is a no-op.
- [ ] 3.9 GREEN — `TestFactories_FundingReplayStrategy_ReadsFromInjectedRates` passes; run `go test ./internal/backtest/...`.
- [ ] 3.10 Commit: `feat(backtest): strategy factories with seed propagation`.

## Phase 4: Metrics (spec M1–M5)

- [ ] 4.1 RED — `internal/backtest/metrics_test.go`: `TestMetrics_TotalPnL_KnownTrades` — 3 trades with NetProfit 5, 3, -2; assert TotalPnL == 6.0. Also assert 0 trades → 0.0 (spec M1).
- [ ] 4.2 Create `internal/backtest/metrics.go`: `sumNetProfit(trades []store.Trade) float64`.
- [ ] 4.3 GREEN — `TestMetrics_TotalPnL_KnownTrades` passes.
- [ ] 4.4 RED — `TestMetrics_MaxDrawdown_KnownSequence` — equity [10,15,8,12,5] → MaxDrawdown == 10.0; monotonic sequence → 0.0 (spec M2).
- [ ] 4.5 Implement `maxDrawdown(trades []store.Trade) float64` walking cumulative equity.
- [ ] 4.6 GREEN — `TestMetrics_MaxDrawdown_KnownSequence` passes.
- [ ] 4.7 RED — `TestMetrics_Sharpe_KnownReturns` — precompute expected `mean/std*sqrt(annFactor)` for a known 10-trade series; assert within 1e-9 tolerance; assert 0 for 0 or 1 trade (spec M3).
- [ ] 4.8 Implement `sharpeRatio(trades []store.Trade, from, to time.Time) float64` with `tradesPerYear = len * 31536000 / replay_sec`.
- [ ] 4.9 GREEN — `TestMetrics_Sharpe_KnownReturns` passes.
- [ ] 4.10 RED — `TestMetrics_HitRate` — 4 wins + 6 losses → 0.4; 0 trades → 0.0 (spec M4).
- [ ] 4.11 Implement `hitRate(trades []store.Trade) float64`.
- [ ] 4.12 GREEN — `TestMetrics_HitRate` passes.
- [ ] 4.13 RED — `TestMetrics_ProfitFactor` — sum(pos)==15, sum(neg)==-5 → 3.0; all-positive → 999; 0 trades → 0.0 (spec M5 all three scenarios).
- [ ] 4.14 Implement `profitFactor(trades []store.Trade) float64` with edge cases.
- [ ] 4.15 GREEN — `TestMetrics_ProfitFactor` passes.
- [ ] 4.16 RED — `TestMetrics_AllStrategiesAggregated` — 2 strategies, 3 trades each; assert `ComputeMetrics` returns a map with 2 keys, each with correct TotalPnL.
- [ ] 4.17 Implement `ComputeMetrics(trades []store.Trade, from, to time.Time) map[string]StrategyMetrics` grouping by `Trade.Strategy`.
- [ ] 4.18 GREEN — `TestMetrics_AllStrategiesAggregated` passes; run `go test ./internal/backtest/...`.
- [ ] 4.19 Commit: `feat(backtest): metrics — TotalPnL, MaxDrawdown, Sharpe, HitRate, ProfitFactor`.

## Phase 5: Runner (spec RR1–RR5)

- [ ] 5.1 RED — `internal/backtest/runner_test.go`: `TestRunner_RejectsConcurrentRuns` — launch `Run` in background goroutine (block with channel), call second `Run`; assert second returns `ErrAlreadyRunning` (spec RR4).
- [ ] 5.2 Create `internal/backtest/runner.go`: `Runner` struct with `sync.Mutex`, `atomic.Pointer[RunStatus]`, `*recorder.Recorder`, `*store.Store`. `ErrAlreadyRunning`. `New(rec, st)`. `Status() RunStatus` (lock-free). `Run(ctx, spec, factories)` skeleton with `mu.TryLock`.
- [ ] 5.3 GREEN — `TestRunner_RejectsConcurrentRuns` passes.
- [ ] 5.4 RED — `TestRunner_ProducesTradesFromRecording` — record 5 frames with known spreads; run with SpatialFactory; assert `RunResult.Metrics["spatial"].TradeCount > 0` OR `RunResult` returned without error (spec RR1 + RR2).
- [ ] 5.5 Implement full replay loop in `Run`: QueryFrames, QueryFundingRates, build `ReplayClock`, `ReplaySnapshot`, instantiate strategies via factories, build `tempStore` (in-memory, empty dataDir), build `engine.Engine`, iterate frames dispatching by `opp.Strategy`, call `ComputeMetrics`, persist via `store.SaveBacktestRun`.
- [ ] 5.6 GREEN — `TestRunner_ProducesTradesFromRecording` passes.
- [ ] 5.7 RED — `TestRunner_DeterministicWithSameSeed` — identical recording + identical spec → identical `Metrics` on two consecutive runs (spec RR5). If flaky, fix triangular seed plumbing (design risk R3).
- [ ] 5.8 GREEN — `TestRunner_DeterministicWithSameSeed` passes deterministically.
- [ ] 5.9 RED — `TestRunner_RespectsSpeedZero` — speed=0, assert no `time.Sleep` call delays (use short recording; wall time < 200ms for 5 frames) (spec RR3 maximum-speed scenario).
- [ ] 5.10 Implement speed control: `if spec.Speed > 0 { time.Sleep(...) }`.
- [ ] 5.11 GREEN — `TestRunner_RespectsSpeedZero` passes.
- [ ] 5.12 RED — `TestRunner_StatusProgressUpdates` — after Run completes, assert `Status().State == "done"` and `Progress == 1.0` (spec RR4 sequential re-run scenario + API2 idle check).
- [ ] 5.13 Implement atomic status updates: set "running" on entry, update Progress per frame, set "done" on completion.
- [ ] 5.14 GREEN — `TestRunner_StatusProgressUpdates` passes; run `go test ./internal/backtest/...`.
- [ ] 5.15 Commit: `feat(backtest): Runner with TryLock guard, replay loop, speed control`.

## Phase 6: Store — backtest_runs (spec API3, API4)

- [ ] 6.1 RED — `internal/store/store_test.go`: `TestStore_SaveBacktestRun` — save a `BacktestRun`; assert no error; assert `GetBacktestRun(id)` returns matching record.
- [ ] 6.2 Add migration in `internal/store/store.go` `migrate()`: `CREATE TABLE IF NOT EXISTS backtest_runs` per design schema; add `idx_backtest_runs_started`.
- [ ] 6.3 Implement `SaveBacktestRun(run BacktestRun) error` — INSERT, serialize `metrics_json` and `strategies_json` via `encoding/json`.
- [ ] 6.4 RED — `TestStore_GetBacktestRun` — save then get; assert fields round-trip cleanly. Also assert 404-style `sql.ErrNoRows` for unknown ID.
- [ ] 6.5 Implement `GetBacktestRun(id string) (BacktestRun, error)`.
- [ ] 6.6 RED — `TestStore_ListBacktestRuns` — insert 3 runs with different `started_at`; assert list returns all 3 in DESC order (spec API3 multiple runs scenario).
- [ ] 6.7 Implement `ListBacktestRuns() ([]BacktestRun, error)` with ORDER BY started_at DESC.
- [ ] 6.8 GREEN — all three Store tests pass; run `go test ./internal/store/...`.
- [ ] 6.9 Commit: `feat(store): backtest_runs table, Save/Get/List`.

## Phase 7: REST API handlers (spec API1–API5)

- [ ] 7.1 RED — `internal/server/backtest_handlers_test.go`: `TestAPI_BacktestStart_202` — fake Runner that returns immediately; POST valid spec; assert 202 + non-empty `run_id` (spec API1 successful-start scenario).
- [ ] 7.2 Create `internal/server/backtest_handlers.go`: `handleBacktestStart` — decode body, validate (`from < to`), call `runner.Run` in goroutine, return 202 + `run_id`.
- [ ] 7.3 RED — `TestAPI_BacktestStart_409` — fake Runner that blocks on a channel; concurrent POST; assert 409 + `{"error":"backtest already running"}` (spec API1 conflict scenario).
- [ ] 7.4 Verify 409 path in `handleBacktestStart`: check `errors.Is(err, backtest.ErrAlreadyRunning)`.
- [ ] 7.5 RED — `TestAPI_BacktestStart_400` — POST body with `from` after `to`; assert 400 (spec API1 invalid-spec scenario).
- [ ] 7.6 Add validation to `handleBacktestStart`.
- [ ] 7.7 RED — `TestAPI_BacktestStatus_Idle` — no run active; GET /status; assert `{"state":"idle"}` (spec API2 no-active-run scenario).
- [ ] 7.8 Implement `handleBacktestStatus` returning `runner.Status()` as JSON.
- [ ] 7.9 RED — `TestAPI_BacktestRuns` — store with 2 saved runs; GET /runs; assert 2 entries in DESC order (spec API3).
- [ ] 7.10 Implement `handleBacktestRuns` calling `store.ListBacktestRuns()`.
- [ ] 7.11 RED — `TestAPI_BacktestResults_404` — GET /results/unknown-id; assert 404 (spec API4 unknown-run scenario).
- [ ] 7.12 Implement `handleBacktestResults` parsing `:run_id`, calling `store.GetBacktestRun`, returning 404 on `sql.ErrNoRows`.
- [ ] 7.13 RED — `TestAPI_BacktestRecording_Toggle` — POST `{enabled:true}` then `{enabled:false}`; assert response `{recording: true}` then `{recording: false}` (spec API5).
- [ ] 7.14 Implement `handleBacktestRecording` toggling `recordingEnabled atomic.Bool`.
- [ ] 7.15 GREEN — all 7 handler tests pass; run `go test ./internal/server/...`.
- [ ] 7.16 Commit: `feat(api): backtest endpoints — start, status, runs, results, recording`.

## Phase 8: main.go wiring + API registration (spec NR2, API1–API5)

- [ ] 8.1 In `internal/server/api.go`: extend `apiHandler` struct with `runner *backtest.Runner`, `recorder *recorder.Recorder`, `recordingEnabled *atomic.Bool`. Register all 5 endpoints in `NewAPIHandler`.
- [ ] 8.2 In `cmd/server/main.go`: construct `recorder.New(cfg.DataDir)`, `defer rec.Close()`. Declare `var recordingEnabled atomic.Bool`.
- [ ] 8.3 Wire `RecordFrame` into `runProcessingLoop` (or equivalent frame dispatch path) gated by `recordingEnabled.Load()`.
- [ ] 8.4 Wire `RecordFundingRate` into `FundingStrategy` poller callback (or tick handler) similarly gated.
- [ ] 8.5 Construct `backtest.Runner` from `rec` and `store`; build `[]backtest.StrategyFactory` from existing strategy configs (spatial, triangular, funding). Pass Runner + recorder + recordingEnabled to `NewAPIHandler`.
- [ ] 8.6 Run `go build ./...` — assert zero errors.
- [ ] 8.7 Run `go test ./...` — assert all 151 baseline tests still pass plus all new tests (spec NR1).
- [ ] 8.8 Run `go mod tidy` — assert no new modules added (spec NR3).
- [ ] 8.9 Commit: `feat(main): wire recorder, runner, and backtest API handler`.

## Phase 9: Frontend BacktestPanel (spec FP1–FP3)

- [ ] 9.1 Extend `web/src/types/api.ts` (or create if absent): add `BacktestSpec`, `RunResult`, `StrategyMetrics`, `BacktestRun` types matching REST payloads.
- [ ] 9.2 Create `web/src/components/BacktestPanel.tsx`: inputs — 2x `datetime-local` (from/to), `number` speed (0=max), 3x strategy checkbox (spatial/triangular/funding), `number` seed, Run button (spec FP1 control-presence scenario).
- [ ] 9.3 Implement Run handler: POST `/api/backtest/start`, poll `/api/backtest/status` every 500ms, on `state=="done"` fetch `/api/backtest/results/:id`, render per-strategy metrics table (TotalPnL, Sharpe, MaxDrawdown, HitRate, TradeCount) (spec FP3).
- [ ] 9.4 Implement history table: poll `/api/backtest/runs` every 5s; render rows; clicking a row fetches and displays its results (spec FP2).
- [ ] 9.5 Add note/tooltip for SpatialStrategy: "~20s warmup before first signal" (design risk R1).
- [ ] 9.6 Wire `BacktestPanel` into `web/src/App.tsx` below `TradeHistory`.
- [ ] 9.7 Run `cd web && npx tsc --noEmit` — assert zero TS errors.
- [ ] 9.8 Run `cd web && npm run build` — assert zero build errors.
- [ ] 9.9 Commit: `feat(ui): BacktestPanel with controls, history table, and metrics display`.
