# Exploration: backtest-engine

## Current State

Live pipeline:

1. `feed.Aggregator.drain()` fans out connector channels into `a.out chan types.PriceUpdate` (cap 512). Maintains `prices map` for `Snapshot()`.
2. `runProcessingLoop` selects on `agg.Updates()`. Each update → `eng.ProcessUpdate(update)` → fan-out to all `StrategyIface.Detect()` → opportunities to max-heap.
3. `engine.Engine` already replay-ready: `snapshotFn` and `clock` are injected interfaces.
4. Strategy state per-instance:
   - SpatialStrategy: `map[string]*model.SpreadModel` (500-sample Welford)
   - TriangularStrategy: `refs`, `ethBtc`, `last` + seeded `*rand.Rand`. `SeedRefs()` helper exists.
   - FundingStrategy: background goroutine via `Starter`. `InjectRates()` helper. Seeded RNG.
5. Store: `NewStore("")` already gives in-memory; live uses `dataDir="./data"`. Single `sql.DB` with MaxOpenConns(1).
6. `types.Clock` is one-method interface — any struct with a mutable time field satisfies it.
7. `handlePnLByStrategy` pattern reusable for backtest metrics.

## Affected Areas

- `internal/feed/aggregator.go` — capture point candidate
- `cmd/server/main.go` — recording toggle, backtest lifecycle, runProcessingLoop recorder injection
- `internal/engine/engine.go` — reused as-is
- `internal/store/store.go` — `frames` table or separate DB
- `internal/strategy/*` — need fresh instances per run (factory pattern)
- `internal/server/api.go` — new backtest endpoints
- `config/config.go` — recording toggle + retention

New packages:
- `internal/recorder/` — frame store
- `internal/backtest/` — Runner + Metrics

## Approaches

### Storage Format

| # | Pros | Cons | Effort |
|---|---|---|---|
| A: SQLite same `trades.db` | Single file | Write contention; needs WAL | Low |
| B: Append-only JSONL | Zero schema, gzippable | No time-range index | Low-Med |
| **C: Separate `frames.db` + WAL** | Full SQL queryability, isolated, WAL allows concurrent read | Two DB connections | Low |

**Recommend C.** Replay needs `SELECT WHERE ts BETWEEN ?` — requires index. JSONL can't. Separate file = no contention with live trades. WAL is 2 lines.

### Recording Capture Point

| # | Pros | Cons | Effort |
|---|---|---|---|
| A: `aggregator.drain()` before fan-out | Max fidelity | Records dropped updates | Low |
| **B: `runProcessingLoop` before `eng.ProcessUpdate`** | Engine-accurate replay | Recorder param in main.go | Low |
| C: Tee channel | Clean separation | Back-pressure if recorder slow | Med |

**Recommend B.** Goal is replay mirrors what engine processed. RecordFn as nullable interface = nil → no-op. No new goroutines.

### Metrics Computation

| # | Pros | Cons | Effort |
|---|---|---|---|
| A: Per-trade returns | Simple | Bursty Sharpe, non-standard | Low |
| B: Per-minute bucketed | Standard | Empty buckets inflate std | Med |
| **C: Equity-curve for drawdown + total, per-trade for Sharpe/hit** | Industry standard, 4 meaningful numbers | None | Low-Med |

**Recommend C.** Max drawdown on cumulative equity = industry standard. Sharpe as `mean/std * sqrt(annualization)` on per-trade returns = simple. Hit rate `wins/total` = trivial.

## Recommendation (Architecture)

`RecorderMiddleware` (simple `func(types.PriceUpdate)`) wired into `runProcessingLoop`. When enabled, inserts into `frames.db` (separate SQLite + WAL).

Replay: `recorder.QueryRange(from, to)` → sorted slice → `ReplayRunner` loops frames, sets `ReplayClock.current = frame.ReceivedAt`, updates `ReplaySnapshot.prices[frame.Exchange]`, calls `eng.ProcessUpdate(frame)`. Fresh strategy instances per run, fresh in-memory `Store("")`, run context cancelled on finish.

## Risks

- **Strategy state pollution (HIGH)**: live strategy instances must NEVER be shared with backtest. Each run = factory-created fresh instances.
- **FundingStrategy goroutine leak (HIGH)**: `Start(ctx)` spawns goroutine. Run's `context.WithCancel` must defer cancel.
- **FundingStrategy replay determinism (MEDIUM)**: background goroutine ticks on wall-clock, not frame time. At max speed, tick count diverges. Decision: pre-record funding rates OR accept qualitative match only.
- **Frame volume (LOW)**: 10 exchanges × ~5 fps = ~50 fps. 24h ≈ 4.3M rows ≈ 200MB. Need retention.
- **SpreadModel warmup (LOW)**: MinSamples=100. 60s × 5fps = 300 per exchange. First ~20s of replay no spatial signal. Document.

## Open Questions for Proposal

1. Pre-record funding rates alongside frames OR accept replay non-determinism for funding strategy?
2. Single concurrent run OR N parallel runs (multiple backtests at once)?
3. Backtest results: persist to SQLite or in-memory only?
4. UI: separate panel or integrate into Tweaks?

## Ready for Proposal

Yes. Architecture clear, 2 open decisions plus UI placement.
