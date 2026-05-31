# Verify Report: backtest-engine

Date: 2026-05-30
Branch: feat/funding-rate-arbitrage
Verdict: BLOCKED on 2 CRITICAL findings. Implementation is technically sound for the recorder, replay primitives, metrics, and REST surface, but the live POST /api/backtest/start endpoint cannot satisfy RR1+RR5 because factories are hardcoded to nil at the handler boundary, and new external Go modules (modernc.org/sqlite + transitive deps) were added without committing the corresponding go.mod/go.sum updates.

## 1. Spec compliance

### Capability: recorder

| Req | Status | Evidence |
|-----|--------|----------|
| R1 Frame persistence | PASS | internal/recorder/recorder.go:107-119 RecordFrame + 128-162 QueryFrames; internal/recorder/recorder_test.go TestRecorder_RecordsAndQueriesFrames, TestRecorder_TimeRangeFilter |
| R2 Funding rate persistence | PASS | recorder.go:122-125 RecordFundingRate + 165-195 QueryFundingRates; recorder_test.go TestRecorder_FundingRates |
| R3 Storage isolation, WAL, indexes | PASS | recorder.go:38 frames.db distinct from trades.db; line 45 `PRAGMA journal_mode=WAL`; lines 93,101 idx_frames_ts and idx_funding_ts; recorder_test.go TestRecorder_WALAndIndexes |

### Capability: replay-runner

| Req | Status | Evidence |
|-----|--------|----------|
| RR1 Fresh strategy instances | PARTIAL | Factories themselves return fresh instances (factories.go:25-64). However the live HTTP path never passes factories to the runner (backtest_handlers.go:61 `StartAsync(ctx, spec, nil, resultCh)`), so no strategies are instantiated in production runs. Unit tests use factories directly and pass. |
| RR2 ReplayClock advances with frames | PASS | runner.go:179 `clk.Set(frame.ReceivedAt)` before ProcessUpdate; clock.go RWMutex; clock_test.go TestReplayClock_RaceSafeSetAndNow |
| RR3 Speed control | PASS | runner.go:183-195 honors Speed>0 with timer and Speed==0 no sleep; runner_test.go TestRunner_RespectsSpeedZero |
| RR4 Single concurrent run guard | PASS | runner.go:79 `r.mu.TryLock()` in StartAsync; line 107 same in Run; runner_test.go TestRunner_RejectsConcurrentRuns |
| RR5 Determinism with same seed | PARTIAL | Triangular factory honors `cfg.Seed = seed` (factories.go:36) and TriangularStrategy.New seeds RNG (triangular.go:72). Determinism is satisfied by unit tests, but in production the handler injects nil factories so RR5 is moot for live runs. |

### Capability: metrics

| Req | Status | Evidence |
|-----|--------|----------|
| M1 TotalPnL | PASS | metrics.go:48-55 sumNetProfit |
| M2 MaxDrawdown | PASS | metrics.go:59-76 peak-to-trough scan |
| M3 Sharpe ratio | PASS | metrics.go:80-115; correctly returns 0 when n<2 or std==0 or replaySec<=0 |
| M4 HitRate | PASS | metrics.go:118-129 |
| M5 Profit factor | PASS | metrics.go:134-151; 0 for empty, 999 when no losers |

### Capability: rest-api

| Req | Status | Evidence |
|-----|--------|----------|
| API1 POST /start | PASS | backtest_handlers.go:27-92; 202 / 400 / 409 / 503 paths covered by 3 RED→GREEN tests |
| API2 GET /status | PASS | backtest_handlers.go:95-107; TestBacktestStatus_Idle |
| API3 GET /runs | PASS | backtest_handlers.go:110-120; TestBacktestRuns_List |
| API4 GET /results/:run_id | PASS | backtest_handlers.go:123-159; TestBacktestResults_404 |
| API5 POST /recording | PASS | backtest_handlers.go:167-185 toggles atomic.Bool; TestBacktestRecording_Toggle |

### Capability: frontend-panel

| Req | Status | Evidence |
|-----|--------|----------|
| FP1 Controls render | PASS | BacktestPanel.tsx:341-409 datetime-local from/to, number speed, number seed, 3 checkboxes, Run button |
| FP2 Run history table | PASS | BacktestPanel.tsx:460-495 polls /api/backtest/runs every 5s |
| FP3 Results display per strategy | PASS | BacktestPanel.tsx:429-457 renders TotalPnL, Sharpe, MaxDrawdown, HitRate, TradeCount per strategy |

### Capability: no-regression

| Req | Status | Evidence |
|-----|--------|----------|
| NR1 Baseline + new tests | PASS | `go test ./... -race` reports 183 passing across 20 packages; ≥32 new tests added |
| NR2 Live engine unchanged | PASS | runProcessingLoop (main.go:441-574) only adds a recording tap guarded by `recordingEnabled.Load()`; engine wiring, strategy startup, and execution paths unchanged |
| NR3 No new external deps | FAIL | working tree has uncommitted go.mod additions: `modernc.org/sqlite v1.51.0` and 7 transitive indirects (dustin/go-humanize, mattn/go-isatty, ncruces/go-strftime, remyoudompheng/bigfft, golang.org/x/sys, modernc.org/libc, modernc.org/mathutil, modernc.org/memory). main never had these. Spec NR3 disallows new third-party modules — the recorder was supposed to use the existing vendored modernc.org/sqlite. Build succeeds only because vendor/ already has the packages. |

## 2. Anti-regression deep checks

| Check | Verdict | Evidence |
|-------|---------|----------|
| 1. Factories injection from spec.Strategies | FAIL | backtest_handlers.go:61 hardcodes `nil` factories. cmd/server/main.go:368-406 builds configs (spatialCfg, triCfg, fundCfg) and discards them with `_ = ...` assignments. Comments at lines 396-404 explicitly acknowledge factories are not wired. Effect: live runs produce an empty trade set and empty metrics map. RR1, RR5, and the user-visible value proposition of the panel are broken in production. |
| 2. WAL mode in recorder.New | PASS | recorder.go:45 `db.Exec("PRAGMA journal_mode=WAL")` runs before migrate; JournalMode() helper used by TestRecorder_WALAndIndexes confirms `wal`. |
| 3. Concurrent run guard uses TryLock | PASS | runner.go:79 (StartAsync) and runner.go:107 (Run) both call `r.mu.TryLock()` and return ErrAlreadyRunning on failure, never blocking. |
| 4. Strategy state isolation | PASS | NewSpatialFactory passes nil SpreadModel map → spatial.New allocates fresh internal maps. NewTriangularFactory copies cfg into local `c` per call and constructs a new TriangularStrategy. NewFundingReplayFactory pre-indexes rates once but each invocation returns a fresh `*fundingReplayStrategy` with its own mu, lastEmit map, and a read-only reference to the indexed rates. |
| 5. Determinism with seed | PASS at unit-test layer, MOOT in production | triangular.go:72 `rng: rand.New(rand.NewSource(cfg.Seed))`; factories.go:36 sets `c.Seed = seed` before construction. Verified by TestRunner_DeterministicWithSameSeed. Becomes irrelevant in the live handler because nil factories produce zero strategies. |

## 3. Test results

- Backend: `go test ./... -race -count=1` → all packages PASS (183 tests across 20 packages).
- Frontend types: `npx tsc --noEmit` → no errors.
- Frontend build: `npm run build` → 1626 modules transformed; dist/index-*.js 220.59 kB, css 9.37 kB.
- `go build ./...` succeeds (build artifacts resolve through vendor/).
- Git history: 9 commits on `feat/funding-rate-arbitrage` since main matches the apply-progress log (fdb56bc..e826c2c).

## 4. Findings

### CRITICAL

- C1. backtest_handlers.go:61 passes `nil` factories to `Runner.StartAsync`. The runner then loops over an empty factories slice (runner.go:139-142), so no strategies are constructed for the run. With no strategies, the engine receives no Detect call paths and the temp store finishes empty, so `ComputeMetrics` returns an empty map. The API will return 200 with `metrics: {}` for every successful run. This silently breaks RR1+RR5 at the integration layer and renders the panel useless in production despite all unit tests passing. Fix: build factories from `spec.Strategies` in the handler (or inject a `factoryBuilder func(spec) []StrategyFactory` closure constructed in main.go from spatialCfg/triCfg/fundCfg + recorder.QueryFundingRates).

- C2. NR3 violation. The change introduces `modernc.org/sqlite` and 7 transitive indirect modules to go.mod/go.sum but those edits are NOT committed (still in working tree). Spec NR3 forbids new external Go modules. Either the spec needs to be amended (recorder should use the same SQLite driver already vendored by `internal/store`) or the recorder must reuse the existing driver. Either way the working-tree state is inconsistent with what was committed for PR1, which means a clean checkout on another machine would fail `go build`.

### WARNING

- W1. Spatial executor in replay is a no-op simulation. runner.go:212-214 calls `recordSpatialTrade` which sets GrossProfit/NetProfit to `opp.NetProfit` and Fees to zero — this bypasses the wallet/depth/slippage path used in production. Metrics for spatial in backtests will diverge from live behavior. Acceptable for a v1 but flag for the user.

- W2. Funding rates queried by the runner (runner.go:127-130) are loaded but then discarded at line 166 (`_ = fundingRates`). The intent (per design.md and factories.go:44) was for the handler to construct `NewFundingReplayFactory(cfg, rates)`. Because factories are nil, this dead load wastes a DB scan per run. Tied to C1.

- W3. POST /start handler uses `context.Background()` (backtest_handlers.go:61) so cancellation is never propagated. If the server shuts down mid-replay, the goroutine continues until completion. Low risk but worth a follow-up.

- W4. `BacktestRunRecord.FromTS` and `ToTS` are stored, but ListBacktestRuns/GetBacktestRun return `RFC3339` strings via the default JSON encoder (BacktestPanel.tsx interface treats them as strings). Frontend shows StartedAt via `new Date(...)` which works for ISO timestamps. Behavior matches spec but worth confirming on the live server.

### SUGGESTION

- S1. Add an end-to-end integration test that POSTs a small recording window with `strategies:["triangular"]` and asserts `len(metrics) > 0`. That single test would have caught C1 immediately.

- S2. Move `secondsPerYear` from factories.go:167 to metrics.go where it is actually used; the current placement is surprising.

- S3. Add a `/api/backtest/runs/:run_id/cancel` endpoint or accept ctx cancellation via the HTTP request, so users can abort long real-time replays.

- S4. Recorder.Close ignores errors from prepared-statement closes. Consider returning them.

## Result

- status: blocked
- executive_summary: 2 CRITICAL (handler injects nil factories → empty metrics; uncommitted go.mod with new modernc.org deps violating NR3), 4 WARNING, 4 SUGGESTION; 183/183 tests pass and frontend builds.
- next_recommended: sdd-apply (small slice to fix C1 and resolve C2)
- risks: live POST /start produces empty metrics; clean clone on another machine fails to build until go.mod/go.sum are committed.
