# Verify Report: latency-tracking

## Executive Summary

All six spec requirements (L1–L6) pass. The full Go test suite (64 tests across 12 packages) passes with `-race`, the frontend type-checks and builds cleanly, and zero dependencies were added to `go.mod` or `web/package.json`. The latency feature itself is well tested; the broader project test coverage averages 32.3% with significant gaps in `cmd/server`, `internal/exchange`, `internal/feed`, `internal/types`, and `config`, none of which are introduced by this change but several of which carry meaningful risk and should be tracked as follow-ups.

CRITICAL: 0. WARNING: 2 (pre-existing coverage gaps). SUGGESTION: 5.

## 1. Spec Compliance

| ID | Requirement | Result | Evidence |
|----|-------------|--------|----------|
| L1 | Ring-buffer latency recording (1024 slots, circular, called once per ProcessUpdate) | PASS | `internal/engine/latency.go:14-30` — `ring [1024]int64`, `idx = (idx+1) % 1024`, `count` capped at 1024. `internal/engine/engine.go:105` `start := time.Now()` at entry; `engine.go:178` `e.latency.Record(time.Since(start))` as last statement (no defer). Tests `TestLatencyTracker_Empty`, `TestLatencyTracker_ColdStart`, `TestLatencyTracker_RingWrap` all pass. |
| L2 | Exact percentile math: copy → sort → nearest-rank index | PASS | `latency.go:34-49` copies `ring[:n]` into scratch under RLock, releases lock, then sorts and indexes. `percentileIndex` uses `ceil(p*n)-1` clamped to `[0, n-1]`. `TestLatencyTracker_Stats_KnownInput` asserts p50=512ns and p99=1014ns for input 1..1024 — passes. |
| L3 | WS `latency_stats` event at ≤1 Hz with hub throttle ≥250ms and payload shape `{p50_us, p99_us, samples}` | PASS | `cmd/server/main.go:298-299` `latencyTicker := time.NewTicker(1 * time.Second)` + `defer latencyTicker.Stop()`. Select case at line 314-315 calls `publishLatencyStats`. `main.go:469-479` publishes `server.Event{Type: "latency_stats", Data: {p50_us, p99_us, samples}}`. Hub throttle: `internal/server/ws.go:28` adds `"latency_stats": true` to `throttledEventTypes`, hub flushes pending at `throttleInterval = 250ms` (`ws.go:14`). |
| L4 | StatusBar shows `p50: X.Xµs / p99: Y.Yµs` only when `samples >= 10` | PASS | `web/src/components/StatusBar.tsx:262` `{latency.samples >= 10 && (…)}` gate. Lines 266-268 format with `.toFixed(1)`. Block reuses existing `styles.stat`, `styles.statKey`, `styles.statValue` — no new CSS. Store: `marketStore.ts:128` initializes `latency: { p50: 0, p99: 0, samples: 0 }`, `marketStore.ts:209` `setLatency` is an atomic replacement. Dispatcher: `useMarketSocket.ts:54` `case 'latency_stats': store.setLatency(ev.data.p50_us, ev.data.p99_us, ev.data.samples)`. |
| L5 | Zero added dependencies in `go.mod` and `web/package.json` | PASS | `git diff f1080ab^..HEAD -- go.mod` returns empty. `git diff f1080ab^..HEAD -- web/package.json web/package-lock.json` returns empty. Latency code imports only stdlib (`math`, `sort`, `sync`, `time`). |
| L6 | Concurrency-safe accessor (RWMutex; Record writes, Stats reads) | PASS | `latency.go:15` `mu sync.RWMutex`. `Record` uses `mu.Lock`/`Unlock` (lines 23, 29). `Stats` uses `mu.RLock`/`RUnlock` (lines 35, 43) and releases the lock before sorting — keeps the hot path off the sort. `TestLatencyTracker_Race` runs 8 goroutines mixing Record + Stats; `go test -race ./...` reports no DATA RACE. |

Scenario-level checks:

- L1 "Ring wraps at 1024": covered by `TestLatencyTracker_RingWrap` (records 1025, asserts samples == 1024).
- L1 "Cold-start suppression" (samples < 10 → engine returns 0,0,n): covered by `TestEngine_LatencyStats_ColdStart`.
- L2 "Uniform input" (all V → p50 = V and p99 = V): NOT directly tested. The nearest-rank formula trivially yields V on a constant array; the indexing math is exercised by the ordered-input test, so this is implicit, but no explicit assertion exists. SUGGESTION below.
- L3 "Hub throttle enforced" (two publishes in 250ms → one delivery): NOT explicitly tested for `latency_stats` (existing `ws_test.go` tests throttle generally with `price_update`). Since the throttle is keyed on `ev.Type` via a shared map, the contract holds by construction, but a one-liner addition to the throttle test would prove it for the new type. SUGGESTION below.
- L4 "Value freshness" (≤2s update on store change): trivially satisfied because the WS publisher fires once per second and the dispatcher does atomic `set`. Not asserted in unit tests; would require an E2E test.

## 2. Test Suite Results

- `go test -race -count=1 ./...` → all packages pass (12 packages, 64 tests, 0 failures, 0 DATA RACE).
- `npx tsc --noEmit` (web) → no errors.
- `npm run build` (web) → 1620 modules transformed, `dist/assets/index-Bgj9aArg.js` 195.34 kB gzip 60.22 kB, built in 692ms.
- `git diff` against pre-change baseline for `go.mod`, `go.sum`, `web/package.json`, `web/package-lock.json` → all empty (L5 verified).

## 3. Project Test Coverage Audit

`go test ./... -cover` summary (statement coverage, all packages):

| Package | Coverage | Has Tests | Risk Tier |
|---------|---------:|-----------|-----------|
| `cmd/server` | 0.0% | no test files | Medium — wiring code, but holds the entire processing loop including the new `publishLatencyStats` |
| `config` | 0.0% | no test files | Low — env parsing, mostly defaults |
| `internal/engine` | 85.7% | yes | Tested |
| `internal/exchange` | 0.0% | no test files | High — 10 WS connectors, message parsing, reconnect/backoff |
| `internal/executor` | 81.8% | yes | Tested |
| `internal/feed` | 0.0% | no test files | High — fan-in aggregator, `Snapshot`/`Latest` accessors used by engine |
| `internal/model` | 86.5% | yes | Tested |
| `internal/risk` | 70.0% | yes | Tested (setters and `String` uncovered) |
| `internal/server` | 90.3% | yes | Tested |
| `internal/store` | 66.2% | yes | Tested (Close, load paths partial) |
| `internal/types` | 0.0% | no test files | Low — value types, `IsStale` is one expression |
| `internal/wallet` | 88.5% | yes | Tested |
| **Total** | **32.3%** | — | — |

### Direct vs indirect coverage (notable gaps)

Functions that have direct tests:

- All of `LatencyTracker` (`Record`, `Stats`, `percentileIndex`) — 100%.
- `Engine.LatencyStats` — 100% via `TestEngine_LatencyStats_*`.
- `Engine.ProcessUpdate` — 94.3% indirectly via the wider engine tests; the latency `Record` line is covered.

Functions covered only indirectly through callers:

- `engine.feeFor` (66.7%) — fallback branch (unknown exchange) is not asserted.
- `model.SpreadModel.Update` (92.0%), `Mean` (80.0%), `stdLocked` (83.3%) — edge cases (`n < 2`) not exercised.
- `store.persistTrade` (55.6%), `store.loadTrades` (38.5%), `store.NewStore` (57.9%) — error branches uncovered.
- `server.handleConfig` (54.5%) — patch error paths uncovered (the fee-patch tests added in commits `4032bbc` / `d0f4c79` cover the happy path).

Functions NOT tested at all (sample, sorted by risk):

High-risk (financial, concurrent state, or error paths that can corrupt user-visible data):

- `cmd/server/main.go` — entire `runProcessingLoop`, including `publishLatencyStats`, `publishSpreadStats`, `publishPriceSnapshot`, `publishPriceUpdate`, the executor scheduling tick, and the new `latencyTicker` wiring. None of this is tested. The new latency feature relies on `runProcessingLoop` to actually call `publishLatencyStats` once per second; only manual smoke testing (task 6.5) proves this end-to-end.
- `internal/exchange/*` — all 10 connectors (`binance`, `bybit`, `okx`, `gate`, `kraken`, `bitget`, `htx`, `kucoin`, `cryptocom`, `mexc`). `Connect`, `runWithReconnect`, `run`, and per-exchange parsers are 0% covered. JSON parsing bugs here would silently drop or corrupt price data, which feeds directly into engine arbitrage detection.
- `internal/exchange/connector.go:backoff` — reconnect backoff is 0% covered. A bug here could either tight-loop on disconnect or starve reconnection.
- `internal/feed/aggregator.go` — `NewAggregator`, `Start`, `Updates`, `Latest`, `Snapshot`, `drain` are all 0% covered. `Snapshot` is the function the engine calls inside `ProcessUpdate`; a bug here affects every detection cycle and would also corrupt the latency timing window.
- `engine.SetFees`, `SetMinNetProfitPct`, `SetMaxPositionUSDT`, `SetStalenessThreshold` (0%) — these are the live-config setters invoked by the PATCH /api/config endpoint. Concurrency is mediated by `cfgMu` but the setters themselves are not unit tested; a bug here would silently apply wrong fees.

Medium-risk (observable but not financially load-bearing):

- `risk.String` (0%) — enum-to-string for logs; cosmetic.
- `risk.SetMinNetProfitPct`, `SetConsecutiveLossN`, `SetLossThreshold` (0%) — runtime config setters, similar to engine.
- `store.Close` (0%) — only runs at shutdown; bug here means dirty shutdown.

Low-risk (glue, DTOs, main wiring):

- `internal/types/types.go` — `Now`, `IsStale` (0%). Tiny one-liners.
- `config/config.go` — env parsing with defaults. Bug here is loud (won't boot).
- `model.SpreadModel.N` (0%) — observation accessor.

### Prioritized recommendations

1. WARNING — Add tests for `internal/exchange` connector message parsing (high impact, 10 files at 0%). At minimum, freeze sample WS frames per exchange and assert correct `types.PriceUpdate` output. Bugs here cause invisible price corruption that feeds into the engine.
2. WARNING — Add a smoke-level test for `runProcessingLoop` in `cmd/server`. The new `latencyTicker` integration is currently only validated by Phase 6.5 manual smoke. A small test that swaps `time.NewTicker` for a mock channel and asserts `publishLatencyStats` is called would lock the contract.
3. SUGGESTION — Add an explicit `TestLatencyTracker_Stats_Uniform` to the latency tests so the L2 "Uniform input" scenario has a direct assertion.
4. SUGGESTION — Extend the existing hub throttle test in `internal/server/ws_test.go` to also assert `latency_stats` is throttled at the same 250ms cadence as `price_update`. One additional case in the existing table-driven test.
5. SUGGESTION — Add tests for `engine.SetFees` and the other live setters; they are short and the patch endpoint depends on them.
6. SUGGESTION — Cover `internal/feed/aggregator.go` (`Snapshot`, `Updates`, `drain`) since this is the function called inside the latency-instrumented `ProcessUpdate`.
7. SUGGESTION — Cover `internal/exchange/connector.go:backoff` with a deterministic timer test; reconnect logic is invisible until it breaks in production.

## 4. Findings

CRITICAL: none.

WARNING (pre-existing, not introduced by this change, but flagged because the user asked for a project-wide coverage view):

- W1 `internal/exchange/*` has zero tests across 10 connector implementations.
- W2 `cmd/server/main.go` has zero tests; `runProcessingLoop` (the consumer of the new latency feature) is only validated by manual smoke.

SUGGESTION:

- S1 Add `TestLatencyTracker_Stats_Uniform` for explicit L2 scenario coverage.
- S2 Extend `ws_test.go` to assert `latency_stats` throttle behavior.
- S3 Add tests for engine and risk config setters.
- S4 Add a test or sample-frame fixture suite for at least the most-used `internal/exchange` connectors.
- S5 Add tests for `internal/feed/aggregator.go` Snapshot/Updates/drain.

## 5. Recommended Next

`sdd-archive` — the feature itself meets every spec requirement and all gating tests pass. The coverage warnings are pre-existing technical debt unrelated to this change and should be tracked as their own SDD changes rather than blocking this archive.

## 6. Risks

None blocking archive. The two WARNINGs above are pre-existing and out of scope for `latency-tracking`.
