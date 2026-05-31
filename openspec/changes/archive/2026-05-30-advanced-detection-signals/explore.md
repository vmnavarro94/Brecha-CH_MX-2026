# Exploration: advanced-detection-signals

5 features for Day 4. Verified against real codebase.

## Current State

- `internal/depth/depth.go` generates synthetic books from BBO. `types.OrderBookLevel` exists but not embedded in PriceUpdate.
- All 10 connectors: BBO-only. `BidSize/AskSize` populated everywhere.
- Aggregator: single `prices map[string]*PriceUpdate`. No L2 storage.
- SpatialStrategy scoring: `netPct*0.6 + sigmoid(z)*0.4`. BidSize/AskSize ignored.
- NetworkLatencyBps static in fee config.
- WS hub upgrader has no `EnableCompression` set. gorilla supports it.
- LatencyTracker: 1 instance, measures ProcessUpdate only. No per-exchange.

## Per-Feature Approaches

### Feature 1 — L2 Real Depth (Binance, Bybit, OKX)

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| A | `Levels` in PriceUpdate | No interface change | BBO clobbers Levels; merge state needed | Medium |
| **B** | `BookConnector` optional iface + `books` map in Aggregator | Clean separation; zero breakage to non-L2 connectors | Slight plumbing | Medium |
| C | New BookUpdate event type | Perfect cadence isolation | Breaks Connector interface | High |

**Recommend B.** Add `BookConnector` optional iface. Aggregator type-asserts on start. 3 of 10 implement it. Executor gets `bookFn(exchange) []Level` closure.

**Per-exchange channels:**
- Binance: combined stream URL `?streams=btcusdt@bookTicker/btcusdt@depth20@100ms`, wrapped messages
- Bybit: `orderbook.50.BTCUSDT`, ignore deltas for demo (use snapshot only)
- OKX: `books5:BTC-USDT`, derive BBO from `bids[0]`/`asks[0]`

### Feature 2 — Order Book Imbalance

| # | Approach | Pros | Cons |
|---|---|---|---|
| A | Hardcoded 0.1 penalty | Zero struct change | Magic number |
| **B** | `ImbalancePenaltyWeight` in spatial.Config | Tunable, PATCH-able | One config field |

**Recommend B.** Formula: `penalty = weight * max(0, -(sellBidSize-sellAskSize)/(sellBidSize+sellAskSize))`. Only penalize when sell side is ask-heavy. Score = `base_score - penalty`. Works for all 10 exchanges (BidSize/AskSize already populated).

### Feature 3 — Dynamic Latency Cost

| # | Approach | Pros | Cons |
|---|---|---|---|
| **A** | Linear `baseBps * (1 + ageMs/1000)` capped at 5× | Simple, predictable | Cap unused at 2s staleness (max 3×) |
| B | Exponential `baseBps * exp(ageMs/2000)` | Smoother | Same effective range, less predictable |

**Recommend A.** 0ms = 1×, 1000ms = 2×, 2000ms = 3×. Only sell-side counterparty age matters (buy leg age ≈ 0).

### Feature 4 — WS Compression

| # | Approach | Pros | Cons |
|---|---|---|---|
| **A** | Hub outbound only (`EnableCompression: true`) | 2 lines, browser auto-negotiates | No inbound savings |
| B | Hub + inbound connectors | ~70% both directions | CEX deflate quirks |

**Recommend A** for this PR. Inbound is follow-up.

### Feature 5 — Per-Exchange WS Latency

| # | Approach | Pros | Cons |
|---|---|---|---|
| **A** | Inject `*LatencyTracker` per connector (nil-safe) | Real T0-T1 parse time | 10 constructor signatures change |
| B | Measure in aggregator drain | Zero connector changes | Measures channel wait, not parse |

**Recommend A.** T0 = right after `conn.ReadMessage()`. T1 = right before `b.ch <- pu`. Move `LatencyTracker` to `internal/metrics` to avoid future import cycle. Extend `/api/health` with `parse_p50_us`/`parse_p99_us` per exchange.

## Risks

1. **Binance combined-stream rewrite (HIGH)** — different URL format + wrapped messages. Existing parseMessage returns ok=false until updated. Needs dedicated fixtures.
2. **Bybit deltas (MEDIUM)** — `orderbook.50` alternates snapshot/delta. For demo, drop deltas → L2 stale after initial snapshot. Document limitation.
3. **Imbalance penalty calibration (MEDIUM)** — wrong sign/coefficient → over-filter. Explicit unit test mandatory.
4. **Linear cap unused (LOW)** — staleness 2s + linear → max 3×. 5× cap is dead code. Either lower or document.
5. **LatencyTracker import cycle (LOW)** — moving to `internal/metrics` prevents future engine↔exchange cycle.

## Affected Files

- `internal/types/types.go` — BookUpdate type (F1)
- `internal/feed/aggregator.go` — books map + BookConnector check + Book() accessor (F1)
- `internal/exchange/binance.go` — combined stream + dual parser (F1) + T0/T1 (F5)
- `internal/exchange/bybit.go` — `orderbook.50` + L2 (F1) + latency (F5)
- `internal/exchange/okx.go` — `books5` + L2 (F1) + latency (F5)
- 7 other connectors — latency injection only (F5)
- `internal/executor/executor.go` — L2 fallback logic (F1)
- `internal/strategy/spatial/spatial.go` — imbalance penalty (F2) + dynamic latency (F3)
- `internal/server/ws.go` — `EnableCompression` + `SetCompressionLevel` (F4)
- `internal/server/api.go` — extend /api/health (F5)
- `internal/engine/latency.go` OR new `internal/metrics/latency.go` (F5)
- `cmd/server/main.go` — wire LatencyTrackers + bookFn closure

## Suggested PR Slicing

5 features, partially independent:

- **Slice 1**: F4 (WS compression) — trivial, ship alone
- **Slice 2**: F3 (dynamic latency cost) — spatial.go only
- **Slice 3**: F2 (imbalance penalty) — spatial.go only
- **Slice 4**: F5 (per-exchange WS latency) — 10 connectors + main + API
- **Slice 5**: F1 (L2 real depth) — 3 connectors major rewrite + executor + aggregator

Slice 5 is the biggest. Could be split further (Binance, Bybit, OKX separately).

## Open Questions for Proposal

1. **F5 LatencyTracker location**: keep in engine package or move to new `internal/metrics`?
2. **F1 Bybit deltas**: support delta application or document as known limitation?
3. **F1 fallback semantics**: when L2 missing for an exchange, fall back to synthetic silently or surface in API/UI?
4. **F2 default weight**: 0.1, 0.2 or higher? Calibrate after demo or pick conservative?
5. **F3 cap value**: 3× (matches staleness threshold) or 5× (safety net)?
6. **Apply order**: do all 5 in one batch or chain (F4 + F3 + F2 first as warm-up, then F5, then F1)?

## Ready for Proposal

Yes. Architecture clear per feature, recommendations locked, open questions are tuning choices.
