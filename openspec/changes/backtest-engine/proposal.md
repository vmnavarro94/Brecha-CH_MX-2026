# Proposal: backtest-engine

## Intent

Enable the system to record live WebSocket frames plus funding rates into a dedicated store, replay them deterministically at a controllable speed against fresh strategy instances, and produce per-strategy performance metrics (Sharpe ratio, max drawdown, hit rate, total P&L, profit factor, trade count) that the jury can review live in the dashboard and across historical runs.

The outcome is a self-contained backtesting subsystem that proves strategies behave reproducibly under identical inputs, without coupling to live state and without disturbing the running Day 1 live pipeline.

## Scope

### In Scope

- `internal/recorder/` package:
  - `Recorder` struct that opens a separate `frames.db` SQLite file in WAL mode with `ts` indexes
  - `RecordFrame(types.PriceUpdate)` and `RecordFundingRate(exchange string, rate float64, ts time.Time)` writers
  - `QueryFrames(from, to time.Time) []types.PriceUpdate` and `QueryFundingRates(from, to time.Time) []FundingRate` readers
  - `Close() error`
  - Recording toggle wired via env variable + REST endpoint `POST /api/backtest/recording`
- `internal/backtest/` package:
  - `Runner` with a `sync.Mutex` guarding single-run execution (second invocation returns 409)
  - `ReplayClock` implementing the existing `types.Clock` interface with mutable time advanced per frame
  - `ReplaySnapshot` implementing the snapshot contract by tracking last-seen price per exchange during replay
  - `Metrics` computer producing Sharpe (per-trade returns, sqrt-N annualization), max drawdown (equity curve), hit rate (wins/total), total P&L, trade count, profit factor
- Strategy factory functions (`NewFresh(cfg)`) for SpatialStrategy, TriangularStrategy, FundingStrategy producing fully isolated instances with new SpreadModels, refs, ethBtc, rate caches, seeded RNGs
- FundingStrategy replay mode: reads from pre-recorded `funding_rates` rows instead of running its live poller (determinism over simplicity)
- REST endpoints:
  - `POST /api/backtest/start` — body `{from, to, speed, strategies, seed}`
  - `GET /api/backtest/status` — current run progress
  - `GET /api/backtest/runs` — historical runs listing
  - `GET /api/backtest/results/:run_id` — detailed metrics per strategy
  - `POST /api/backtest/recording` — toggle recording on/off
- `backtest_runs(id, started_at, ended_at, from_ts, to_ts, strategies_json, metrics_json, status)` table in the main SQLite for results history
- Frontend `BacktestPanel` component placed below `TradeHistory` in `App.tsx`, with controls (from, to, speed, strategy multi-select) and a metrics table rendering the latest run plus historical runs
- Wire `RecordFrame` into `runProcessingLoop` before `eng.ProcessUpdate` (capture point B from exploration)
- Wire `RecordFundingRate` into the FundingStrategy poller (or a tap goroutine reading the same rate source)

### Out of Scope

- Parallel concurrent backtest runs (single mutex enforces 1-at-a-time, 409 on conflict)
- Step-by-step interactive debugging UI
- Synthetic stress scenarios or "what-if" generated frames
- L2 order book depth replay (BBO only, matching what the live engine consumes)
- Strategy parameter sweeps / grid search
- Distributed or external backtest workers

## Public Contract

### `internal/recorder`

```go
type Recorder struct { /* unexported */ }

func New(dataDir string) (*Recorder, error)
func (r *Recorder) RecordFrame(u types.PriceUpdate) error
func (r *Recorder) RecordFundingRate(exchange string, rate float64, ts time.Time) error
func (r *Recorder) QueryFrames(from, to time.Time) ([]types.PriceUpdate, error)
func (r *Recorder) QueryFundingRates(from, to time.Time) ([]FundingRate, error)
func (r *Recorder) Close() error

type FundingRate struct {
    Exchange string
    Rate     float64
    Ts       time.Time
}
```

### `internal/backtest`

```go
type Runner struct { /* unexported, holds mu sync.Mutex */ }

func New(rec *recorder.Recorder, store *store.Store, factories map[string]StrategyFactory) *Runner
func (r *Runner) Run(ctx context.Context, spec BacktestSpec) (RunResult, error) // returns 409-mapped ErrBusy if already running
func (r *Runner) Status() RunStatus

type BacktestSpec struct {
    From       time.Time
    To         time.Time
    Speed      float64 // 1.0 = real-time, 10.0 = 10x, 0 = max
    Strategies []string
    Seed       int64
}

type RunResult struct {
    RunID   string
    Metrics map[string]StrategyMetrics // keyed by strategy name
}

type StrategyMetrics struct {
    TotalPnL     float64
    Sharpe       float64
    MaxDrawdown  float64
    HitRate      float64
    TradeCount   int
    ProfitFactor float64
}

type RunStatus struct {
    Running     bool
    RunID       string
    Progress    float64 // 0..1
    FramesTotal int
    FramesDone  int
}

type StrategyFactory func(cfg config.Config, seed int64) strategy.StrategyIface
```

### REST shapes

- `POST /api/backtest/start` request: `{"from":"RFC3339","to":"RFC3339","speed":10.0,"strategies":["spatial","triangular","funding"],"seed":42}` → `200 {"run_id":"..."}` or `409 {"error":"backtest already running"}`
- `GET /api/backtest/status` → `{"running":true,"run_id":"...","progress":0.42,"frames_total":4500,"frames_done":1890}`
- `GET /api/backtest/runs` → `[{"id":"...","started_at":"...","ended_at":"...","from":"...","to":"...","strategies":[...],"status":"completed"}, ...]`
- `GET /api/backtest/results/:run_id` → `{"run_id":"...","metrics":{"spatial":{...},"triangular":{...}}}`
- `POST /api/backtest/recording` body `{"enabled":true}` → `{"recording":true}`

## Approach

### Recording

`cmd/server/main.go` constructs a `*recorder.Recorder` if recording is enabled (env or initial config). `runProcessingLoop` calls `rec.RecordFrame(update)` immediately before `eng.ProcessUpdate(update)` so the replay sees exactly the frames the engine consumed. Funding rates are tapped from the FundingStrategy's existing rate-fetch path (or a dedicated lightweight ticker) and written via `RecordFundingRate`. Recording is gated by an atomic flag the `POST /api/backtest/recording` endpoint toggles.

Storage uses a separate `frames.db` file (chosen approach C from exploration) opened with WAL mode and `MaxOpenConns(1)`. Schema:

```sql
CREATE TABLE frames (ts INTEGER, exchange TEXT, bid REAL, ask REAL, last REAL);
CREATE INDEX idx_frames_ts ON frames(ts);
CREATE TABLE funding_rates (ts INTEGER, exchange TEXT, rate REAL);
CREATE INDEX idx_funding_ts ON funding_rates(ts);
```

This isolates write contention from the live `trades.db`, allows concurrent reads during replay, and gives full SQL queryability for time-range slicing.

### Replay

`Runner.Run` flow:

1. Acquire `mu`; if held, return `ErrBusy` (mapped to HTTP 409).
2. Generate `run_id`, insert a `backtest_runs` row with `status='running'`.
3. Open queries: `frames := rec.QueryFrames(spec.From, spec.To)` and `rates := rec.QueryFundingRates(spec.From, spec.To)`.
4. Construct `ReplayClock{current: spec.From}` and `ReplaySnapshot{prices: map[string]types.Price{}}`.
5. Build fresh in-memory `*store.Store` via `NewStore("")` and fresh `*engine.Engine` injected with the replay clock and snapshot.
6. Instantiate strategies through factories with `spec.Seed`. For FundingStrategy, instead of starting its poller, pre-seed via `InjectRates(rates)` (or feed rates inline during replay loop).
7. Iterate `frames` in order:
   - Advance `ReplayClock.current = frame.ReceivedAt`
   - Update `ReplaySnapshot.prices[frame.Exchange] = frame.Price`
   - Call `eng.ProcessUpdate(frame)`
   - For each opportunity emitted, execute via the same dispatch switch used in live (`handleOpportunity`-equivalent) so trades land in the in-memory store with the correct strategy tag
   - If `spec.Speed > 0`, sleep `(frame_dt / spec.Speed)` to throttle; if `spec.Speed == 0`, no sleep (max speed)
   - Update `RunStatus.FramesDone` atomically for `GET /status` polling
8. After loop: query `store.AllTrades()`, group by `Strategy`, compute `StrategyMetrics` per group.
9. Persist `metrics_json` into `backtest_runs`, mark `status='completed'`, release mutex.
10. Return `RunResult{RunID, Metrics}`.

### Metrics

Per strategy:

- `TotalPnL` = sum of trade P&L
- Equity curve = cumulative `TotalPnL` over trades; `MaxDrawdown` = max peak-to-trough decline on that curve
- Per-trade returns array → `Sharpe` = `mean(returns)/stdev(returns) * sqrt(N_annualized)`, where `N_annualized` is extrapolated from observed trades-per-second over the replay window
- `HitRate` = wins / total
- `ProfitFactor` = sum(positive P&L) / abs(sum(negative P&L))
- `TradeCount` = len(trades)

### Frontend

`BacktestPanel` is a new component below `TradeHistory` in `App.tsx`:

- Top row: from/to datetime inputs, speed slider (1, 5, 10, max), strategy multi-select, Start button
- Below: status badge (idle / running / completed) with progress bar polling `GET /api/backtest/status`
- Table of metrics for the latest run keyed by strategy
- Collapsible historical runs list from `GET /api/backtest/runs`, clicking a row loads its results

No coupling to `StrategyPnL` (Day 1) — it keeps showing live data unchanged.

## Risks

Carried from exploration:

- **Strategy state pollution (HIGH)**: live strategy instances must never be shared with backtest. Mitigated by factory pattern producing fresh instances per run.
- **FundingStrategy goroutine leak (HIGH)**: in replay mode we do not call `Start(ctx)` at all; rates come from pre-recorded rows. `Runner.Run` uses `context.WithCancel` defensively so any spawned helpers are cleaned up.
- **Frame volume (LOW)**: documented retention strategy in design phase.
- **SpreadModel warmup (LOW)**: first ~20s of replay produces no spatial signal. Documented in the panel UI.

New risks introduced:

- **Status polling concurrency**: `GET /api/backtest/status` reads progress fields while `Run` writes them. Mitigated with atomic counters or a small read mutex separate from the run mutex so polling never blocks the run loop.
- **Recording write back-pressure (LOW)**: SQLite insert per frame at 50 fps is fine, but a burst could stall the processing loop. Mitigated by batching inserts (e.g., 50 frames per transaction) or a small buffered channel feeding a writer goroutine. Final decision deferred to design phase.
- **Backwards-compatible factory introduction**: existing strategy constructors must remain so live wiring is untouched; we add `NewFresh(cfg, seed)` alongside.

## Success Criteria

- Recording 60s of live data produces `frames.db` with ≥ 1000 frame rows
- Backtest replay of that 60s window with all 3 strategies returns metrics for each strategy
- Two replays of the same recording with the same seed produce byte-identical `metrics_json` (determinism check)
- Replay at `speed=10` completes in ≤ 7s of wall time for 60s of source data
- All 151 baseline tests still pass; ≥ 20 new tests cover recorder, runner, metrics, and factories
- Day 1 `StrategyPnL` panel still shows live data unchanged with recording disabled
- No new external dependencies (use existing `modernc.org/sqlite` and stdlib only)
- Second concurrent `POST /api/backtest/start` returns HTTP 409 with a clear error body
