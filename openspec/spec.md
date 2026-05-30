# Spec: coding-challenge-mexico

Change: coding-challenge-mexico
Status: new (all domains are NEW — no existing specs to delta against)
TDD Mode: STRICT — every requirement is annotated with test strategy

---

## Already Implemented (not re-specced)

- `internal/types/types.go` — PriceUpdate, Opportunity, Trade, OpportunityStatus
- `config/config.go` — env var loading
- `internal/exchange/` — Binance, Kraken, Bybit WS connectors with exponential-backoff reconnect
- `internal/feed/aggregator.go` — fan-out aggregator

---

## 1. Spread Model (`internal/model/`)

### Requirement: Welford Online Statistics

The model MUST maintain a running mean and variance per exchange pair using the Welford one-pass algorithm, updating on every new spread sample without storing the full history.

#### Scenario: First sample initializes state

- GIVEN a SpreadModel with no samples for pair (A, B)
- WHEN a spread value s is ingested
- THEN Mean = s, Variance = 0, Count = 1

#### Scenario: Subsequent samples converge

- GIVEN a SpreadModel with N >= 2 samples
- WHEN another sample is ingested
- THEN Mean and Variance are updated incrementally without recomputing from scratch
- AND the result is numerically equivalent to the batch computation within float64 tolerance

**Test strategy**: unit — deterministic numeric sequence, compare against reference batch stats.

---

### Requirement: Ring Buffer Window

The model MUST limit its sample history to the most recent 500 samples using a fixed-size ring buffer. Samples older than the window boundary MUST be evicted and their contribution removed from the running statistics.

#### Scenario: Buffer not yet full

- GIVEN a ring buffer with fewer than 500 entries
- WHEN a new sample is added
- THEN the buffer grows by one and no eviction occurs

#### Scenario: Buffer at capacity triggers eviction

- GIVEN a ring buffer at 500 entries
- WHEN sample 501 arrives
- THEN the oldest sample is evicted, the buffer stays at 500, and Mean/Variance are updated correctly

**Test strategy**: unit — fill to capacity + 1, assert count remains 500 and oldest value is gone.

---

### Requirement: Ready Flag

The model MUST NOT produce a z-score until at least 100 samples have been collected (MinSamples = 100). Before that threshold the model MUST report IsReady = false.

#### Scenario: Below threshold

- GIVEN 99 samples ingested
- WHEN IsReady is queried
- THEN it returns false

#### Scenario: At threshold

- GIVEN exactly 100 samples ingested
- WHEN IsReady is queried
- THEN it returns true

**Test strategy**: unit — feed exactly 99 then 100 samples, assert state transitions.

---

### Requirement: Z-Score Computation

When IsReady is true the model MUST return z = (sample - mean) / stddev. When stddev = 0 the model MUST return z = 0.

#### Scenario: Normal computation

- GIVEN IsReady = true, mean = 10.0, stddev = 2.0
- WHEN spread 14.0 is scored
- THEN z-score = 2.0

#### Scenario: Zero variance guard

- GIVEN all samples are identical (stddev = 0)
- WHEN ZScore is called
- THEN it returns 0.0 without a divide-by-zero panic

**Test strategy**: unit — pure math, no I/O.

---

## 2. Arbitrage Engine (`internal/engine/`)

### Requirement: O(N) Opportunity Detection

On every PriceUpdate the engine MUST scan all N exchange pairs exactly once. For each pair (A, B) it MUST evaluate both directions (buy-A-sell-B and buy-B-sell-A).

#### Scenario: Detectable spread triggers opportunity

- GIVEN Ask(A) = 100, Bid(B) = 101, fee_A = 0.001, fee_B = 0.001, slippage_A = 0.0002
- WHEN the engine processes a PriceUpdate
- THEN an Opportunity is created with NetProfit = Bid(B) - Ask(A) - Ask(A)*fee_A - Bid(B)*fee_B - Ask(A)*slippage_A

#### Scenario: Sub-threshold spread is discarded

- GIVEN net profit < MinNetProfitPct (0.15%)
- WHEN detection runs
- THEN no Opportunity is emitted

**Test strategy**: unit — inject deterministic PriceUpdates, assert Opportunity fields match formula.

---

### Requirement: Opportunity Scoring

Every detected Opportunity MUST receive a Score computed as:
`Score = (net_pct / max_net_pct) * 0.6 + sigmoid(z_score) * 0.4`
When the spread model is not ready (IsReady = false), Score MUST fall back to net_pct only.

#### Scenario: Model ready

- GIVEN model IsReady = true, z_score = 1.5, net_pct = 0.003, max_net_pct = 0.005
- WHEN Score is computed
- THEN Score = (0.003/0.005)*0.6 + sigmoid(1.5)*0.4

#### Scenario: Model not ready fallback

- GIVEN model IsReady = false
- WHEN Score is computed
- THEN Score = net_pct (no z-score component)

**Test strategy**: unit — inject mock model returning IsReady true/false, assert Score formula.

---

### Requirement: Priority Heap

The engine MUST maintain a max-heap of pending Opportunities ordered by Score descending. The heap MUST evict entries whose age exceeds OpportunityTTL (500ms).

#### Scenario: Expired opportunity is evicted before processing

- GIVEN an Opportunity with DetectedAt more than 500ms in the past is at heap head
- WHEN the engine's execution tick fires
- THEN the expired Opportunity is dequeued and its status set to expired without execution

#### Scenario: Highest-score opportunity is dequeued first

- GIVEN two opportunities with scores 0.8 and 0.5 in the heap
- WHEN the execution tick fires
- THEN the 0.8 opportunity is dequeued first

**Test strategy**: unit with injected clock — insert two entries, advance time past TTL on one, assert ordering and eviction.

---

## 3. Risk Manager (`internal/risk/`)

### Requirement: Minimum Profit Threshold

The risk manager MUST reject any Opportunity whose NetProfitPct < MinNetProfitPct (0.0015). Rejected opportunities MUST be marked skipped.

#### Scenario: Below threshold

- GIVEN NetProfitPct = 0.001
- WHEN risk manager evaluates the opportunity
- THEN it returns false and the opportunity status is set to skipped

#### Scenario: At threshold

- GIVEN NetProfitPct = 0.0015
- WHEN risk manager evaluates the opportunity
- THEN it returns true (opportunity is approved)

**Test strategy**: unit — inject Opportunity structs, assert return value and status.

---

### Requirement: Circuit Breaker State Machine

The circuit breaker MUST implement three states: Active, Watching, and Paused. After N = 5 consecutive trades with net profit below the loss threshold (-0.5%), the breaker MUST transition to Paused for 5 minutes. A profitable trade while in Watching MUST reset the consecutive-loss counter.

#### Scenario: Transition Active -> Watching -> Paused

- GIVEN 5 consecutive trades each with NetProfit < -0.5%
- WHEN the 5th trade is recorded
- THEN state transitions to Paused and no new opportunities are approved until the 5-minute window expires

#### Scenario: Recovery resets counter

- GIVEN the breaker is in Watching with 3 consecutive losses
- WHEN a profitable trade is recorded
- THEN the consecutive-loss counter resets to 0 and state stays Active

#### Scenario: Paused state blocks execution

- GIVEN state = Paused and pause window has not expired
- WHEN risk manager evaluates any opportunity
- THEN it returns false regardless of profit

**Test strategy**: unit with injected clock — drive state transitions, assert state and counter values.

---

### Requirement: Maximum Position Size

The risk manager MUST reject any Opportunity whose required volume would exceed MaxPositionUSDT (1000 USDT).

#### Scenario: Oversized position rejected

- GIVEN volume * buy_price = 1001 USDT
- WHEN risk manager evaluates
- THEN it returns false

**Test strategy**: unit.

---

## 4. Execution Simulator (`internal/executor/`)

### Requirement: Price Freshness Check

The executor MUST NOT execute an opportunity if either side's last PriceUpdate is older than 2000ms at execution time. Stale opportunities MUST be marked expired.

#### Scenario: Stale price blocks execution

- GIVEN the most recent PriceUpdate for exchange A arrived 2100ms ago
- WHEN the executor attempts to execute
- THEN it returns an error, the opportunity is marked expired, and no wallet changes occur

#### Scenario: Fresh price allows execution

- GIVEN both sides' PriceUpdates arrived within 2000ms
- WHEN the executor attempts to execute
- THEN execution proceeds to the wallet debit/credit phase

**Test strategy**: unit with injected clock.

---

### Requirement: Volume Calculation

The executor MUST compute Volume = min(Opportunity.MaxVolume, walletBalance / currentAsk). Volume MUST be positive; if it rounds to zero the execution MUST be aborted with status skipped.

#### Scenario: Wallet-constrained volume

- GIVEN MaxVolume = 0.1 BTC, walletBalance = 50 USDT, currentAsk = 100000
- WHEN volume is computed
- THEN Volume = min(0.1, 50/100000) = 0.0005 BTC

#### Scenario: Zero volume aborts

- GIVEN walletBalance = 0 USDT
- WHEN volume is computed
- THEN execution is aborted with status skipped and no wallet change

**Test strategy**: unit — no I/O, pure arithmetic.

---

### Requirement: Wallet Debit/Credit

Upon successful execution the executor MUST atomically: debit USDT and credit BTC on the buy side, debit BTC and credit USDT on the sell side. Both sides MUST be applied together; partial application is not permitted.

#### Scenario: Successful trade updates both wallets

- GIVEN Volume = 0.01 BTC, BuyPrice = 99000, SellPrice = 100000
- WHEN execution completes
- THEN USDT wallet decreases by 0.01 * 99000, BTC wallet stays net zero (buy then sell), USDT wallet increases by 0.01 * 100000, net USDT gain = (100000 - 99000) * 0.01 minus fees

#### Scenario: Insufficient balance is caught before debit

- GIVEN walletBalance = 0 USDT
- WHEN execution is attempted
- THEN no debit/credit occurs and trade is not recorded

**Test strategy**: unit — mock wallet, assert balance changes match formula.

---

### Requirement: Trade Recording

The executor MUST create a Trade record with computed GrossProfit, Fees, NetProfit, and Slippage and persist it to the Opportunity Store.

#### Scenario: Trade persisted

- GIVEN a completed execution
- WHEN the executor finishes
- THEN a Trade with all fields populated is retrievable from the store

**Test strategy**: unit with mock store.

---

## 5. Wallet Manager (`internal/wallet/`)

### Requirement: Concurrent-Safe Balance Operations

The wallet MUST support concurrent reads and writes. Balance queries and updates MUST never produce a data race.

#### Scenario: Concurrent updates do not lose writes

- GIVEN 100 goroutines each adding 1 USDT concurrently
- WHEN all finish
- THEN balance = initial + 100 (no lost updates)

**Test strategy**: unit with `go test -race`.

---

### Requirement: Debit Atomicity

A Debit operation MUST fail atomically if the requested amount exceeds the current balance. It MUST NOT partially deduct.

#### Scenario: Insufficient balance

- GIVEN balance = 10 USDT
- WHEN Debit(15) is called
- THEN it returns an error and balance remains 10

**Test strategy**: unit.

---

## 6. Opportunity Store (`internal/store/`)

### Requirement: In-Memory Storage and Retrieval

The store MUST persist Opportunity and Trade records in memory and support retrieval by status and by ID.

#### Scenario: Store and retrieve by status

- GIVEN 3 opportunities with status detected and 2 with status executed
- WHEN QueryByStatus(executed) is called
- THEN exactly 2 opportunities are returned

#### Scenario: Retrieve by ID

- GIVEN an opportunity with ID "abc"
- WHEN GetByID("abc") is called
- THEN the exact opportunity is returned

**Test strategy**: unit.

---

### Requirement: Concurrent-Safe Operations

All store reads and writes MUST be safe for concurrent access without external locking by callers.

#### Scenario: Race-free concurrent inserts

- GIVEN 50 goroutines inserting opportunities simultaneously
- WHEN all finish
- THEN store count = 50 with no data race detected

**Test strategy**: unit with `go test -race`.

---

## 7. WebSocket Server (`internal/server/`)

### Requirement: Event Types and Delivery

The server MUST emit the following event types to all connected clients:

| Event              | Trigger      | Payload        |
|--------------------|--------------|----------------|
| price_update       | throttled 250ms | latest BBO per exchange |
| spread_stats       | throttled 250ms | mean, stddev, z-score per pair |
| opportunity        | immediate    | Opportunity struct |
| trade_executed     | immediate    | Trade struct |
| circuit_breaker    | immediate    | state change + reason |
| pnl_update         | on trade     | cumulative P&L |

#### Scenario: Throttled events are batched

- GIVEN 10 price updates arrive within a 250ms window
- WHEN the throttle timer fires
- THEN exactly 1 price_update message is sent containing the latest values

#### Scenario: Immediate event is sent without delay

- GIVEN a new opportunity is detected
- WHEN it is enqueued for broadcast
- THEN it is sent to all clients within one event-loop cycle (no throttle buffer)

**Test strategy**: unit with mock conn — inject events, assert message counts and timing.

---

### Requirement: Broadcast to All Clients

The server MUST broadcast each event to all currently connected clients. A slow or disconnected client MUST NOT block delivery to others.

#### Scenario: Slow client does not block fast client

- GIVEN two clients, one with a blocked write channel
- WHEN a broadcast occurs
- THEN the unblocked client receives the message and the blocked client is dropped or skipped

**Test strategy**: unit — mock connections with buffered channels.

---

## 8. REST API (`internal/server/`)

### Requirement: Six Endpoints

The server MUST expose the following HTTP endpoints:

| Method | Path | Response |
|--------|------|----------|
| GET | /api/status | system state, circuit breaker state, uptime |
| GET | /api/trades | paginated Trade list |
| GET | /api/opportunities | paginated Opportunity list filterable by status |
| GET | /api/pnl | cumulative and per-trade P&L summary |
| GET | /api/spreads | current spread stats per pair |

#### Scenario: /api/status reflects circuit breaker state

- GIVEN the circuit breaker is Paused
- WHEN GET /api/status is called
- THEN the response JSON includes `"circuit_breaker": "paused"`

#### Scenario: /api/trades returns paginated results

- GIVEN 25 trades in the store
- WHEN GET /api/trades?page=2&limit=10
- THEN 10 trades are returned starting from offset 10

#### Scenario: /api/opportunities filters by status

- GIVEN 5 detected and 3 executed opportunities
- WHEN GET /api/opportunities?status=executed
- THEN exactly 3 are returned

**Test strategy**: integration — spin up httptest.Server, assert response shape and status codes.

---

## 9. Latency Tracking (`internal/engine/latency.go`, `web/`)

### Requirement L1: Ring-buffer latency recording

The system MUST record the elapsed time of every ProcessUpdate call in a fixed-size 1024-slot ring buffer of int64 nanoseconds. When all 1024 slots are filled, subsequent writes MUST evict the oldest sample (circular overwrite). LatencyTracker.Record MUST be called exactly once per ProcessUpdate invocation, capturing T1 at entry and T2 just before return.

#### Scenario: Single update recorded

- GIVEN the LatencyTracker is freshly created
- WHEN one ProcessUpdate call completes
- THEN Stats() returns samples = 1 and p50 > 0

#### Scenario: Ring wraps at 1024

- GIVEN 1024 samples have been recorded
- WHEN one additional sample is recorded
- THEN the ring overwrites slot 0, samples remains 1024, and the oldest value is gone

**Test strategy**: unit — fill ring to capacity + 1, assert count remains 1024.

### Requirement L2: Exact percentile math

The system MUST compute p50 and p99 by copying the active window into a scratch slice, sorting ascending, and indexing sorted[n/2] (p50) and sorted[n*99/100] (p99) where n is the number of valid samples (up to 1024). Computation MUST NOT happen on the hot path — it MUST occur only when Stats() is called.

#### Scenario: Known ordered input

- GIVEN the ring contains exactly the values [1ns, 2ns, ..., 1024ns] in order
- WHEN Stats() is called
- THEN p50 = 512ns AND p99 = 1014ns

#### Scenario: Uniform input

- GIVEN all 1024 slots contain the same value V
- WHEN Stats() is called
- THEN p50 = V AND p99 = V

**Test strategy**: unit — feed deterministic input, verify nearest-rank percentile math.

### Requirement L3: WS latency_stats event

The system MUST publish a `latency_stats` WebSocket event at most once per second from the engine processing loop. The event payload MUST conform to: `{ type: "latency_stats", data: { p50_us: number, p99_us: number, samples: number } }`. The hub MUST throttle this event type at a minimum interval of 250ms.

#### Scenario: Event fires after warm-up

- GIVEN the bot has been running for at least 5 seconds in demo mode
- WHEN a connected frontend is present
- THEN at least one `latency_stats` event is received per second with p50_us > 0

#### Scenario: Hub throttle enforced

- GIVEN two latency_stats publishes occur within 250ms
- WHEN the hub dispatches to clients
- THEN only one event is delivered to each client in that window

**Test strategy**: unit with mock hub — assert event structure and throttle registration.

### Requirement L4: StatusBar latency display

The StatusBar MUST render the block `detect p50: X.Xµs | p99: Y.Yµs` when `latency.samples >= 10` in the frontend store. The block MUST be hidden when `latency.samples < 10`. Values MUST be formatted to one decimal place in µs.

#### Scenario: Samples sufficient

- GIVEN the store has latency.samples = 10 or more
- WHEN the StatusBar renders
- THEN the block "detect p50: X.Xµs | p99: Y.Yµs" is visible

#### Scenario: Cold start hidden

- GIVEN the store has latency.samples < 10
- WHEN the StatusBar renders
- THEN no latency block is visible

**Test strategy**: unit with React Testing Library — mock store with samples >= 10, assert render output.

### Requirement L5: Zero added dependencies

After implementation, the project MUST have no new entries in `go.mod` and no new entries in `package.json` compared to the pre-change baseline.

#### Scenario: go.mod unchanged

- GIVEN the implementation is complete
- WHEN `go.mod` is diffed against the pre-change version
- THEN no new `require` lines are present

#### Scenario: package.json unchanged

- GIVEN the implementation is complete
- WHEN `package.json` is diffed against the pre-change version
- THEN no new dependency or devDependency entries are present

**Test strategy**: integration — diff against baseline, assert no new imports.

### Requirement L6: Concurrency-safe Stats accessor

LatencyTracker.Stats() and LatencyTracker.Record() MUST be safe to call from different goroutines concurrently. The implementation MUST use sync.RWMutex: Record holds a write lock; Stats holds a read lock.

#### Scenario: No data race under concurrent access

- GIVEN Record() is called continuously in one goroutine
- WHEN Stats() is called from a different goroutine simultaneously
- THEN the Go race detector reports no data race

**Test strategy**: unit with `go test -race` — concurrent goroutines hammering Record and Stats simultaneously.

---

## 10. Frontend (`web/`)

### Requirement: Component Inventory

The frontend MUST include the following components:

| Component | Responsibility |
|-----------|----------------|
| PriceTable | Live BBO per exchange, color-coded updates |
| OpportunityFeed | Detected opportunities, sortable by score |
| TradeHistory | Executed trades with P&L per row |
| PnLChart | Cumulative P&L over time |
| SpreadChart | Z-score live line with ±1σ and ±2σ bands |
| StatusBar | Circuit breaker state, uptime, connection indicator |

#### Scenario: SpreadChart renders sigma bands

- GIVEN spread stats with mean = 50, stddev = 5
- WHEN SpreadChart renders
- THEN horizontal reference lines appear at 45, 55 (±1σ) and 40, 60 (±2σ)

#### Scenario: StatusBar reflects circuit breaker state

- GIVEN a circuit_breaker event with state = paused is received
- WHEN StatusBar re-renders
- THEN it displays a red "Circuit Breaker: PAUSED" indicator

**Test strategy**: unit with React Testing Library — mock WebSocket context, assert DOM output.

---

### Requirement: useMarketSocket Hook

The frontend MUST provide a `useMarketSocket` hook that connects to the WebSocket server, parses incoming events by type, distributes them to the correct Zustand store slices, and reconnects automatically on disconnect.

#### Scenario: Auto-reconnect on disconnect

- GIVEN the WebSocket closes unexpectedly
- WHEN the hook detects the close event
- THEN it attempts to reconnect after a backoff delay without requiring a page reload

#### Scenario: Event routing by type

- GIVEN a price_update message arrives
- WHEN the hook processes it
- THEN only the price slice of the Zustand store is updated

**Test strategy**: unit — mock WebSocket, dispatch events, assert store state.

---

## Cross-Cutting: Strict TDD Notes

All unit tests MUST use `go test ./...`. Tests MUST be in `_test.go` files alongside the package under test. Integration tests for the HTTP layer MUST use `net/http/httptest`. Race detection MUST be enabled (`-race`) for all concurrent components.

The ready flag, circuit breaker state transitions, and wallet debit atomicity are the three highest-risk behaviors — write these tests FIRST before any implementation.
