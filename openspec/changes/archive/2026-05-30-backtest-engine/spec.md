# Spec: backtest-engine

## Purpose

Delta spec for the `backtest-engine` change. All capabilities below are NEW — no prior spec exists for this domain. Scenarios are written in Given/When/Then format and MUST be implementable as automated tests.

---

## Capability: recorder

### Requirement R1: Frame persistence

The `Recorder` MUST persist every `PriceUpdate` passed to `RecordFrame` as a row in the `frames` table of `frames.db`.

#### Scenario: Single frame round-trip

- GIVEN a `Recorder` opened against a test data directory
- WHEN `RecordFrame` is called with `PriceUpdate{Exchange:"binance", Bid:50000, Ask:50001, Ts:T}`
- THEN `QueryFrames(T-1s, T+1s)` returns exactly 1 frame with matching Exchange, Bid, Ask, and Ts values

#### Scenario: Ordering guarantee

- GIVEN 100 `RecordFrame` calls with timestamps spread over a 1-hour window
- WHEN `QueryFrames(T-1h, T+1h)` is called
- THEN the returned slice has length 100 and each element's Ts is less than or equal to the next element's Ts

### Requirement R2: Funding rate persistence

The `Recorder` MUST persist every funding rate passed to `RecordFundingRate` as a row in the `funding_rates` table of `frames.db`.

#### Scenario: Single rate round-trip

- GIVEN a `Recorder` opened against a test data directory
- WHEN `RecordFundingRate("binance", 0.0001, T)` is called
- THEN `QueryFundingRates(T-1s, T+1s)` returns exactly 1 entry with Exchange "binance", Rate 0.0001, and Ts T

#### Scenario: Range ordering

- GIVEN multiple `RecordFundingRate` calls with different timestamps
- WHEN `QueryFundingRates(from, to)` is called for a range covering all of them
- THEN the returned slice is sorted by Ts ascending

### Requirement R3: Storage isolation and schema

The `Recorder` MUST open a file named `frames.db` that is separate from the main `trades.db`. The database MUST use WAL journal mode. An index on `frames.ts` and an index on `funding_rates.ts` MUST exist.

#### Scenario: Separate file

- GIVEN a running system with recording enabled
- WHEN the data directory is inspected
- THEN `frames.db` and `trades.db` are distinct files; writes to `frames.db` do not acquire locks on `trades.db`

#### Scenario: WAL mode active

- GIVEN a `Recorder` opened successfully
- WHEN `PRAGMA journal_mode` is queried on the `frames.db` connection
- THEN the result is `wal`

#### Scenario: Index present

- GIVEN a `Recorder` opened successfully
- WHEN the sqlite_master table is queried
- THEN an index on `frames(ts)` and an index on `funding_rates(ts)` are present

---

## Capability: replay-runner

### Requirement RR1: Fresh strategy instances per run

`Runner.Run` MUST construct new strategy instances via the registered factories for each run. No strategy state from a previous run or from the live engine MAY be shared.

#### Scenario: SpatialStrategy isolation

- GIVEN a `Runner` with a SpatialStrategy factory
- WHEN `Run` is called
- THEN SpatialStrategy is instantiated with a new empty `SpreadModel` map; the live SpatialStrategy instance is not referenced

#### Scenario: FundingStrategy replay mode

- GIVEN a `Runner` with a FundingStrategy factory
- WHEN `Run` is called
- THEN the FundingStrategy instance receives pre-recorded rates via injection; its live poller MUST NOT be started

### Requirement RR2: ReplayClock advances with frames

`ReplayClock.Now()` MUST return the `ReceivedAt` timestamp of the frame currently being processed.

#### Scenario: Clock matches frame time

- GIVEN a replay loop processing frames with timestamps T1, T2, T3
- WHEN frame at T1 is processed
- THEN `ReplayClock.Now()` == T1
- AND when frame at T2 is processed, `ReplayClock.Now()` == T2

#### Scenario: Strategies see frame time as now

- GIVEN a strategy that records the clock value it observes when `Process` is called
- WHEN a frame with Ts=T is dispatched through the replay engine
- THEN the strategy observes `now` == T

### Requirement RR3: Speed control

`Runner.Run` MUST throttle playback when `BacktestSpec.Speed > 0` and MUST run at maximum CPU speed when `Speed == 0`.

#### Scenario: Real-time speed

- GIVEN a recording with 10 seconds of source data at `Speed = 1.0`
- WHEN `Run` completes
- THEN elapsed wall time is between 9s and 11s (within 10% tolerance)

#### Scenario: Accelerated speed

- GIVEN a recording with 60 seconds of source data at `Speed = 10.0`
- WHEN `Run` completes
- THEN elapsed wall time is at most 7 seconds

#### Scenario: Maximum speed

- GIVEN a recording with any source data at `Speed = 0`
- WHEN `Run` completes
- THEN no artificial sleep is introduced; the run completes as fast as the CPU allows

### Requirement RR4: Single concurrent run guard

`Runner.Run` MUST return `ErrAlreadyRunning` (mapped to HTTP 409) if a run is already in progress. A second run MUST start cleanly once the first has finished.

#### Scenario: Concurrent rejection

- GIVEN a `Runner` with a run in progress
- WHEN a second `Run` call is made concurrently
- THEN the second call returns `ErrAlreadyRunning` immediately without waiting

#### Scenario: Sequential re-run

- GIVEN a `Runner` where a previous run has completed
- WHEN a new `Run` call is made
- THEN it starts successfully and returns a non-error result

### Requirement RR5: Determinism with same seed

Two `Run` calls with identical `BacktestSpec` (same `From`, `To`, `Seed`, and `Strategies`) against the same recording MUST produce byte-identical `RunResult.Metrics`.

#### Scenario: Reproducible metrics

- GIVEN the same recording window, same `Seed`, and same strategy set
- WHEN `Run` is called twice with the same spec
- THEN `RunResult.Metrics` values are identical across both runs

#### Scenario: FundingStrategy uses recorded rates

- GIVEN a replay run with FundingStrategy enabled
- WHEN the replay loop processes frames
- THEN FundingStrategy reads rate values from the pre-recorded `funding_rates` rows, not from any live network source

---

## Capability: metrics

### Requirement M1: Total P&L per strategy

`StrategyMetrics.TotalPnL` MUST equal the arithmetic sum of `NetProfit` across all trades for that strategy.

#### Scenario: Multiple trades

- GIVEN 3 trades on strategy "spatial" with NetProfit values 5, 3, and -2
- WHEN metrics are computed
- THEN `TotalPnL` == 6.0

#### Scenario: No trades

- GIVEN 0 trades for a strategy
- WHEN metrics are computed
- THEN `TotalPnL` == 0.0

### Requirement M2: Max drawdown on equity curve

`StrategyMetrics.MaxDrawdown` MUST equal the maximum peak-to-trough decline on the cumulative P&L equity curve.

#### Scenario: Drawdown calculation

- GIVEN cumulative P&L sequence [10, 15, 8, 12, 5]
- WHEN metrics are computed
- THEN `MaxDrawdown` == 10.0 (peak 15, trough 5)

#### Scenario: Monotonically increasing equity

- GIVEN a cumulative P&L sequence that is strictly non-decreasing
- WHEN metrics are computed
- THEN `MaxDrawdown` == 0.0

### Requirement M3: Sharpe ratio

`StrategyMetrics.Sharpe` MUST be computed as `mean(returns) / std(returns) * sqrt(annualization_factor)` where `annualization_factor` is derived from observed trades-per-second over the replay window. MUST return 0 when fewer than 2 trades exist.

#### Scenario: Valid Sharpe

- GIVEN 10 trades with per-trade returns r
- WHEN metrics are computed
- THEN `Sharpe == mean(r) / std(r) * sqrt(annualization_factor)` within floating-point tolerance

#### Scenario: Insufficient data

- GIVEN 0 or 1 trade for a strategy
- WHEN metrics are computed
- THEN `Sharpe` == 0.0

### Requirement M4: Hit rate

`StrategyMetrics.HitRate` MUST equal `winning_trades / total_trades`. MUST return 0 when `total_trades` is 0.

#### Scenario: Partial win rate

- GIVEN 4 winning trades and 6 losing trades
- WHEN metrics are computed
- THEN `HitRate` == 0.4

#### Scenario: No trades

- GIVEN 0 trades
- WHEN metrics are computed
- THEN `HitRate` == 0.0

### Requirement M5: Profit factor

`StrategyMetrics.ProfitFactor` MUST equal `sum(positive NetProfit) / abs(sum(negative NetProfit))`. MUST return 999 when there are no losing trades with non-zero losses. MUST return 0 when there are no trades.

#### Scenario: Normal profit factor

- GIVEN trades with sum of positive NetProfit == 15 and sum of negative NetProfit == -5
- WHEN metrics are computed
- THEN `ProfitFactor` == 3.0

#### Scenario: No losing trades

- GIVEN trades where all NetProfit values are positive
- WHEN metrics are computed
- THEN `ProfitFactor` == 999 (capped infinity)

#### Scenario: No trades

- GIVEN 0 trades
- WHEN metrics are computed
- THEN `ProfitFactor` == 0.0

---

## Capability: rest-api

### Requirement API1: POST /api/backtest/start

The endpoint MUST accept a JSON body `{from, to, speed, strategies, seed}` and return 202 with `{run_id}` on success. It MUST return 409 if a run is in progress, and 400 for an invalid spec.

#### Scenario: Successful start

- GIVEN no run is in progress and a valid spec body
- WHEN `POST /api/backtest/start` is called
- THEN response status is 202 and body contains a non-empty `run_id`

#### Scenario: Conflict

- GIVEN a run is currently in progress
- WHEN `POST /api/backtest/start` is called
- THEN response status is 409 and body contains `{"error":"backtest already running"}`

#### Scenario: Invalid spec

- GIVEN a body where `from` is after `to`
- WHEN `POST /api/backtest/start` is called
- THEN response status is 400

### Requirement API2: GET /api/backtest/status

The endpoint MUST return current run state including `state`, `progress` (0..1), and `current_ts` when running. MUST return `{state:"idle"}` when no run is active.

#### Scenario: Run in progress

- GIVEN a run is in progress and 45% of frames have been processed
- WHEN `GET /api/backtest/status` is called
- THEN response contains `state: "running"` and `progress` approximately 0.45

#### Scenario: No active run

- GIVEN no run is in progress
- WHEN `GET /api/backtest/status` is called
- THEN response contains `state: "idle"`

### Requirement API3: GET /api/backtest/runs

The endpoint MUST return all historical run entries sorted by `started_at` descending.

#### Scenario: Multiple runs

- GIVEN 3 completed runs stored in `backtest_runs`
- WHEN `GET /api/backtest/runs` is called
- THEN response contains 3 entries ordered by `started_at` DESC

### Requirement API4: GET /api/backtest/results/:run_id

The endpoint MUST return the `RunResult` with per-strategy metrics for a completed run. MUST return 404 for an unknown run ID.

#### Scenario: Known run

- GIVEN a completed run with `run_id` R
- WHEN `GET /api/backtest/results/R` is called
- THEN response contains `run_id: R` and a `metrics` map with entries for each strategy that was run

#### Scenario: Unknown run

- GIVEN no run with the requested ID exists
- WHEN `GET /api/backtest/results/:run_id` is called
- THEN response status is 404

### Requirement API5: POST /api/backtest/recording

The endpoint MUST toggle frame and funding-rate recording. `{enabled: true}` MUST start recording; `{enabled: false}` MUST stop it.

#### Scenario: Enable recording

- GIVEN recording is disabled
- WHEN `POST /api/backtest/recording` with body `{"enabled": true}` is called
- THEN response contains `{"recording": true}` and subsequent `RecordFrame` calls write to `frames.db`

#### Scenario: Disable recording

- GIVEN recording is enabled
- WHEN `POST /api/backtest/recording` with body `{"enabled": false}` is called
- THEN response contains `{"recording": false}` and subsequent engine updates produce no DB writes

---

## Capability: frontend-panel

### Requirement FP1: BacktestPanel renders controls

`BacktestPanel` MUST render date/time inputs for `from` and `to`, a speed control, strategy checkboxes or multi-select, a seed input, and a "Run" button.

#### Scenario: Control presence

- GIVEN the `BacktestPanel` component is rendered in isolation
- WHEN the component tree is inspected
- THEN date inputs for "from" and "to", a speed control, strategy selectors, a seed input, and a Run button are all present in the DOM

### Requirement FP2: BacktestPanel shows run history

`BacktestPanel` MUST display historical runs returned by `GET /api/backtest/runs` in a table.

#### Scenario: History table

- GIVEN `GET /api/backtest/runs` returns 3 entries
- WHEN `BacktestPanel` fetches and renders the data
- THEN the history table contains 3 rows

### Requirement FP3: Results display per strategy

Selecting a completed run MUST display per-strategy metrics (TotalPnL, Sharpe, MaxDrawdown, HitRate, TradeCount).

#### Scenario: Metrics table populated

- GIVEN a completed run with metrics for strategies "spatial" and "triangular"
- WHEN the user selects that run in the history table
- THEN the panel displays TotalPnL, Sharpe, MaxDrawdown, HitRate, and TradeCount for each strategy

---

## Capability: no-regression

### Requirement NR1: Baseline test suite passes

All 151 pre-existing tests MUST continue to pass after the change is applied. New tests added for backtest capabilities MUST number at least 20.

#### Scenario: Baseline green

- GIVEN the full test suite is run after all changes are applied
- WHEN `go test ./...` completes
- THEN all 151 previously passing tests pass and at least 20 new tests covering recorder, runner, metrics, and factories also pass

### Requirement NR2: Live engine unchanged

The live `StrategyPnL` panel MUST continue to display live spatial, triangular, and funding strategy data when recording is disabled.

#### Scenario: Live PnL unaffected

- GIVEN the system is running with recording disabled
- WHEN the live WebSocket stream is active
- THEN `StrategyPnL` receives updates for all three strategies as before the change

### Requirement NR3: No new external dependencies

No new Go modules outside the current `go.mod` MAY be introduced. Only `modernc.org/sqlite` (already vendored) and stdlib are permitted.

#### Scenario: Dependency check

- GIVEN all new packages are implemented
- WHEN `go mod tidy` and `go mod vendor` are run
- THEN `go.mod` and `vendor/` contain no new third-party modules
