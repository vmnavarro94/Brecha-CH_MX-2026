# Spec: advanced-detection-signals

## Scope

Delta spec describing what MUST be true after both PR1 and PR2 land. This spec is
intentionally silent about implementation approach; it states observable outcomes only.

---

## Capability F4: WS Compression (outbound)

### W1 — Compression negotiated per RFC 7692

**Given** a browser WebSocket client that advertises `permessage-deflate` in its
upgrade request

**When** it connects to the `/ws` endpoint

**Then** the server upgrade response includes the `Sec-WebSocket-Extensions:
permessage-deflate` header, confirming negotiation succeeded

---

**Given** a JSON spread update payload whose raw serialized size exceeds 100 bytes

**When** the hub broadcasts it to a connected browser client

**Then** the wire bytes transferred for that frame are measurably smaller than the
uncompressed byte count (verifiable in browser DevTools network panel)

---

## Capability F3: Dynamic Latency Cost

### L1 — Linear scale with counterparty age

**Given** a sell-side counterparty quote with `ReceivedAt = now` (age = 0 ms)

**When** the engine computes the effective latency cost for that counterparty

**Then** `effectiveCost == baseCost * 1.0` (factor = 1×)

---

**Given** a sell-side counterparty quote with `ReceivedAt = now - 1000ms` (age = 1 s)

**When** the engine computes the effective latency cost

**Then** `effectiveCost == baseCost * 2.0` (factor = 2×)

---

**Given** a sell-side counterparty quote with `ReceivedAt = now - 2000ms` (age = 2 s,
equal to the staleness threshold)

**When** the engine computes the effective latency cost

**Then** `effectiveCost == baseCost * 3.0` (factor = 3×)

---

### L2 — Factor capped at 3×

**Given** a sell-side counterparty quote whose age exceeds 2000 ms (e.g., 5000 ms)

**When** the engine computes the effective latency cost

**Then** `effectiveCost == baseCost * 3.0` — the factor is clamped and does not exceed 3×

---

**Given** any effective latency cost computed under L1 or L2

**When** the engine calculates net profit for an opportunity

**Then** the net profit formula uses the scaled effective cost, not the base cost

---

### L3 — Buy-leg age not factored

**Given** the buy-leg counterparty quote (age may be non-zero)

**When** the engine computes latency cost

**Then** only the sell-side counterparty age drives the dynamic scaling; the buy-side
age has no effect on the multiplier

---

## Capability F2: Imbalance Penalty

### I1 — Penalty formula

**Given** a sell-side order book with `BidSize = 10`, `AskSize = 10` (perfectly balanced)

**When** the imbalance penalty is computed with the default weight (0.15)

**Then** `penalty == 0.0` (no penalty for a balanced book)

---

**Given** a sell-side order book with `BidSize = 20`, `AskSize = 5` (bid-heavy — favors
sell execution)

**When** the imbalance penalty is computed

**Then** `imbalance = (20-5)/(20+5) = 0.60 > 0`, so `max(0, -0.60) = 0`,
therefore `penalty == 0.0`

---

**Given** a sell-side order book with `BidSize = 5`, `AskSize = 20` (ask-heavy — harms
sell execution)

**When** the imbalance penalty is computed with default weight 0.15

**Then** `imbalance = (5-20)/(5+20) = -0.60`, `max(0, 0.60) = 0.60`,
`penalty = 0.15 * 0.60 = 0.09`

---

### I2 — Penalty subtracted from score

**Given** a computed base score and a non-zero imbalance penalty

**When** the opportunity score is finalized before priority-queue insertion

**Then** `finalScore = baseScore - penalty`

---

**Given** a large enough penalty (e.g., penalty > baseScore)

**When** `finalScore` is computed

**Then** `finalScore` may be negative — negative scores are valid and not clamped

---

### I3 — Configurable weight

**Given** `spatial.Config` is initialized without explicitly setting `ImbalancePenaltyWeight`

**When** any penalty is computed

**Then** the weight used is `0.15` (the default)

---

**Given** `SetImbalancePenaltyWeight(0.0)` has been called

**When** any penalty is computed regardless of book imbalance

**Then** `penalty == 0.0` — the feature is effectively disabled

---

**Given** `SetImbalancePenaltyWeight(w)` where `w > 0`

**When** the opportunity is scored

**Then** the live penalty calculation reflects the new weight without restarting the
engine

---

## Capability F5: Per-Exchange WS Parse Latency

### P1 — Package contract

**Given** the codebase after PR1 lands

**When** any file imports `internal/metrics`

**Then** `metrics.LatencyTracker` is available with the same public surface as the
former `engine.LatencyTracker`: `Record(d time.Duration)`, `Stats() (p50, p99 int64)`
with a ring buffer of 1024 entries

---

**Given** the same codebase

**When** the `internal/engine` package is inspected

**Then** it imports `internal/metrics` (not the other way around); there is no circular
import between `engine` and `exchange`

---

### P2 — Connector measurement

**Given** a connector constructed with a non-nil `*metrics.LatencyTracker`

**When** a WebSocket message is received

**Then** T0 is captured immediately after `conn.ReadMessage()` returns and T1 is
captured immediately before the parsed update is emitted on the connector's output
channel; `tracker.Record(T1 - T0)` is called on every successfully parsed frame

---

**Given** a connector constructed with `nil` as the `*metrics.LatencyTracker`

**When** a WebSocket message is received

**Then** no recording call is made and the connector behaves identically to its current
behavior (nil-safe no-op path)

---

### P3 — /api/health exposure

**Given** at least 10 samples have been recorded for a given exchange's tracker

**When** `GET /api/health` is called

**Then** the JSON response for that exchange entry contains integer fields
`parse_p50_us` and `parse_p99_us` representing microsecond percentiles; both values
are >= 0

---

**Given** fewer than 10 samples have been recorded for an exchange (cold-start)

**When** `GET /api/health` is called

**Then** `parse_p50_us` and `parse_p99_us` for that exchange are `0`

---

**Given** an existing `/api/health` consumer that reads known fields

**When** PR1 lands

**Then** the consumer is unaffected — `parse_p50_us` and `parse_p99_us` are additive
fields; no existing field is renamed or removed

---

## Capability F1: Real L2 Depth (PR2)

### D1 — BookConnector interface

**Given** the codebase after PR2 lands

**When** `internal/exchange` is inspected

**Then** an interface `BookConnector` exists with a single method `Book() []types.OrderBookLevel`

---

**Given** the 10 exchange connectors in the codebase

**When** each is type-asserted against `exchange.BookConnector`

**Then** Binance, Bybit, and OKX satisfy the assertion; the remaining seven do not

---

### D2 — Aggregator carries books

**Given** the feed Aggregator is running with Binance connected

**When** `Aggregator.Book("binance")` is called

**Then** it returns the latest L2 snapshot (non-nil, non-empty slice of
`types.OrderBookLevel`)

---

**Given** the feed Aggregator is running with Kraken connected (no L2 support)

**When** `Aggregator.Book("kraken")` is called

**Then** it returns `nil` or an empty slice

---

### D3 — Executor uses real L2 when available

**Given** an opportunity where `BuyExchange = "binance"` and Binance has a non-empty
book

**When** the executor performs the VWAP walk to estimate fill cost

**Then** it walks the real L2 levels returned by `Aggregator.Book("binance")`, not
synthetic depth

---

**Given** an opportunity where `BuyExchange = "kraken"` (no L2 available)

**When** the executor performs the VWAP walk

**Then** it falls back to synthetic depth generation — behavior identical to the
current pre-PR2 implementation

---

### D4 — Bybit snapshot-only constraint

**Given** a Bybit `orderbook.50.BTCUSDT` WebSocket feed

**When** a frame with `type = "snapshot"` is received

**Then** the Bybit connector replaces its full L2 book with all levels from that
snapshot (up to 50 bids and 50 asks)

---

**Given** the same Bybit feed

**When** a frame with `type = "delta"` is received

**Then** the connector ignores it — the L2 book is not modified; BBO continues to be
updated from the existing channel

---

### D5 — /api/health has_l2 field

**Given** `GET /api/health` is called after PR2 lands

**When** the response JSON is inspected per exchange

**Then** `has_l2` is `true` for `binance`, `bybit`, and `okx`; `has_l2` is `false` for
the seven exchanges that do not implement `BookConnector`

---

**Given** an existing `/api/health` consumer

**When** PR2 lands

**Then** `has_l2` is an additive field — no existing field is renamed or removed

---

## Capability NR: No-Regression

### NR1 — Baseline tests after PR1

**Given** the test suite after PR1 lands

**When** `go test ./...` is run

**Then** all 184 pre-existing tests pass; no existing test is skipped or removed

---

### NR2 — Baseline tests after PR2

**Given** the test suite after PR2 lands (count grows with new feature tests)

**When** `go test ./...` is run

**Then** all tests — baseline plus new — pass; no test is skipped or removed

---

### NR3 — No new external dependencies

**Given** `go.mod` and `vendor/` after both PRs land

**When** they are inspected

**Then** no package that was not already vendored before this change has been added

---

### NR4 — Backward-compatible imbalance default

**Given** `ImbalancePenaltyWeight` is left at its default value of `0.15`

**When** the spatial scorer processes an opportunity

**Then** the scoring behavior is consistent with the documented formula; existing
fixtures that do not set book sizes explicitly still pass (they use balanced or nil
books which yield penalty = 0)

---

## API Contract Addendum (non-breaking)

After PR1 and PR2 land, `GET /api/health` gains the following additive fields per
exchange entry. No field is renamed or removed.

| Field | Type | Present after | Semantics |
|---|---|---|---|
| `parse_p50_us` | integer | PR1 | P50 WS parse latency in microseconds; 0 when < 10 samples |
| `parse_p99_us` | integer | PR1 | P99 WS parse latency in microseconds; 0 when < 10 samples |
| `has_l2` | bool | PR2 | true when the exchange implements BookConnector |

---

## Out of scope (explicit exclusions)

The following are NOT covered by this spec and must not be verified against it:

- Inbound WS compression on connector dialers
- Bybit delta frame application to the L2 book
- Real L2 depth for Kraken, Gate, MEXC, Bitget, HTX, Crypto.com, KuCoin
- Frontend visualization of per-exchange parse latency
- L2 order book panel or spread chart integration
