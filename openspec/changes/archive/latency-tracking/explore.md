# Exploration: latency-tracking

## Current State

Pipeline: Exchange WS → `feed.Aggregator.drain()` (sets `ReceivedAt`) → `agg.out` channel (cap 512) → `runProcessingLoop` select case → `eng.ProcessUpdate(update)` → `heap.Push` → returns.

`ProcessUpdate` is synchronous, called from a single goroutine in `runProcessingLoop`. NOT concurrent today — select loop is single-threaded. Critical: no lock contention risk on the tracker if it lives inside the engine.

Engine uses `types.Clock` (interface). `ProcessUpdate` does NOT currently record a start time — only uses `e.clock.Now()` for `Opportunity.DetectedAt`, AFTER all arithmetic.

**Pipeline timing breakdown:**
- T0 = `ReceivedAt` on `PriceUpdate` — set in `aggregator.drain()` on raw WS frame arrival
- T1 = entry of `eng.ProcessUpdate(update)` — currently unmeasured
- T2 = `e.clock.Now()` used as `DetectedAt` — set after profit/score arithmetic
- T_publish = `hub.Publish(...)` — after ProcessUpdate returns

Gap T1→T2 = "detection latency" for the jury: pure engine computation time (snapshot fetch + O(N) profit math + heap push).
Gap T0→T1 includes channel queuing in `agg.out` (can be nonzero under load).

**WS throttling:** `price_update` and `spread_stats` throttled at 250ms. Others unthrottled. A new `latency_stats` event should be unthrottled or throttled ≥250ms.

**Frontend StatusBar:** Reads from `useMarketStore`: circuitBreakerState, pnl, winRate, tradeCount, wsConnected, prices, execCount. No latency field exists. WS dispatcher in App.tsx. Adding p50/p99 requires: new field in MarketState, action, WS handler, stat block in StatusBar.

**No external percentile library** in go.mod. Only: uuid, gorilla/websocket, shopspring/decimal, modernc/sqlite.

## Affected Areas

- `internal/engine/engine.go` — add latency sampling at ProcessUpdate entry/exit; expose percentile accessor
- `internal/types/types.go` — optionally add LatencyStats struct
- `cmd/server/main.go` — add `publishLatencyStats(hub, eng)` on a ticker
- `internal/server/ws.go` — add `"latency_stats"` to throttledEventTypes
- `web/src/store/marketStore.ts` — add `latency: { p50: number; p99: number }` + action
- `web/src/components/StatusBar.tsx` — add latency stat block
- `web/src/App.tsx` (WS dispatcher) — handle `latency_stats` event

## Approaches

### 1. Ring Buffer + Sort (zero-dependency, pure Go) — **RECOMMENDED**

Fixed-size circular buffer (1024 slots) of `int64` nanoseconds. On each `ProcessUpdate`, record elapsed ns into `index % 1024`. Percentiles: copy slice, sort (1024 ints ≈ 8 µs), read p50=sorted[511], p99=sorted[1013]. Expose via method. Publish on 1s ticker.

- Pros: zero deps; exact percentiles over rolling window; simple to test with fixedClock; no allocs after init; O(N log N) only on reads
- Cons: sort on read O(N log N); window by sample count not wall time; needs mutex if read from different goroutine
- Effort: Low (~50–70 lines Go)

### 2. HDR Histogram

Add `github.com/HdrHistogram/hdrhistogram-go`. O(1) record and read. Reset on interval.

- Pros: O(1); battle-tested; accurate across wide ranges
- Cons: new dep + vendoring; overkill for sub-ms ranges; library does the math (weaker jury signal than bespoke code)
- Effort: Medium

### 3. EMA

Running EMA of latency.

- Pros: trivial; zero overhead
- Cons: NOT a percentile estimator; p99 fabricated — worse than no p99
- Effort: Low — but wrong output

### 4. T-Digest (caio/go-tdigest)

Approximate percentiles, accurate tails.

- Pros: accurate p99 on skewed distributions
- Cons: new dep; marginal vs ring-buffer for 1024 samples
- Effort: Medium-High

## Recommendation

**Approach 1 — Ring Buffer + Sort.**

Rationale: engine loop is single-goroutine, ring write is contention-free. 1024 samples at ~100 updates/s covers ~10s of history. Sort takes ≈4–8µs, negligible vs 1s publish interval. Zero deps keeps vendor clean and demonstrates the candidate wrote the math themselves — stronger jury signal than wrapping hdrhistogram.

**Injection point:** measure from `ProcessUpdate` entry to just before return. Captures: snapshot copy + O(N) profit loop + heap push. Excludes channel queuing (T0→T1) which is infrastructure noise, not engine intelligence.

**Exposure:** 1s ticker in `runProcessingLoop` calls `eng.LatencyStats()` and publishes `latency_stats`. Add `"latency_stats"` to `throttledEventTypes`. Frontend displays `detect p50: X.Xµs | p99: Y.Yµs` in StatusBar.

**Sync note:** `ProcessUpdate` and 1s ticker run in same `runProcessingLoop` goroutine — NO mutex needed. If REST endpoint later exposes latency, add `RWMutex` at that point.

## Risks

- **Sample starvation:** illiquid pairs → fewer samples → p99 reflects old samples. Mitigate: only report when count ≥ 10.
- **Clock resolution:** Linux `time.Since()` has ~1µs resolution. Sub-µs measurements noisy. Acceptable — jury cares about order of magnitude.
- **Throttle interaction:** 250ms throttle = 4 updates/s in frontend, smooth + low bandwidth. Unthrottled = 1/s server rate.
- **Heap push:** ProcessUpdate writes `e.pq` without mutex (single-goroutine). Ring buffer write same — no new contention.
- **p99 accuracy:** at 100 upd/s, 1024 ring = ~10s window. p99 = sample #1013 = 1013th worst in last ~10s. Statistically valid.

## Open Questions for Proposal Phase

1. Include T0→T1 channel-queuing delay? Current recommendation: engine-only (T1→T2).
2. Ring buffer size: 1024 (~10s) vs 512 (~5s) vs 256 (~2.5s)?
3. Display granularity: µs or ms? Expected 10–500µs in demo mode.
4. Persist p50/p99 to SQLite for trend charting, or live-only?
5. REST `GET /api/latency` for polling clients (future), or WS-only for now?

## Ready for Proposal

Yes. Architecture clear, recommendation unambiguous, no blockers.
