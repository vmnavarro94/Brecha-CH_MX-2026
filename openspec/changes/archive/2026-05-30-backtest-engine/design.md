# Design: backtest-engine

## Executive Summary

Add two new internal packages (`recorder`, `backtest`), one new SQLite database (`frames.db`, WAL), one new table in `trades.db` (`backtest_runs`), five new REST endpoints, and one new React component. The replay path reuses the existing `engine.Engine` via dependency injection (`Clock`, `snapshotFn`), constructing fresh strategy instances per run through factory closures. Concurrency uses `sync.Mutex.TryLock` plus an `atomic.Bool` to satisfy single-run guarantees without blocking status polls.

## Architecture Approach

Hexagonal boundaries preserved. The existing `engine.Engine` already accepts injected `Clock` and `snapshotFn`, so the replay path simply substitutes a `ReplayClock` and `ReplaySnapshot` and reuses the engine unchanged. Strategy state is isolated per run by introducing factory closures around the existing constructors (`spatial.New`, `triangular.New`, `funding.New`). The `FundingStrategy` is wrapped in replay mode by a thin adapter that consumes a pre-recorded `[]FundingRate` slice instead of starting its poller goroutine.

Recording sits on the live processing path as an `atomic.Bool`-gated tap before `eng.ProcessUpdate`. Persistence lives in a dedicated `frames.db` with WAL mode so write contention is isolated from `trades.db` and replay reads can run concurrently with live recording.

## Key Implementation Details

- **internal/recorder/**: Separate WAL SQLite with frames and funding_rates tables, indexed on ts, MaxOpenConns(1).
- **internal/backtest/**: Runner with TryLock guard, ReplayClock/ReplaySnapshot with RWMutex, factories producing fresh strategy instances per run.
- **Metrics**: TotalPnL, MaxDrawdown, HitRate, ProfitFactor, Sharpe (annualized by observed trades-per-year).
- **REST API**: 5 endpoints (POST start, GET status, GET runs, GET results/:id, POST recording).
- **Frontend**: BacktestPanel with controls, history table, metrics display.

## Concurrency Model

- `Runner.mu sync.Mutex` enforces single concurrent run via TryLock; returns ErrAlreadyRunning (HTTP 409) without blocking.
- `Runner.status atomic.Pointer[RunStatus]` allows lock-free reads for GET /status polling.
- `ReplayClock.mu sync.RWMutex` for hot-path Now() reads.
- Recording toggle via `atomic.Bool`.

## Testing Strategy

Strict TDD: ~30 new tests across recorder, clock, snapshot, factories, runner, metrics, handlers. All 151 baseline tests pass.

## Risks & Mitigations

- **Strategy state pollution**: Mitigated by factory pattern producing fresh instances per run.
- **FundingStrategy goroutine leak**: In replay mode, do not call Start(); use pre-recorded rates only.
- **Determinism**: Seed propagation through factory closure ensures byte-identical metrics on identical runs.
- **SpreadModel warmup**: First ~20s yields no spatial signal; document in UI.
