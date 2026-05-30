# Proposal: Latency Tracking

## Intent

Surface engine detection latency (p50/p99 in µs) live in the StatusBar so the jury can verify the bot processes price updates in sub-millisecond time. Today there is zero observability into how fast `ProcessUpdate` actually runs — we claim "fast" without proof.

## Scope

### In Scope

- `LatencyTracker` struct in `internal/engine` with a 1024-slot ring buffer of `int64` nanoseconds
- Instrumentation at `ProcessUpdate` entry (T1) and exit (T2), pure engine time only
- `Engine.LatencyStats() (p50, p99 int64)` accessor returning nanoseconds
- 1-second ticker in `runProcessingLoop` that publishes a `latency_stats` WS event
- `marketStore.ts` field `latency: { p50: number; p99: number }` (microseconds) plus action
- WS dispatcher handler in `App.tsx` for the new event
- StatusBar stat block: `detect p50: X.Xµs | p99: Y.Yµs`

### Out of Scope

- T0→T1 channel-queuing measurement (not pure engine time)
- SQLite persistence of latency samples (live-only)
- `GET /api/latency` REST endpoint (WS-only)
- HDR histogram or t-digest dependencies (ring buffer is sufficient)
- Per-exchange-pair latency breakdown (aggregate only)
- Historical charting in the dashboard

## Capabilities

### New Capabilities

- `latency-tracking`: rolling-window p50/p99 percentile tracking of engine detection latency, with WS exposure and StatusBar display

### Modified Capabilities

- None

## Approach

Pure-Go ring buffer + sort, zero new dependencies. Locked defaults:

1. **Window**: T1→T2 only — entry of `ProcessUpdate` to just before return. Excludes channel queuing.
2. **Buffer size**: 1024 slots (≈10s of history at 100 upd/s).
3. **Display unit**: microseconds (µs), formatted to 1 decimal place.
4. **Persistence**: live-only, no SQLite write path.
5. **Exposure**: WS event `latency_stats` only, no REST endpoint.

Because `ProcessUpdate` and the publish ticker both run in `runProcessingLoop`'s single goroutine, the ring needs no mutex today. A `sync.RWMutex` is added for forward-compatibility if a REST reader appears later — read-side cost is negligible.

Percentiles are computed by copying the ring into a scratch slice, sorting (≈4–8µs for 1024 ints), and indexing `sorted[511]` (p50) and `sorted[1013]` (p99). Compute happens once per second inside the ticker, never on the hot path.

## Public Contract

### Go API

```go
// internal/engine/latency.go
type LatencyTracker struct { /* ring [1024]int64, idx int, count int, mu sync.RWMutex */ }

func NewLatencyTracker() *LatencyTracker
func (t *LatencyTracker) Record(elapsed time.Duration)
func (t *LatencyTracker) Stats() (p50, p99 time.Duration, samples int)

// internal/engine/engine.go
func (e *Engine) LatencyStats() (p50us, p99us float64, samples int)
```

If `samples < 10`, return zeros — avoid reporting noise from cold start.

### WS event payload

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

Throttling: added to `throttledEventTypes` at 250ms (matches `price_update` cadence; values change slowly so this is more than enough).

### Frontend state

```ts
// marketStore.ts
latency: { p50: number; p99: number; samples: number }
setLatency: (p50: number, p99: number, samples: number) => void
```

### StatusBar display

```
detect p50: 42.3µs | p99: 187.5µs
```

Hidden when `samples < 10`.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/engine/latency.go` | New | `LatencyTracker` with ring + percentile math |
| `internal/engine/engine.go` | Modified | Instrument `ProcessUpdate`, expose `LatencyStats()` |
| `cmd/server/main.go` | Modified | 1s ticker in processing loop publishing `latency_stats` |
| `internal/server/ws.go` | Modified | Add `"latency_stats"` to throttled event types |
| `web/src/store/marketStore.ts` | Modified | `latency` field + `setLatency` action |
| `web/src/App.tsx` | Modified | WS dispatcher handles `latency_stats` |
| `web/src/components/StatusBar.tsx` | Modified | New stat block |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Sample starvation on illiquid pairs (ring covers minutes, not seconds) | Medium | Return zeros until `samples ≥ 10`; track count separately |
| Linux clock resolution ~1µs adds noise to sub-µs samples | Low | µs display is order-of-magnitude appropriate; document limitation |
| Future REST endpoint introduces concurrent reads | Low | Ship with `sync.RWMutex` from day one |
| Sort-on-read cost grows if buffer expands later | Low | 1024 is fixed; documented as constant |

## Rollback Plan

Revert the seven affected files. The `LatencyTracker` is isolated (one new file); engine instrumentation is two added lines around `ProcessUpdate`'s body; the WS event is additive and the frontend tolerates unknown events. No database migration, no schema change, no breaking contract.

## Dependencies

- None. Zero new Go modules, zero new npm packages.

## Success Criteria

- [ ] `LatencyTracker` records every `ProcessUpdate` call with T1→T2 elapsed nanoseconds
- [ ] `Engine.LatencyStats()` returns exact p50/p99 over the last 1024 samples
- [ ] WS `latency_stats` event fires once per second with non-zero values after 5s of bot operation in demo mode
- [ ] StatusBar shows `detect p50: X.Xµs | p99: Y.Yµs` updating live
- [ ] `samples < 10` correctly suppresses the display (cold-start safety)
- [ ] Unit test using `fixedClock` verifies percentile math is exact for a known input set
- [ ] No new entries in `go.mod` or `package.json`
