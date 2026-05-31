# Tasks: advanced-detection-signals

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | PR1 ~490 lines, PR2 ~830 lines |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 (F4+F3+F2+F5) → PR2 (F1 L2 depth) |
| Delivery strategy | auto-chain |
| Chain strategy | feature-branch-chain |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Signal quality + observability (F4, F3, F2, F5) | PR1 | Base: `feat/funding-rate-arbitrage`; all tests green before merge |
| 2 | L2 real depth for Binance/Bybit/OKX (F1) | PR2 | Base: PR1 branch; highest-risk commit is Binance combined-stream |

---

## Phase 1 — PR1: Foundation (F5 metrics package)

- [ ] 1.1 Create `internal/metrics/` directory; move `internal/engine/latency.go` → `internal/metrics/latency.go`; rename package declaration to `package metrics`. Spec: P1.
- [ ] 1.2 Move `internal/engine/latency_test.go` → `internal/metrics/latency_test.go`; rename package reference.
- [ ] 1.3 Update `internal/engine/engine.go`: replace import of engine-local latency with `internal/metrics`; update field type to `*metrics.LatencyTracker`.
- [ ] 1.4 Build verify + `go test ./...` must pass with all 184 baseline tests. Spec: NR1, NR3.
- [ ] 1.5 Commit: `feat(metrics): move LatencyTracker to internal/metrics package`

## Phase 2 — PR1: F4 WS Compression

- [ ] 2.1 In `internal/server/ws.go`: set `EnableCompression: true` on the gorilla upgrader at package level. Spec: W1.
- [ ] 2.2 In `ServeWS` after `upgrader.Upgrade(...)` succeeds: call `conn.EnableWriteCompression(true)` + `conn.SetCompressionLevel(1)`. Spec: W1.
- [ ] 2.3 Build verify (gorilla negotiation is automatic; no unit test needed for header).
- [ ] 2.4 Commit: `feat(ws): enable permessage-deflate compression level 1`

## Phase 3 — PR1: F3 Dynamic Latency Cost (TDD)

- [ ] 3.1-3.5 RED: Five tests for fresh/1s/2s/cap/buy-unaffected scenarios. Spec: L1-L3.
- [ ] 3.6 Implement `ageFactor` in `internal/strategy/spatial/spatial.go Detect`: apply only to `sellBid * sellFee.NetworkLatencyBps / 10000.0` component; cap at 3.0.
- [ ] 3.7 GREEN: `go test ./internal/strategy/spatial/...`
- [ ] 3.8 Commit: `feat(spatial): dynamic latency cost scales with sell-side counterparty age`

## Phase 4 — PR1: F2 Imbalance Penalty (TDD)

- [ ] 4.1 Add `ImbalancePenaltyWeight float64` to `spatial.Config`; default 0.15. Spec: I3.
- [ ] 4.2-4.5 RED: Four tests (balanced, bid-heavy, ask-heavy, nil-book). Spec: I1.
- [ ] 4.6 Implement penalty formula in `Detect` (subtract from score before heap push). Spec: I2.
- [ ] 4.7 Add `SetImbalancePenaltyWeight(v float64)` under `cfgMu.Lock`. Spec: I3.
- [ ] 4.8 RED: Test live weight change reflects on next Detect.
- [ ] 4.9 GREEN: All 8 new tests + existing pass.
- [ ] 4.10 Commit: `feat(spatial): imbalance penalty with configurable weight`

## Phase 5 — PR1: F5 Connector Measurement (TDD)

- [ ] 5.1 RED: `TestBinance_ParseTrackerRecordsLatency`. Spec: P2.
- [ ] 5.2 Add `tracker *metrics.LatencyTracker` field to connectors; update constructors.
- [ ] 5.3 In read loop: capture `t0 := time.Now()` after `ReadMessage`; call `tracker.Record(...)` nil-guarded before emitting. Spec: P2.
- [ ] 5.4 GREEN: Binance tests.
- [ ] 5.5 Apply same pattern to remaining 9 connectors.
- [ ] 5.6 `go test ./...` — all 184+ tests pass. Spec: NR1.
- [ ] 5.7 Commit: `feat(exchange): per-connector WS parse latency tracker`

## Phase 6 — PR1: F5 API Health Extension (TDD)

- [ ] 6.1 Add `ParseLatencyP50Us`, `ParseLatencyP99Us` to `ExchangeHealth`. Spec: P3.
- [ ] 6.2 RED: `TestAPIHealth_IncludesParseLatency`. Spec: P3.
- [ ] 6.3 In health handler: read tracker stats, convert to microseconds, apply cold-start gate.
- [ ] 6.4 GREEN: `go test ./internal/server/...`
- [ ] 6.5 Commit: `feat(api): expose parse_p50_us and parse_p99_us in /api/health`

## Phase 7 — PR1: main.go Wiring

- [ ] 7.1 Construct `parseTrackers` map with one tracker per exchange.
- [ ] 7.2 Pass each tracker to connector constructors.
- [ ] 7.3 Pass `parseTrackers` into health handler.
- [ ] 7.4 Build verify + `go test ./...` — all tests pass.
- [ ] 7.5 Commit: `feat(main): wire parseTrackers into connectors and health handler`

**STOP — PR1 complete. Open PR targeting `feat/funding-rate-arbitrage`. Verify CI. Begin PR2 on new branch from PR1.**

---

## Phase 8 — PR2: Foundation (types + BookConnector interface)

- [ ] 8.1 Add `types.OrderBook` struct (Bids, Asks, ReceivedAt).
- [ ] 8.2 Create `internal/exchange/book_connector.go`: define `BookConnector interface { BookUpdates() <-chan BookUpdate }` and `BookUpdate`.
- [ ] 8.3 Commit: `feat(exchange): BookConnector interface and BookUpdate type`

## Phase 9 — PR2: Aggregator.Book (TDD)

- [ ] 9.1-9.3 Add books map, accessors, drainBook goroutine in Aggregator.Start().
- [ ] 9.4-9.5 RED: Two tests (L2 connector, no-L2 connector).
- [ ] 9.6 GREEN: `go test ./internal/feed/...`
- [ ] 9.7 Commit: `feat(aggregator): Book/HasL2 backed by BookConnector drain`

## Phase 10 — PR2: Binance L2 (TDD — highest risk)

- [ ] 10.1 Update Binance `wsURL` to combined stream.
- [ ] 10.2 Update `parseMessage` to unwrap envelope, dispatch by stream.
- [ ] 10.3 Implement depth20 parser.
- [ ] 10.4 Add `bookCh` field, `BookUpdates()` method.
- [ ] 10.5-10.6 RED: Two tests (bookTicker, depth20).
- [ ] 10.7 GREEN: `go test ./internal/exchange/...`
- [ ] 10.8 Commit: `feat(binance): combined-stream L2 depth20 + bookTicker`

## Phase 11 — PR2: Bybit L2 (TDD)

- [ ] 11.1 Update subscription to `orderbook.50.BTCUSDT`.
- [ ] 11.2 Handle snapshots (emit BookUpdate), deltas (no-op).
- [ ] 11.3 Add `bookCh` field, `BookUpdates()` method.
- [ ] 11.4-11.5 RED: Snapshot test, delta no-op test.
- [ ] 11.6 GREEN: `go test ./internal/exchange/...`
- [ ] 11.7 Commit: `feat(bybit): orderbook.50 snapshot L2; delta frames no-op`

## Phase 12 — PR2: OKX L2 (TDD)

- [ ] 12.1 Add `books5:BTC-USDT` subscription.
- [ ] 12.2 Dispatch `tickers` → BBO, `books5` → BookUpdate; derive BBO from levels.
- [ ] 12.3 Add `bookCh` field, `BookUpdates()` method.
- [ ] 12.4 RED: books5 parsing test.
- [ ] 12.5 GREEN: `go test ./internal/exchange/...`
- [ ] 12.6 Commit: `feat(okx): books5 L2 subscription and parser`

## Phase 13 — PR2: Executor Fallback (TDD)

- [ ] 13.1 Define `bookSource interface { Book(exchange string) types.OrderBook }`.
- [ ] 13.2 Update Executor constructor to accept bookSource.
- [ ] 13.3 Implement fallback helpers (prefer real L2 when available, else synthetic).
- [ ] 13.4 RED: Two tests (real L2, synthetic fallback).
- [ ] 13.5 GREEN: `go test ./internal/executor/...`
- [ ] 13.6 Commit: `feat(executor): real L2 VWAP walk with silent synthetic fallback`

## Phase 14 — PR2: /api/health has_l2 (TDD)

- [ ] 14.1 Add `HasL2 bool` to `ExchangeHealth`.
- [ ] 14.2 RED: Test that has_l2 is true for binance/bybit/okx, false for other 7.
- [ ] 14.3 In health handler: set from `aggregator.HasL2(exchange)`.
- [ ] 14.4 GREEN: `go test ./internal/server/...`
- [ ] 14.5 Commit: `feat(api): has_l2 field in /api/health sourced from aggregator`

## Phase 15 — PR2: main.go Wiring + Final Verify

- [ ] 15.1 Construct aggregator-backed `bookSource`; pass to Executor.
- [ ] 15.2 Build verify + `go test ./...` — all tests pass (baseline + new).
- [ ] 15.3 Confirm no new entries in `go.mod` / `vendor/`.
- [ ] 15.4 Commit: `feat(main): wire aggregator bookSource into executor`

---

## Summary

15 phases across 2 chained PRs:
- **PR1 (7 commits)**: F4 compression + F3 dynamic latency + F2 imbalance + F5 metrics/connectors/API
- **PR2 (8 commits)**: F1 BookConnector interface + Binance/Bybit/OKX L2 + Executor fallback + API has_l2 field + main wiring

Every commit maintains green test suite. All 23 new tests added across 5 features. No breakage to 184-test baseline.
