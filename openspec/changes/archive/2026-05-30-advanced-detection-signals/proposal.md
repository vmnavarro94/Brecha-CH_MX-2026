# Proposal: advanced-detection-signals

## Intent

Ship five production-grade microstructure features (real L2 depth on top-3 exchanges, order book imbalance scoring, dynamic latency cost, WS compression, per-exchange WS parse latency) so the arbitrage engine stops lying about depth, stops trusting stale counterparties at face value, and exposes its own internal latency honestly through the API.

## Why now

The current engine ignores liquidity geometry (synthetic depth everywhere, no imbalance signal), treats a 2-second-old quote the same as a fresh one for cost purposes, ships uncompressed WS frames to the browser, and reports a single aggregate processing latency that hides per-exchange parse cost. Demo day is close, and these five gaps are the difference between "looks like an arb engine" and "behaves like one." They are also independent enough to land in two well-scoped PRs without risking the 184-test baseline.

## Success criteria

- F4: WebSocket frames compressed end-to-end, observable in browser DevTools network panel
- F3: A counterparty quote 1s stale produces 2x the base latency cost vs a fresh one; fixture test verifies the linear law and the 3x cap at 2000ms
- F2: A sell-side bid-heavy book (`BidSize >> AskSize`) yields penalty 0; a sell-side ask-heavy book yields a positive penalty proportional to imbalance and weight
- F5: `/api/health` exposes `parse_p50_us` and `parse_p99_us` per exchange, all under 100µs in steady state
- F1: `/api/health` exposes `has_l2` per exchange, `true` for binance/bybit/okx, `false` for the other seven. Executor uses real L2 levels when available and synthetic fallback otherwise, verified by integration test
- All 184 existing tests pass, plus dedicated new tests per feature
- No new external dependencies (gorilla compression is already vendored)

## Scope (in)

### PR1 — quick wins, low blast radius (~500 lines)

- **F4 — WS compression (outbound)**: enable `Upgrader.EnableCompression = true` plus `conn.SetCompressionLevel(1)` on the browser hub. Browser auto-negotiates `permessage-deflate`.
- **F3 — Dynamic latency cost**: in `internal/strategy/spatial`, replace static `NetworkLatencyBps` usage with `baseBps * (1 + ageMs/1000)` capped at 3x. Only the sell-side counterparty age contributes (buy leg age ≈ 0 at evaluation time).
- **F2 — Imbalance penalty**: add `ImbalancePenaltyWeight float64` to `spatial.Config` with default `0.15`. Formula: `penalty = weight * max(0, -(sellBidSize - sellAskSize) / (sellBidSize + sellAskSize))`. Final score = `base_score - penalty`. Setter `SetImbalancePenaltyWeight` for live PATCH support, matching the existing fee patch pattern.
- **F5 — Per-exchange WS parse latency**:
  - Move `internal/engine/latency.go` to new `internal/metrics/latency.go`. `engine.LatencyTracker` becomes `metrics.LatencyTracker`. Both `engine` and `exchange` packages import from `metrics`. Proactive break of the latent import cycle.
  - Inject optional `*metrics.LatencyTracker` into each of the 10 connector constructors (nil-safe — connector skips recording when nil).
  - T0 = right after `conn.ReadMessage()`. T1 = right before `b.ch <- pu`. Tracker.Record(T1 - T0).
  - Extend `/api/health` response per exchange with `parse_p50_us` and `parse_p99_us`.

### PR2 — L2 real depth on top-3 (~800 lines, isolated rewrite)

- **F1 — Real L2 depth**:
  - Define `BookConnector` optional interface in `internal/exchange`: `Book() []types.OrderBookLevel`.
  - Extend `feed.Aggregator` with `books map[string][]types.OrderBookLevel` and accessor `Book(exchange string) []types.OrderBookLevel`. Type-assert connectors on aggregator start to discover which ones implement `BookConnector`.
  - Executor receives a `bookFn func(string) []types.OrderBookLevel` closure. When `bookFn(ex)` returns non-empty, use real L2 levels for VWAP walk; otherwise fall back to synthetic depth generation.
  - **Binance**: switch to combined stream URL `?streams=btcusdt@bookTicker/btcusdt@depth20@100ms`. Parse wrapped messages (`{stream, data}`). Maintain BBO from bookTicker and L2 snapshot from depth20.
  - **Bybit**: subscribe to `orderbook.50.BTCUSDT`. Snapshot frames only (`type: "snapshot"`). Delta application deferred — documented as known limitation; L2 becomes stale between snapshots but BBO stays fresh from existing channel.
  - **OKX**: subscribe to `books5:BTC-USDT`. Derive BBO from `bids[0]` / `asks[0]`, expose full 5-level snapshot via `Book()`.
  - **API honesty**: extend `/api/health` per-exchange entry with `has_l2 bool`. True for binance/bybit/okx, false for the other seven. No silent fallback — the API tells the truth.

## Scope (out)

- Inbound WS compression on connector dialers (separate follow-up; CEX deflate quirks need their own investigation)
- Bybit `orderbook.50` delta application — always consume snapshot frames only for this change
- L2 depth for Kraken, Gate, MEXC, Bitget, HTX, Crypto.com, KuCoin — synthetic depth remains for these seven
- Frontend visualization of per-exchange parse latency — data is exposed via `/api/health` but UI panel is deferred
- L2 visualization in the spread chart or order book panel (data layer only in this change)

## Public contract changes

- `metrics.LatencyTracker` — same surface as current `engine.LatencyTracker` (`Record(dur)`, `Stats() (p50, p99)`, ring size 1024). All call sites update import path.
- `exchange.BookConnector` — optional interface: `Book() []types.OrderBookLevel`. Discovered via type assertion.
- `feed.Aggregator.Book(exchange string) []types.OrderBookLevel` — new accessor.
- `spatial.Config.ImbalancePenaltyWeight float64` — new tunable, default 0.15, exposed via `SetImbalancePenaltyWeight` and `/api/config` PATCH.
- `/api/health` JSON gains `parse_p50_us`, `parse_p99_us`, and `has_l2` per exchange. Additive only — no breaking renames.

## Approach

### PR1 atomic commit plan

1. F4: enable compression on outbound hub (~5 LOC)
2. F3: linear latency cost with 3x cap, plus fixture test (~10 LOC)
3. F2: `ImbalancePenaltyWeight` + setter + unit test covering balanced, bid-heavy, ask-heavy cases (~20 LOC)
4. F5 setup: move `engine/latency.go` to `metrics/latency.go`, update imports across engine and tests (~20 LOC diff, mechanical)
5. F5 wiring: extend the 10 connector constructors with optional `*metrics.LatencyTracker`, measure T0/T1, nil-safe path verified (~80 LOC across connectors)
6. F5 API: extend `/api/health` shape with `parse_p50_us` and `parse_p99_us`, plus response shape test (~30 LOC)

### PR2 atomic commit plan

1. Plumbing: `BookConnector` interface + `Aggregator.books` map + `Book()` accessor + executor `bookFn` closure (~50 LOC + tests)
2. Binance L2: combined stream URL + wrapped message parser + depth20 handler + fixture test (~150 LOC)
3. Bybit L2: `orderbook.50.BTCUSDT` snapshot consumer (~100 LOC)
4. OKX L2: `books5:BTC-USDT` snapshot consumer + BBO derivation (~80 LOC)
5. Executor fallback: real L2 when available, synthetic when not, integration test covering both paths (~30 LOC)
6. `/api/health` honesty: `has_l2` field per exchange + test (~20 LOC)

### Apply order

Chained 2 PRs. PR1 first (F4, F3, F2, F5) lands the quick wins and prepares the metrics package. PR2 (F1) rides on top with the L2 rewrite isolated from everything else. This keeps each diff reviewable and lets us bail on PR2 cleanly if Binance combined-stream parsing slips.

## Risks

1. **Binance combined-stream rewrite (HIGH)** — URL format and message envelope both change. Existing `parseMessage` returns `ok=false` until updated. Requires dedicated fixtures for both `bookTicker` and `depth20` payloads. Mitigation: ship behind explicit fixture-driven tests before flipping the live URL.
2. **Bybit deltas (MEDIUM, accepted)** — `orderbook.50` alternates snapshot and delta. We consume snapshots only, so L2 goes stale between snapshots. Documented as known limitation; BBO remains accurate from the existing channel.
3. **Imbalance penalty calibration (MEDIUM)** — wrong sign or coefficient over-filters opportunities. Mitigation: explicit unit test covering balanced, bid-heavy, and ask-heavy sell-side cases; default weight 0.15 conservative; live PATCH support so demo can tune.
4. **Linear cap at 3x (LOW)** — staleness threshold is 2s, linear law caps at 3x at 2000ms. No dead range. If staleness threshold ever loosens, cap should be revisited.
5. **LatencyTracker package migration (LOW)** — moving to `internal/metrics` touches engine imports and tests. Mitigation: do as a single mechanical commit early in PR1, run full suite before next commit.
6. **`/api/health` shape change (LOW)** — additive fields, no breaking renames, but UI consumers should be checked. Mitigation: web/src consumers ignore unknown fields; verify on first deploy.

## Open questions (resolved)

All six exploration questions are now locked:

1. LatencyTracker location → move to `internal/metrics`
2. Bybit deltas → snapshot-only, document limitation
3. Fallback semantics → expose `has_l2` in `/api/health`
4. F2 default weight → `0.15`
5. F3 cap → `3x`
6. Apply order → chained 2 PRs (PR1: F4+F3+F2+F5, PR2: F1)
