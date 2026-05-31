# Tasks: backtest-engine

## Review Workload Forecast

Estimated changed lines: 1,200-1,600. 400-line budget risk: High. Chained PRs recommended.

Work units:
1. Recorder + ReplayClock + ReplaySnapshot (PR1, ~300 lines)
2. Factories + Metrics + Runner (PR2, ~450 lines)
3. Store migration + REST handlers + main.go wiring (PR3, ~350 lines)
4. Frontend BacktestPanel (PR4, ~150 lines)

## Implementation Phases

### Phase 1: Recorder (spec R1-R3)

TDD: `TestRecorder_RecordsAndQueriesFrames`, `TestRecorder_FundingRates`, `TestRecorder_WALAndIndexes`, `TestRecorder_TimeRangeFilter`, `TestRecorder_Close`.

Implement: `Recorder` struct, `New`, `RecordFrame`, `QueryFrames`, `RecordFundingRate`, `QueryFundingRates`, `Close`. Schema: frames table with ts index, funding_rates table with ts index, WAL mode.

### Phase 2: ReplayClock + ReplaySnapshot (spec RR2)

TDD: Concurrent reads/writes under `-race`.

Implement: `ReplayClock` with RWMutex (Set/Now), `ReplaySnapshot` with RWMutex (Update/Snapshot).

### Phase 3: Strategy Factories (spec RR1)

TDD: `TestFactories_FreshSpatialInstancePerCall`, `TestFactories_FreshTriangularInstancePerCall`, `TestFactories_FundingReplayStrategy_ReadsFromInjectedRates`.

Implement: `StrategyFactory` type, `NewSpatialFactory`, `NewTriangularFactory`, `NewFundingReplayFactory` (pre-recorded rates, no goroutine).

### Phase 4: Metrics (spec M1-M5)

TDD: `TestMetrics_TotalPnL`, `TestMetrics_MaxDrawdown_KnownSequence`, `TestMetrics_Sharpe_KnownReturns`, `TestMetrics_HitRate`, `TestMetrics_ProfitFactor`, `TestMetrics_AllStrategiesAggregated`.

Implement: `ComputeMetrics(trades, from, to)` → map[string]StrategyMetrics. Per-strategy: TotalPnL, MaxDrawdown, Sharpe (annualized), HitRate, ProfitFactor.

### Phase 5: Runner (spec RR1-RR5)

TDD: `TestRunner_RejectsConcurrentRuns`, `TestRunner_ProducesTradesFromRecording`, `TestRunner_DeterministicWithSameSeed`, `TestRunner_RespectsSpeedZero`, `TestRunner_StatusProgressUpdates`.

Implement: `Runner` with TryLock, `Run(ctx, spec, factories)` replay loop, `Status()` lock-free via atomic pointer.

### Phase 6: Store — backtest_runs (spec API3-API4)

TDD: `TestStore_SaveBacktestRun`, `TestStore_GetBacktestRun`, `TestStore_ListBacktestRuns`.

Implement: backtest_runs table migration, `SaveBacktestRun`, `GetBacktestRun`, `ListBacktestRuns` (DESC order).

### Phase 7: REST Handlers (spec API1-API5)

TDD: `TestAPI_BacktestStart_202`, `TestAPI_BacktestStart_409`, `TestAPI_BacktestStart_400`, `TestAPI_BacktestStatus_Idle`, `TestAPI_BacktestRuns`, `TestAPI_BacktestResults_404`, `TestAPI_BacktestRecording_Toggle`.

Implement: `handleBacktestStart` (202/409/400), `handleBacktestStatus`, `handleBacktestRuns`, `handleBacktestResults`, `handleBacktestRecording`.

### Phase 8: main.go Wiring (spec NR2, API1-API5)

Extend `apiHandler` with runner, recorder, recordingEnabled. Construct Recorder, Runner, factories. Wire RecordFrame into live loop. Register 5 endpoints. Verify: `go build`, `go test ./...` (184 tests green), `go mod tidy` (no new deps).

### Phase 9: Frontend (spec FP1-FP3)

Create `BacktestPanel.tsx`: datetime inputs (from/to), speed control, strategy checkboxes, seed input, Run button. Implement: POST start → poll status → fetch results → render metrics table. History table: GET /runs, clickable rows. Verify: tsc, npm build.

## Success Criteria

- All 184 tests pass (151 baseline + 33 new)
- Recording 60s produces frames.db with ≥ 1000 rows
- Replay with all 3 strategies returns metrics for each
- Determinism: identical spec + seed → byte-identical metrics
- Speed: 10x speedup on 60s window completes in ≤7s
- Live StrategyPnL unchanged with recording disabled
- No new external dependencies
- Second concurrent POST /start returns 409
