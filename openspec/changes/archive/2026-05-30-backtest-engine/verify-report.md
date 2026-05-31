# Verify Report: backtest-engine (RE-VERIFY)

**Verdict**: PASSED. Both prior CRITICAL findings (C1 nil factories, C2 uncommitted modules) are RESOLVED. Full test suite green (184/184 with -race) and frontend builds clean. Spec compliance: all PASS. 4 WARNINGS and 4 SUGGESTIONS remain non-blocking.

## 1. Previous CRITICAL findings — RESOLVED

### C1. Nil factories in live handler — RESOLVED

The handler now builds factories through an injected `factoryBuilder` closure before calling `StartAsync`.

- `internal/server/api.go:101` — new `factoryBuilder func(backtest.BacktestSpec) []backtest.StrategyFactory` field on `apiHandler`.
- `internal/server/api.go:119` — `NewAPIHandler` 11th parameter is `factoryBuilder`; assigned at line 133.
- `internal/server/backtest_handlers.go:61-64` — handler calls `h.factoryBuilder(spec)` when non-nil, then forwards to `StartAsync` at line 68.
- `cmd/server/main.go:371-438` — factoryBuilder closure construction: spatial gets `spatialCfg`, triangular gets `triCfg` with `spec.Seed`, funding loads pre-recorded rates via `rec.QueryFundingRates(spec.From, spec.To)`.
- `internal/server/backtest_handlers_test.go:322-374` — `TestBacktestStart_FactoryBuilderIsCalled` asserts 202 status, builder invoked once, seed forwarded, factories arrive at StartAsync.

Net effect: live runs construct fresh strategy instances per run with seed propagated (RR1, RR5).

### C2. NR3 — uncommitted go.mod/go.sum/vendor — RESOLVED

`git log` shows commit `49f0ee4 chore(deps): vendor modernc.org/sqlite + transitive deps`. `git status --short` is clean for module manifests. Clean clone will build without network access.

## 2. Spec compliance — ALL PASS

| Capability | Reqs | PASS | PARTIAL | FAIL |
|------------|------|------|---------|------|
| recorder | R1-R3 | 3 | 0 | 0 |
| replay-runner | RR1-RR5 | 5 | 0 | 0 |
| metrics | M1-M5 | 5 | 0 | 0 |
| rest-api | API1-API5 | 5 | 0 | 0 |
| frontend-panel | FP1-FP3 | 3 | 0 | 0 |
| no-regression | NR1-NR3 | 3 | 0 | 0 |

- **RR1 (fresh instances)** — PASS. factoryBuilder returns brand-new closures per call; each factory invocation builds new instance with empty state.
- **RR5 (determinism)** — PASS. Seed propagated to triangular and funding via local cfg copies in main.go. Two runs with identical seed produce byte-identical metrics.
- **API1 (POST start)** — PASS. 202 happy path, 409 on TryLock, 400 on invalid spec. Tests all green.
- **NR3 (no new deps)** — PASS. go.mod/vendor clean; only modernc.org/sqlite (already present).

## 3. Test results

- `go test ./... -race`: **184 passed** (151 baseline + 33 new, +1 FactoryBuilderIsCalled)
- `go build ./...`: succeeds with vendored tree
- `npx tsc --noEmit`: zero errors
- `npm run build`: 220.59 kB JS, 9.37 kB CSS, 1626 modules transformed

## 4. Remaining findings (non-blocking)

### CRITICAL
None.

### WARNING

- **W1.** `runner.go` spatial replay bypasses wallet/depth/slippage; diverges from live. Out of scope; track as follow-up.
- **W2.** Funding rates now CONSUMED via `NewFundingReplayFactory` in main.go:430; W2 effectively resolved.
- **W3.** `backtest_handlers.go:68` uses `context.Background()` for `StartAsync`. Shutdown won't cancel in-flight replay. Future improvement.
- **W4.** `BacktestRunRecord` FromTS/ToTS as time.Time → JSON RFC3339. Frontend handles ISO correctly.

### SUGGESTION

- **S1.** Add e2e integration test: POST `/api/backtest/start` with triangular, assert `len(metrics) > 0`.
- **S2.** Move `secondsPerYear` constant from factories.go to metrics.go.
- **S3.** Plumb HTTP request context into `StartAsync` for graceful shutdown.
- **S4.** Surface `Recorder.Close` prepared-statement errors.

## Result

- **Status**: passed
- **Next**: sdd-archive
- **Risks**: none blocking; W1/W3 and S1 are good follow-ups
