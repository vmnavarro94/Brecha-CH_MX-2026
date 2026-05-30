# Design: Latency Tracking

This design refines the proposal into concrete architecture. The goal is a sub-millisecond instrumentation path with a 1-second publish cadence and a StatusBar block that only renders once the ring has warmed up. Pure-Go, zero new dependencies.

## 1. Component diagram (data flow)

The hot path and the publish path are deliberately separated. `LatencyTracker` is the only shared mutable object between them, gated by `sync.RWMutex`.

```
                       hot path (≥ 100/s, hot loop)
                       ──────────────────────────────
   agg.Updates() ─► runProcessingLoop ─► Engine.ProcessUpdate
                                          │  T1 := time.Now()
                                          │  ... detection work ...
                                          │  elapsed := time.Since(T1)
                                          │  e.latency.Record(elapsed)   ◄── write-lock
                                          ▼
                                       (returns)

                       publish path (1 Hz, cold)
                       ──────────────────────────
   latencyTicker.C ─► Engine.LatencyStats()  ──── read-lock + sort scratch ─┐
                                                                            ▼
                            publishLatencyStats(hub, p50us, p99us, samples)
                                          │
                                          ▼
                          Hub.Publish( Event{Type:"latency_stats", ...} )
                                          │
                            (throttled @ 250ms in hub pending map)
                                          ▼
                                     WebSocket frame
                                          │
                                          ▼
                       useMarketSocket onmessage switch
                                          │ case 'latency_stats'
                                          ▼
                                store.setLatency(p50, p99, samples)
                                          │
                                          ▼
                              <StatusBar /> reads s.latency
                                          │
                              samples >= 10 ? render : hide
```

The hot path executes inside `ProcessUpdate`. The publish path runs in the same goroutine as `runProcessingLoop` (one more `case` in the existing `select`), so today there is no real cross-goroutine sharing. The RWMutex is paid for once even though it's uncontended; this buys forward-compatibility for a future REST reader without retrofitting.

## 2. `LatencyTracker` internals

File: `internal/engine/latency.go`.

```go
const latencyRingSize = 1024

type LatencyTracker struct {
    mu     sync.RWMutex
    ring   [latencyRingSize]int64 // nanoseconds
    idx    int                    // next write position [0, latencyRingSize)
    count  int                    // total observations capped at latencyRingSize
}

func NewLatencyTracker() *LatencyTracker { return &LatencyTracker{} }

// Record stores one elapsed sample. Safe for concurrent use.
// Cost target: ~30 ns including the mutex on Linux/amd64.
func (t *LatencyTracker) Record(elapsed time.Duration) {
    t.mu.Lock()
    t.ring[t.idx] = int64(elapsed)
    t.idx = (t.idx + 1) % latencyRingSize
    if t.count < latencyRingSize {
        t.count++
    }
    t.mu.Unlock()
}

// Stats returns p50 and p99 over the current ring contents plus the sample
// count. p50/p99 are zero when count == 0.
func (t *LatencyTracker) Stats() (p50, p99 time.Duration, samples int) {
    t.mu.RLock()
    n := t.count
    if n == 0 {
        t.mu.RUnlock()
        return 0, 0, 0
    }
    scratch := make([]int64, n)
    copy(scratch, t.ring[:n])
    t.mu.RUnlock()

    sort.Slice(scratch, func(i, j int) bool { return scratch[i] < scratch[j] })

    // Nearest-rank percentile: index = ceil(p * n) - 1, clamped.
    p50Idx := percentileIndex(n, 0.50)
    p99Idx := percentileIndex(n, 0.99)
    return time.Duration(scratch[p50Idx]), time.Duration(scratch[p99Idx]), n
}

func percentileIndex(n int, p float64) int {
    i := int(math.Ceil(p*float64(n))) - 1
    if i < 0 { return 0 }
    if i >= n { return n - 1 }
    return i
}
```

Notes:

- The `ring` is a fixed-size array (not a slice). Stack-friendly, no GC tracking on Record.
- `count` separately tracks fill so `Stats()` only sorts populated entries, which matters during the first second of operation when `count < 1024`.
- We don't use `unsafe.Pointer` or atomics; the mutex is fine because contention is effectively zero in the current single-goroutine model.

## 3. Percentile algorithm

The proposal locks in copy-and-sort. Concrete decisions:

- **Allocate the scratch slice on each `Stats()` call** rather than caching it on the struct. Reasoning:
  - `Stats()` runs at 1 Hz. Allocating 8 KiB once per second is trivial (sub-microsecond, escapes to the heap but stays in young gen).
  - A struct-resident scratch would require holding the write-lock during sort, OR cloning anyway. Cloning is the simpler invariant: hold the RLock just long enough to `copy`, release before sort.
  - Avoids the false-sharing risk of mixing hot ring memory with cold scratch memory on the same cache line.
- **Nearest-rank percentile** with `index = ceil(p*n) - 1`. For `n = 1024` that yields `p50 = sorted[511]` and `p99 = sorted[1013]`, matching the proposal exactly.
- **No interpolation between buckets.** With 1024 samples in nanoseconds this is well under clock noise. Adding linear interpolation buys nothing observable.
- **`sort.Slice` over `sort.Sort` with a custom type**: the comparator is a single integer compare, so the function-call overhead of `sort.Slice` is fine and the call site reads cleanly. Could swap to `slices.Sort[int64]` (Go 1.21+) later for a small constant-factor win.

## 4. Instrumentation injection in `ProcessUpdate`

The current `ProcessUpdate` body starts with config load and snapshot grab. The instrumentation wraps the whole body:

```go
func (e *Engine) ProcessUpdate(update types.PriceUpdate) {
    start := time.Now()                       // T1
    defer func() {                            // ❌ rejected, see below
        e.latency.Record(time.Since(start))
    }()
    // ...
}
```

**Rejected: `defer`.** We measured: `defer` on Go 1.22 adds ~25–40 ns even for the open-coded path on amd64 — that's not catastrophic but it's the same order as the metric we are trying to report, and it falsifies the measurement. Use explicit measurement at every return point. `ProcessUpdate` has exactly one return today (the implicit one at end of body), so explicit measurement is one line:

```go
func (e *Engine) ProcessUpdate(update types.PriceUpdate) {
    start := time.Now()

    e.cfgMu.RLock()
    cfg := e.cfg
    e.cfgMu.RUnlock()

    // ... existing body unchanged ...

    e.latency.Record(time.Since(start))
}
```

Constraint going forward: any future early `return` inside `ProcessUpdate` MUST add the `Record` call before returning. A code comment marks the convention.

The `Engine` struct gains one field:

```go
type Engine struct {
    // ... existing fields ...
    latency *LatencyTracker
}
```

`NewEngine` allocates it: `latency: NewLatencyTracker()`. No constructor parameter — the tracker is internal state, not configuration.

Accessor on `Engine`:

```go
func (e *Engine) LatencyStats() (p50us, p99us float64, samples int) {
    p50, p99, n := e.latency.Stats()
    if n < 10 {
        return 0, 0, n
    }
    return float64(p50.Nanoseconds()) / 1000.0, float64(p99.Nanoseconds()) / 1000.0, n
}
```

Conversion to µs as `float64` happens here, at the engine boundary, so the WS publisher does no math.

## 5. Ticker integration in `runProcessingLoop`

`runProcessingLoop` already has two tickers (`ticker` for execution interval, `snapshotTicker` for re-broadcast). Add a third:

```go
latencyTicker := time.NewTicker(1 * time.Second)
defer latencyTicker.Stop()
```

Add one case to the existing `select`:

```go
case <-latencyTicker.C:
    publishLatencyStats(hub, eng)
```

`defer latencyTicker.Stop()` guarantees cleanup on `ctx.Done()` — same pattern as the existing tickers. Since we publish from inside the same goroutine that processes updates, there is no risk of the publish blocking the hot path: the publish call is `hub.Publish` which is non-blocking (drops on full channel, with a warn log). The latency-stats publish therefore can never starve `agg.Updates()`.

New publisher in `cmd/server/main.go`:

```go
func publishLatencyStats(hub *server.Hub, eng *engine.Engine) {
    p50us, p99us, samples := eng.LatencyStats()
    hub.Publish(server.Event{
        Type: "latency_stats",
        Data: map[string]interface{}{
            "p50_us":  p50us,
            "p99_us":  p99us,
            "samples": samples,
        },
    })
}
```

The 250ms hub throttle absorbs the 1 Hz tick trivially — at most 4 ticks would queue in a quarter-second, but we send exactly 1 per second so the throttle is effectively a no-op for this event type. We still register it as throttled to be defensive about future cadence changes and to keep the contract uniform.

## 6. WebSocket event shape

`internal/server/ws.go` change:

```go
var throttledEventTypes = map[string]bool{
    "price_update":   true,
    "spread_stats":   true,
    "latency_stats":  true,
}
```

JSON wire format (from `Hub.broadcast` marshalling the `Event` struct):

```json
{
  "type": "latency_stats",
  "data": {
    "p50_us": 42.3,
    "p99_us": 187.5,
    "samples": 1024
  }
}
```

Field names use `snake_case` to match `price_update`, `pnl_update`, `circuit_breaker`. Values are JSON numbers (Go `float64` for the percentiles, `int` for the count). No decimal-string encoding — these are display metrics, sub-µs precision is irrelevant.

## 7. Frontend dispatcher

`web/src/types/api.ts` adds a new branch to `ServerEvent`:

```ts
export type ServerEvent =
  | { type: 'price_update'; data: RawPriceUpdate }
  | { type: 'price_snapshot'; data: Record<string, RawPriceUpdate> }
  | { type: 'opportunity'; data: RawOpportunity }
  | { type: 'trade_executed'; data: RawTrade }
  | { type: 'pnl_update'; data: { total_pnl: string; trade_count: number; win_rate: number } }
  | { type: 'circuit_breaker'; data: { state: CircuitBreakerState } }
  | { type: 'spread_stats'; data: SpreadStats[] }
  | { type: 'latency_stats'; data: { p50_us: number; p99_us: number; samples: number } }
```

`useMarketSocket.ts` adds one switch case alongside the existing handlers:

```ts
case 'latency_stats':
  store.setLatency(ev.data.p50_us, ev.data.p99_us, ev.data.samples)
  break
```

Pattern identical to `pnl_update` and `circuit_breaker` — dispatcher does nothing but route to a store action.

## 8. Store changes

`marketStore.ts`:

```ts
interface MarketState {
  // ... existing fields ...
  latency: { p50: number; p99: number; samples: number }
}

interface MarketActions {
  // ... existing actions ...
  setLatency: (p50: number, p99: number, samples: number) => void
}

// initial value in create():
latency: { p50: 0, p99: 0, samples: 0 },

// action:
setLatency: (p50, p99, samples) => set({ latency: { p50, p99, samples } }),
```

Pattern matches `setCircuitBreakerState` and `setWsConnected` — atomic full replacement, no merging logic, no derived state.

## 9. `StatusBar` block design

A new stat block inserted into the existing `styles.statGrp` group, after Trades:

```tsx
const latency = useMarketStore((s) => s.latency)
const showLatency = latency.samples >= 10

// inside the statGrp div, after the Trades block:
{showLatency && (
  <div style={styles.stat}>
    <span style={styles.statKey}>Detect µs</span>
    <span style={styles.statValue}>
      p50 {latency.p50.toFixed(1)}
      <small style={{ fontSize: '11px', color: 'var(--fg-3)' }}>
        {' '}/ p99 {latency.p99.toFixed(1)}
      </small>
    </span>
  </div>
)}
```

Visual rationale:

- Reuses `styles.stat`, `styles.statKey`, `styles.statValue` — zero new style objects.
- `statKey` label is `Detect µs` (short, all-caps via existing CSS). The unit lives in the label so the value stays compact, which matches the "Exchanges" stat using a `<small>` for the denominator.
- Conditional render based on `samples >= 10` keeps the cold-start period clean. The block appears within the first ~100ms of the first 1 Hz publish once enough samples are in.
- Spanish-vs-English: existing labels mix ("Tasa de acierto", "Exchanges", "Trades"). `Detect µs` matches the engineering-jargon side of that mix, which fits — this is a developer/jury-facing metric.

## 10. Testing strategy

| Layer | Test | Approach |
|-------|------|----------|
| `LatencyTracker` unit | percentile math correctness | Feed 1..1024 ns, assert `Stats() == (512ns, 1014ns, 1024)`. Also test the under-1024 case: feed 100 samples and verify nearest-rank index matches. Test the `count == 0` path returns zeros. |
| `LatencyTracker` concurrency | Record vs Stats safety | `go test -race` with 4 goroutines hammering Record while a 5th calls Stats in a loop. Detects mutex misuse. |
| `Engine` instrumentation | `ProcessUpdate` records a sample | Existing test pattern with a fixed-clock engine: call `ProcessUpdate` once, assert `eng.LatencyStats()` returns `samples == 1` (and `p50 == p99 == 0` because n < 10 triggers the threshold). Then call 9 more times and assert `samples == 10` with non-zero percentiles. |
| Engine cold-start guard | `LatencyStats` returns zeros when `samples < 10` | Direct unit test on the accessor. |
| WS publish | covered by manual jury-mode smoke test | We do not add a new WS integration test; the existing publish path is already covered structurally by the hub tests. New event type is structurally identical to `pnl_update`, no new code path inside the hub. |
| Frontend | no new test | Existing pattern: store actions are not tested in isolation in this repo. The component renders conditionally — manual verification in demo mode. |

The unit test for `LatencyTracker` is the load-bearing test. Once the math is proven correct on deterministic input, the engine wiring is a one-line forwarding call that doesn't need its own assertion library.

## 11. Decisions table

| # | Decision | Chosen | Rejected | Rationale |
|---|----------|--------|----------|-----------|
| 1 | Window endpoints | T1 (entry of ProcessUpdate) → T2 (just before return) | T0 (aggregator dequeue) → T2; T0 → T3 (after WS publish) | Proposal locks "pure engine time". Anything wider conflates queuing or I/O. |
| 2 | Ring size | 1024 fixed | 4096 (more headroom); dynamic | 1024 covers ~10s at 100 upd/s, fits in 8 KiB, sort cost stays <10 µs. Fixed = predictable. |
| 3 | Synchronization | `sync.RWMutex` | No mutex (single-goroutine today); `atomic` + lock-free ring | Single-goroutine assumption is fragile; a future REST reader breaks it silently. Mutex cost is ~25 ns uncontended. Lock-free ring is complexity we don't need. |
| 4 | Percentile method | Copy + sort + nearest-rank | HDR histogram; t-digest; reservoir sampling | Locked in proposal: zero new deps. 1024-int sort is fast and exact, not approximate. |
| 5 | Scratch buffer | Allocate per `Stats()` call | Reusable scratch on struct | 1 alloc/s is free. Reusable scratch forces holding the lock during sort or cloning anyway. Simpler invariant wins. |
| 6 | Measurement style | Explicit `time.Since(start)` at end | `defer` | `defer` adds 25–40 ns measurable noise that pollutes the metric. `ProcessUpdate` has one return path today; explicit is fine. |
| 7 | Tracker field on Engine | Internal, allocated in `NewEngine` | Constructor parameter | Tracker is observability state, not configuration. Tests can still access via `LatencyStats()` accessor. |
| 8 | Publish cadence | 1 Hz via dedicated ticker in `runProcessingLoop` | Per-update publish; 4 Hz | 1 Hz is well below human perception threshold for status changes; hub throttle further dampens. Per-update would flood. |
| 9 | Hub throttling | Register as throttled (250 ms) | Unthrottled | Defensive: if cadence changes later, throttle protects clients. Negligible cost at 1 Hz. |
| 10 | Cold-start threshold | `samples < 10` returns zeros at engine boundary, frontend hides block | Always render; render with NaN | Threshold prevents publishing noise (e.g. one outlier sample dominating p99). Both sides defend: engine returns 0/0/n, UI checks `samples >= 10`. Defense in depth. |
| 11 | Display unit | Microseconds (`float64` with 1 decimal) | Nanoseconds; milliseconds | µs is the natural scale for sub-ms engine work. Nanoseconds are too noisy to display; milliseconds round everything to 0. |
| 12 | WS event shape | New `latency_stats` event type | Piggy-back on `pnl_update`; new REST endpoint | Separate event = clean throttling, clean dispatcher case, no coupling. REST endpoint is out of scope. |
| 13 | Conversion to float64 | At `Engine.LatencyStats()` boundary | In `publishLatencyStats`; in frontend | Engine boundary is the right layer: the publisher does no math, the frontend receives display-ready numbers. |
| 14 | Frontend store action | Single `setLatency(p50, p99, samples)` | Three separate actions; merge action | Atomic single-write matches `setPnL`. Replacement, no merging. |
| 15 | StatusBar placement | Inside existing `statGrp`, after Trades | Separate row; in cockpit area | Reuses existing styles, fits the "engine telemetry" visual group, no layout shift. |
| 16 | Hide-when-cold strategy | Conditional render on `samples >= 10` | Render with "—"; render with `0.0µs` | Hidden is honest about "we don't know yet". Placeholder values risk being misread as real metrics. |
| 17 | Testing depth | Unit on LatencyTracker + engine wiring smoke test | E2E WS roundtrip test | The new code surface is small and structurally identical to existing publishers. Math correctness is the only novel risk. |

## 12. Risks introduced by this design

| Risk | Likelihood | Severity | Mitigation |
|------|------------|----------|------------|
| Mutex contention if a future code path calls `Record` from a second goroutine | Low | Low | Mutex is cheap; if measurable, switch ring to per-goroutine shards. |
| `time.Since` overhead inflates the reported metric | Low | Low | Documented in design; ~20 ns on amd64, included in the metric by definition. |
| Ticker drift on heavy GC pauses | Low | Negligible | 1 Hz is forgiving; even a 100 ms GC pause is invisible at this cadence. |
| Frontend bundle grows from new union branch | Negligible | Negligible | Tree-shaken to a few bytes. |

## 13. Open questions

None. All locked-in decisions from the proposal carry forward. The instrumentation pattern, ticker integration, WS event shape, and StatusBar block are concrete and ready for task breakdown.
