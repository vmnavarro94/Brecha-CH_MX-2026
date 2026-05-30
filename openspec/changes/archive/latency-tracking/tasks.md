# Tasks: Latency Tracking

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 120–160 |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | single-pr |
| Chain strategy | size-exception (N/A — under budget) |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | All groups A–E | PR 1 | TDD order: RED → GREEN per group; single coherent feature |

---

## Phase 1: Foundation — LatencyTracker (new file)

- [ ] 1.1 **RED** — Create `internal/engine/latency_test.go`. Write `TestLatencyTracker_Stats_KnownInput`: feed values 1..1024 ns via `Record`, call `Stats()`, assert `p50 == 512ns`, `p99 == 1014ns`, `samples == 1024`. Write `TestLatencyTracker_Empty`: assert `p50 == 0`, `p99 == 0`, `samples == 0` on fresh tracker. Write `TestLatencyTracker_ColdStart`: record 9 values, assert `samples == 9` (percentile values may be non-zero — cold-start suppression is at engine boundary, not tracker). Write `TestLatencyTracker_RingWrap`: record 1025 values, assert `samples == 1024`. Run `go test ./internal/engine/... -run TestLatencyTracker` — must FAIL (file does not exist yet).
  - Files: `internal/engine/latency_test.go`
  - Verify: `go test ./internal/engine/... -run TestLatencyTracker` → compile error / FAIL

- [ ] 1.2 **GREEN** — Create `internal/engine/latency.go`. Implement `LatencyTracker` struct with `ring [1024]int64`, `idx int`, `count int`, `mu sync.RWMutex`. Implement `Record(elapsed time.Duration)` (write lock). Implement `Stats() (p50, p99 time.Duration, samples int)` (read lock, copy ring[:count] to scratch, sort ascending, nearest-rank index `ceil(p*n)-1`). Implement `percentileIndex(n int, p float64) int`. Imports: `math`, `sort`, `sync`, `time` — no new deps.
  - Files: `internal/engine/latency.go`
  - Verify: `go test ./internal/engine/... -run TestLatencyTracker` → PASS

- [ ] 1.3 **RACE** — Run `go test -race ./internal/engine/... -run TestLatencyTracker`. Write `TestLatencyTracker_Race` in `latency_test.go`: spawn goroutine calling `Record` 10000 times, call `Stats()` 100 times in main goroutine concurrently. Assert no panic.
  - Files: `internal/engine/latency_test.go`
  - Verify: `go test -race ./internal/engine/... -run TestLatencyTracker` → PASS, no DATA RACE

---

## Phase 2: Core Implementation — Engine Instrumentation

- [ ] 2.1 **RED** — Add to `internal/engine/engine_test.go`: `TestEngine_LatencyStats_AfterUpdates` — create engine, call `ProcessUpdate` 10 times with valid price updates, call `eng.LatencyStats()`, assert `samples == 10` and `p50us > 0`, `p99us > 0`. Add `TestEngine_LatencyStats_ColdStart` — call `ProcessUpdate` 9 times, assert `eng.LatencyStats()` returns `p50us == 0`, `p99us == 0`, `samples == 9`.
  - Files: `internal/engine/engine_test.go`
  - Verify: `go test ./internal/engine/... -run TestEngine_LatencyStats` → FAIL (method does not exist)

- [ ] 2.2 **GREEN** — In `internal/engine/engine.go`: add `latency *LatencyTracker` field to `Engine` struct. In `NewEngine`, allocate `latency: &LatencyTracker{}`. In `ProcessUpdate`, add `start := time.Now()` at entry and `e.latency.Record(time.Since(start))` as the last statement before return (no defer — per design decision 6). Add `LatencyStats() (p50us, p99us float64, samples int)` method: calls `e.latency.Stats()`, returns zeros when `n < 10`, else converts ns to µs as `float64(p50.Nanoseconds())/1000.0`.
  - Files: `internal/engine/engine.go`
  - Verify: `go test ./internal/engine/...` → all PASS

---

## Phase 3: Integration — WS Publishing

- [ ] 3.1 Add `"latency_stats": true` to `throttledEventTypes` map in `internal/server/ws.go`.
  - Files: `internal/server/ws.go`
  - Verify: `go build ./...` → no errors

- [ ] 3.2 In `cmd/server/main.go`, inside `runProcessingLoop`: add `latencyTicker := time.NewTicker(1 * time.Second)` + `defer latencyTicker.Stop()`. Add select case `case <-latencyTicker.C: publishLatencyStats(hub, eng)`. Add function `publishLatencyStats(hub *server.Hub, eng *engine.Engine)` that calls `eng.LatencyStats()` and calls `hub.Publish(server.Event{Type: "latency_stats", Data: map[string]interface{}{"p50_us": p50us, "p99_us": p99us, "samples": samples}})`.
  - Files: `cmd/server/main.go`
  - Verify: `go build ./...` → no errors; `go test ./...` → all PASS

---

## Phase 4: Frontend — Store + Dispatcher

- [ ] 4.1 In `web/src/store/marketStore.ts`: add `latency: { p50: number; p99: number; samples: number }` field to store state. Add `setLatency: (p50: number, p99: number, samples: number) => void` action. Initialize `latency` as `{ p50: 0, p99: 0, samples: 0 }`. Implement `setLatency` as atomic replacement `set({ latency: { p50, p99, samples } })`.
  - Files: `web/src/store/marketStore.ts`
  - Verify: `cd /home/vnav/coding-challenge-mexico/web && npx tsc --noEmit` → no errors

- [ ] 4.2 In `web/src/types/api.ts`: add union branch `| { type: 'latency_stats'; data: { p50_us: number; p99_us: number; samples: number } }` to the WS message type.
  - Files: `web/src/types/api.ts`
  - Verify: `npx tsc --noEmit` → no errors

- [ ] 4.3 In `web/src/hooks/useMarketSocket.ts`: add `case 'latency_stats': store.setLatency(ev.data.p50_us, ev.data.p99_us, ev.data.samples); break;` to the WS message dispatcher switch.
  - Files: `web/src/hooks/useMarketSocket.ts`
  - Verify: `npx tsc --noEmit` → no errors

---

## Phase 5: Frontend — StatusBar Display

- [ ] 5.1 In `web/src/components/StatusBar.tsx`: add `const latency = useMarketStore((s) => s.latency)`. Add conditional block after the Trades stat group: render `detect p50: {latency.p50.toFixed(1)}µs | p99: {latency.p99.toFixed(1)}µs` only when `latency.samples >= 10`. Reuse existing `styles.stat`, `styles.statKey`, `styles.statValue` — no new style definitions.
  - Files: `web/src/components/StatusBar.tsx`
  - Verify: `npx tsc --noEmit && npm run build` → no errors

---

## Phase 6: Verification

- [ ] 6.1 Run full Go test suite including race detector: `go test -race ./...` → all PASS, no DATA RACE.
- [ ] 6.2 Run frontend build: `cd /home/vnav/coding-challenge-mexico/web && npx tsc --noEmit && npm run build` → no errors.
- [ ] 6.3 Confirm `go.mod` has no new `require` lines vs pre-change baseline (L5).
- [ ] 6.4 Confirm `package.json` has no new dependency entries (L5).
- [ ] 6.5 Manual smoke: start app in demo mode, open browser, wait 10+ seconds, verify StatusBar shows `detect p50: X.Xµs | p99: Y.Yµs`. Verify block is absent before 10 samples accumulate.
