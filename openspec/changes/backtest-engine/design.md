# Design: backtest-engine

## Executive Summary

Add two new internal packages (`recorder`, `backtest`), one new SQLite database (`frames.db`, WAL), one new table in `trades.db` (`backtest_runs`), five new REST endpoints, and one new React component. The replay path reuses the existing `engine.Engine` via dependency injection (`Clock`, `snapshotFn`), constructing fresh strategy instances per run through factory closures. Concurrency uses `sync.Mutex.TryLock` plus an `atomic.Bool` to satisfy single-run guarantees without blocking status polls.

## Architecture Approach

Hexagonal boundaries preserved. The existing `engine.Engine` already accepts injected `Clock` and `snapshotFn`, so the replay path simply substitutes a `ReplayClock` and `ReplaySnapshot` and reuses the engine unchanged. Strategy state is isolated per run by introducing factory closures around the existing constructors (`spatial.New`, `triangular.New`, `funding.New`). The `FundingStrategy` is wrapped in replay mode by a thin adapter that consumes a pre-recorded `[]FundingRate` slice instead of starting its poller goroutine.

Recording sits on the live processing path as an `atomic.Bool`-gated tap before `eng.ProcessUpdate`. Persistence lives in a dedicated `frames.db` with WAL mode so write contention is isolated from `trades.db` and replay reads can run concurrently with live recording.

## Package Layout

```
internal/recorder/
  recorder.go      Recorder struct, RecordFrame, RecordFundingRate,
                   QueryFrames, QueryFundingRates, Close
  recorder_test.go
internal/backtest/
  runner.go        Runner struct, Run(ctx, BacktestSpec), Status
  factories.go     strategy factories: NewSpatialFactory,
                   NewTriangularFactory, NewFundingReplayFactory
  clock.go         ReplayClock (types.Clock impl)
  snapshot.go      ReplaySnapshot (mutable per-exchange BBO)
  metrics.go       Metrics computer (Sharpe, drawdown, hit rate,
                   profit factor, total P&L)
  runner_test.go
  factories_test.go
  metrics_test.go
  clock_test.go
internal/server/
  api.go (modified)            5 new endpoint registrations
  backtest_handlers.go (new)   handlers for backtest endpoints
internal/store/
  store.go (modified)          backtest_runs table migration,
                               SaveBacktestRun, ListBacktestRuns,
                               GetBacktestRun
web/src/components/
  BacktestPanel.tsx            controls + results + history
```

## Recorder Component

### Public API

```go
package recorder

type Recorder struct {
    db *sql.DB
}

type FundingRate struct {
    Exchange string
    Rate     float64
    At       time.Time
}

func New(dataDir string) (*Recorder, error)
func (r *Recorder) RecordFrame(p types.PriceUpdate) error
func (r *Recorder) RecordFundingRate(exchange string, rate float64, at time.Time) error
func (r *Recorder) QueryFrames(from, to time.Time) ([]types.PriceUpdate, error)
func (r *Recorder) QueryFundingRates(from, to time.Time) ([]FundingRate, error)
func (r *Recorder) Close() error
```

### Schema

```sql
CREATE TABLE IF NOT EXISTS frames (
    id INTEGER PRIMARY KEY,
    ts INTEGER NOT NULL,
    exchange TEXT NOT NULL,
    bid TEXT NOT NULL,
    ask TEXT NOT NULL,
    bid_size TEXT NOT NULL,
    ask_size TEXT NOT NULL,
    received_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_frames_ts ON frames(ts);

CREATE TABLE IF NOT EXISTS funding_rates (
    id INTEGER PRIMARY KEY,
    ts INTEGER NOT NULL,
    exchange TEXT NOT NULL,
    rate REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_funding_ts ON funding_rates(ts);
```

`bid`, `ask`, `bid_size`, `ask_size` are stored as TEXT to preserve `decimal.Decimal` precision; `ts` and `received_at` use Unix nano integers for fast range scans on the indexed column.

### Persistence Details

- `frames.db` opened with `PRAGMA journal_mode=WAL` at construction.
- `SetMaxOpenConns(1)` matches the existing pattern in `internal/store/store.go`.
- Naive per-frame `INSERT` with a prepared statement. The expected sustained rate is ~50 fps; at <1ms per insert there is no contention with replay reads under WAL. If profiling later shows contention, batching can be added behind the existing API without affecting callers.

## Backtest Component

### Replay primitives

```go
type ReplayClock struct {
    mu  sync.RWMutex
    now time.Time
}
func (c *ReplayClock) Now() time.Time
func (c *ReplayClock) Set(t time.Time)

type ReplaySnapshot struct {
    mu     sync.RWMutex
    prices map[string]types.PriceUpdate
}
func (s *ReplaySnapshot) Update(p types.PriceUpdate)
func (s *ReplaySnapshot) Snapshot() map[string]types.PriceUpdate
```

`ReplayClock` uses `RWMutex` because `Now()` is called by every strategy on the hot path while `Set()` runs once per frame. `ReplaySnapshot.Snapshot()` returns a defensive copy to satisfy the `engine.snapshotFn` contract.

### Strategy factories

```go
type StrategyFactory func(seed int64) engine.StrategyIface

func NewSpatialFactory(cfg spatial.Config) StrategyFactory
func NewTriangularFactory(cfg triangular.Config) StrategyFactory
func NewFundingReplayFactory(cfg funding.Config, rates []recorder.FundingRate) StrategyFactory
```

Each factory closes over its config and returns a fresh strategy instance per call. `NewSpatialFactory` calls `spatial.New(cfg, nil)` so the `SpreadModel` map starts empty. `NewTriangularFactory` calls `triangular.New(cfg)` and reseeds its RNG with the spec seed. `NewFundingReplayFactory` returns a thin wrapper struct that satisfies `engine.StrategyIface`, holds the pre-recorded rates indexed by exchange, and on each `Detect` call resolves the most recent rate at-or-before `now`. The live `funding.Strategy.Start` goroutine is never invoked.

### Runner

```go
type BacktestSpec struct {
    From       time.Time
    To         time.Time
    Speed      float64
    Strategies []string
    Seed       int64
}

type StrategyMetrics struct {
    TotalPnL     float64
    Sharpe       float64
    MaxDrawdown  float64
    HitRate      float64
    TradeCount   int
    ProfitFactor float64
}

type RunResult struct {
    RunID     string
    Metrics   map[string]StrategyMetrics
    StartedAt time.Time
    EndedAt   time.Time
}

type RunStatus struct {
    State     string  // "idle" | "running" | "done"
    Progress  float64
    CurrentTs int64
    RunID     string
}

type Runner struct {
    mu       sync.Mutex
    status   atomic.Pointer[RunStatus]
    recorder *recorder.Recorder
    store    *store.Store
}

var ErrAlreadyRunning = errors.New("backtest already running")

func New(rec *recorder.Recorder, st *store.Store) *Runner
func (r *Runner) Run(ctx context.Context, spec BacktestSpec, factories []StrategyFactory) (RunResult, error)
func (r *Runner) Status() RunStatus
```

### Run algorithm

1. `if !r.mu.TryLock() { return ErrAlreadyRunning }`; `defer r.mu.Unlock()`.
2. Generate `runID` (UUID), seed initial `RunStatus{State:"running", RunID:runID}`.
3. `frames, err := r.recorder.QueryFrames(spec.From, spec.To)`; same for funding rates.
4. Construct `clock := &ReplayClock{}`, `snap := &ReplaySnapshot{}`.
5. Build strategies by invoking each factory with `spec.Seed`; the funding factory is pre-seeded with the queried rates.
6. `tempStore := store.NewStore("")` (in-memory; no SQLite writes).
7. `eng := engine.NewEngine(snap.Snapshot, clock, engine.Config{OpportunityTTL: 500*time.Millisecond}, strategies)`.
8. Construct fresh `spot`, `funding`, `triangular` executors against `tempStore` and `clock`.
9. Loop over `frames`:
   - `clock.Set(f.ReceivedAt)`
   - `snap.Update(f)`
   - `eng.ProcessUpdate(f)`
   - Drain `eng.DequeueTop()` until empty, dispatching each opportunity by `opp.Strategy` to the matching executor.
   - If `spec.Speed > 0`, sleep `time.Until(next_frame_clock)/spec.Speed`. If `spec.Speed == 0`, no sleep.
   - Update status (`Progress`, `CurrentTs`) via `status.Store(&new)`.
10. Compute `metrics := ComputeMetrics(tempStore.AllTrades(), spec.From, spec.To)`.
11. Persist via `store.SaveBacktestRun(runID, spec, metrics, startedAt, endedAt)`.
12. Return `RunResult`.

The mutex guarantees single concurrent run. `Status()` reads the atomic pointer without contending with `Run`.

## Metrics Component

```go
func ComputeMetrics(trades []types.Trade, replayStart, replayEnd time.Time) map[string]StrategyMetrics
```

Groups trades by `Strategy`, then per group:

- `TotalPnL = sum(NetProfit)` (decimal converted via `.InexactFloat64()`).
- `equityCurve[i] = sum(NetProfit[0..i])`.
- `MaxDrawdown = max(peak - eq[i])` walking left to right.
- `HitRate = wins/total` where win means `NetProfit > 0`. Returns 0 when `total == 0`.
- `ProfitFactor = sum(positive)/abs(sum(negative))`. Returns 999 when no losing trades exist with non-zero losses. Returns 0 when no trades.
- `Sharpe`: returns 0 when `len(trades) < 2`. Otherwise:
  - `returns[i] = NetProfit[i]`
  - `mean = stat.Mean(returns)`, `std = stat.Std(returns)`
  - If `std == 0`, return 0.
  - `elapsedSec = replayEnd.Sub(replayStart).Seconds()`. If `elapsedSec == 0`, return 0.
  - `tradesPerYear = len(trades) * (365*24*3600) / elapsedSec`
  - `Sharpe = mean/std * math.Sqrt(tradesPerYear)`

All math uses stdlib (`math`); a tiny inline `mean`/`std` helper avoids adding `gonum`.

## REST API

```
POST /api/backtest/start
  Body: {from, to, speed, strategies, seed}
  202:  {run_id, status:"accepted"}
  409:  {error:"backtest already running"}
  400:  {error:"invalid spec"}

GET /api/backtest/status
  200:  {state:"running"|"idle"|"done", progress, current_ts, run_id}

GET /api/backtest/runs
  200:  {runs:[{id, started_at, ended_at, status, strategies, metrics_summary},...]}

GET /api/backtest/results/:run_id
  200:  RunResult JSON
  404:  unknown id

POST /api/backtest/recording
  Body: {enabled: bool}
  200:  {recording: bool}
```

Handlers live in `internal/server/backtest_handlers.go` and are wired in `api.go` via the same `apiHandler` pattern used today (struct fields + `mux.HandleFunc`). The 202 status code is chosen because `Run` executes asynchronously: the request returns immediately with a `run_id` and the client polls `GET /status` for progress.

## Store Migration

```sql
CREATE TABLE IF NOT EXISTS backtest_runs (
    id TEXT PRIMARY KEY,
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    from_ts INTEGER NOT NULL,
    to_ts INTEGER NOT NULL,
    strategies_json TEXT NOT NULL,
    metrics_json TEXT,
    status TEXT NOT NULL  -- "running" | "completed" | "failed"
);
CREATE INDEX IF NOT EXISTS idx_backtest_runs_started ON backtest_runs(started_at DESC);
```

Added to the existing `migrate()` in `internal/store/store.go`. New methods: `SaveBacktestRun`, `ListBacktestRuns`, `GetBacktestRun`. Lives in `trades.db` (low write frequency, no contention with frame inserts).

## Frontend Panel

`web/src/components/BacktestPanel.tsx` mounts below `TradeHistory` in `App.tsx`. Controls:

- Two `<input type="datetime-local">` for `from` / `to`.
- One `<input type="number">` for speed (0 = max).
- Three `<input type="checkbox">` for spatial / triangular / funding.
- One `<input type="number">` for seed.
- A `Run` button.

On click: `POST /api/backtest/start` → poll `GET /api/backtest/status` every 500ms → on `state==="done"`, `GET /api/backtest/results/:run_id` → render metrics table. A history table at the bottom polls `GET /api/backtest/runs` every 5s and lets the user click a row to load past results.

No unit tests for `BacktestPanel.tsx`: the existing project pattern keeps presentational components untested at the unit level. Coverage comes from integration via the backend API tests.

## Concurrency Model

- `Runner.mu sync.Mutex` enforces single concurrent run. `TryLock` returns `ErrAlreadyRunning` without blocking.
- `Runner.status atomic.Pointer[RunStatus]` exposes lock-free reads for `GET /status` polling.
- `ReplayClock.mu sync.RWMutex` because `Now()` is on the hot path.
- `Recorder` write path uses the SQLite driver's own connection serialization (`MaxOpenConns(1)`).
- Recording toggle (`atomic.Bool`) is read once per live frame before any DB call.

## Wiring Touches

- `cmd/server/main.go`:
  - Construct `recorder.New(cfg.DataDir)` at startup; defer `rec.Close()`.
  - Inject `recordingEnabled *atomic.Bool` into the processing loop; tap `RecordFrame` before `eng.ProcessUpdate`.
  - Construct `backtest.Runner` and pass it to the API handler.
  - Wire `[]backtest.StrategyFactory` from existing strategy configs.
- `internal/server/api.go`:
  - Extend `apiHandler` with `runner *backtest.Runner`, `recorder *recorder.Recorder`, `recordingEnabled *atomic.Bool`.
  - Register five new endpoints.

## Decisions (ADR-style)

### D1: Separate `frames.db` from `trades.db`

- **Why**: WAL contention isolation. Replay reads frames while live writes append; mixing in `trades.db` would couple unrelated write paths.
- **Rejected**: single DB with table-level separation. Same file means shared WAL and shared lock contention under load.

### D2: Funding rate replay via pre-recorded slice, no goroutine

- **Why**: Determinism is non-negotiable per RR5. A poller goroutine introduces wall-clock dependence and breaks reproducibility.
- **Rejected**: scheduler that replays the original poll cadence. Adds complexity without changing the strategy's observable behaviour, which only depends on rate-at-time.

### D3: `sync.Mutex.TryLock` + `atomic.Pointer[RunStatus]`

- **Why**: `TryLock` satisfies RR4 ("returns immediately without waiting"). Atomic status pointer lets `GET /status` poll at any rate without blocking the run loop.
- **Rejected**: channel-based semaphore. Equivalent semantics but heavier; the existing codebase does not use this pattern.

### D4: 202 Accepted for `POST /api/backtest/start`

- **Why**: The request kicks off async work; the client must poll for progress. 202 is the canonical async-accept code.
- **Rejected**: 200 with synchronous completion. Would block the HTTP handler for the full replay window (up to minutes). 201 implies a single resource is fully created, which is misleading for a long-running process.

### D5: Sharpe annualization derived from replay window

- **Why**: A replay covers an arbitrary window. Using `sqrt(trades_per_year)` where `trades_per_year = trade_count * seconds_per_year / replay_duration_seconds` produces a value comparable to standard convention without baking in a fixed period assumption.
- **Rejected**: fixed `sqrt(252)` (daily). Meaningless when the replay is a 60-second window with hundreds of trades.

### D6: ReplayClock uses `RWMutex`, not `atomic.Pointer[time.Time]`

- **Why**: `Now()` is called by every strategy on every frame; multiple strategies per frame multiplies reads. `RWMutex` allows concurrent reads with low overhead.
- **Rejected**: `atomic.Pointer[time.Time]`. Slightly faster on reads but pointer indirection per read offsets the gain; `RWMutex` is clearer and idiomatic in this codebase.

### D7: Naive per-frame insert for recorder

- **Why**: At ~50 fps and <1ms per insert under WAL, there is no contention. Measure before optimizing.
- **Rejected**: batch transaction every N frames. Adds buffering, complicates shutdown semantics, and saves no time at current throughput.

### D8: Backtest runs history lives in `trades.db`

- **Why**: Rare writes (one row per run), no read/write contention with anything else. Keeps `frames.db` purely a recording sink.
- **Rejected**: third database file. Operational overhead with no benefit.

## Testing Strategy

Strict TDD: write failing tests first, then implementation.

- `recorder_test.go`: `TestRecorder_RecordsAndQueries`, `TestRecorder_FundingRates`, `TestRecorder_TimeRangeFilter`, `TestRecorder_WALModeActive`, `TestRecorder_IndexesExist`, `TestRecorder_Close`.
- `clock_test.go`: `TestReplayClock_Race` (concurrent `Set` + `Now`).
- `factories_test.go`: `TestSpatialFactory_FreshInstancePerCall`, `TestTriangularFactory_FreshInstancePerCall`, `TestFundingReplayFactory_NoGoroutineStarted`, `TestFundingReplayFactory_UsesRecordedRates`.
- `runner_test.go`: `TestRunner_RejectsConcurrentRuns`, `TestRunner_ProducesTradesFromRecording`, `TestRunner_DeterministicWithSameSeed`, `TestRunner_SpeedZeroNoSleep`, `TestRunner_SequentialReRun`.
- `metrics_test.go`: `TestMetrics_TotalPnL`, `TestMetrics_TotalPnL_NoTrades`, `TestMetrics_MaxDrawdown_KnownSequence`, `TestMetrics_MaxDrawdown_MonotonicNoDD`, `TestMetrics_Sharpe_KnownReturns`, `TestMetrics_Sharpe_Insufficient`, `TestMetrics_HitRate`, `TestMetrics_HitRate_NoTrades`, `TestMetrics_ProfitFactor_Normal`, `TestMetrics_ProfitFactor_ZeroLosses`, `TestMetrics_ProfitFactor_NoTrades`.
- `internal/server/backtest_handlers_test.go`: `TestAPI_BacktestStart_202`, `TestAPI_BacktestStart_409_WhenRunning` (uses a channel-blocked fake `Runner` so the second call observes a held mutex), `TestAPI_BacktestStart_400_InvalidSpec`, `TestAPI_BacktestStatus_Idle`, `TestAPI_BacktestRuns_List`, `TestAPI_BacktestResults_404`, `TestAPI_BacktestRecording_Toggle`.

Total new tests: ~30, well above the spec's minimum of 20.

## Risks

- **R1**: `SpreadModel` warmup means the first ~20s of a replay yields no spatial signals. Surface this in the frontend ("warmup period") so users do not interpret zero spatial trades as a bug.
- **R2**: Replay throughput under `Speed=0` may saturate the executor dispatch loop if a single frame produces many opportunities. Mitigation: existing `DequeueTop` already drains lazily; if profiling shows the loop is the bottleneck, batched dispatch can be added without API changes.
- **R3**: Determinism depends on `triangular.New` accepting and using `spec.Seed`. The current constructor seeds its own RNG; the factory must reseed deterministically. Verified against `internal/strategy/triangular/triangular.go` before implementation.
- **R4**: Recording at 50fps for an hour produces ~180k rows. Retention policy is out of scope but should be tracked as follow-up.
- **R5**: `funding.Strategy` is not currently designed for external rate injection. The replay adapter is a new struct that satisfies `engine.StrategyIface` directly, reusing `funding.Config` for fee/threshold parameters but reimplementing the detection step against the injected rate slice. This duplicates logic; if the live funding strategy evolves, the replay adapter must stay in sync. Acceptable trade-off versus polluting the live strategy with replay branching.
