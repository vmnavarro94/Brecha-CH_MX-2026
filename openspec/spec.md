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

## 11. Order Book Depth (`internal/depth/`, executor refactor)

### Requirement D1: Synthetic ask book generated from BBO

The system MUST generate N ask levels starting at the BBO ask price, each step `StepPct` worse than the previous, with randomized quantities in `[MinLevelQty, MaxLevelQty]`.

#### Scenario: Five ask levels from BBO

- GIVEN bbo_ask=50000, step_pct=0.0001, levels=5
- WHEN `AskLevels` is called
- THEN the returned slice has exactly 5 entries
- AND prices are [50000, 50005, 50010, 50015, 50020]

#### Scenario: Quantities within configured bounds

- GIVEN min_level_qty=0.005, max_level_qty=0.025
- WHEN `AskLevels` is called with any BBO
- THEN every level quantity satisfies 0.005 <= qty <= 0.025

**Test strategy**: unit — deterministic seed, verify price progression and qty bounds.

---

### Requirement D2: Deterministic generation with seeded source

The system MUST produce identical level prices and quantities when given the same `rand.Source` seed, BBO, and config.

#### Scenario: Same seed reproduces same book

- GIVEN a fixed seed S, bbo_ask=50000, and depth config C
- WHEN `AskLevels` is called twice with a freshly-seeded source each time
- THEN both calls return byte-identical slices

**Test strategy**: unit — same seed, assert exact value equivalence.

---

### Requirement D3: Walk-the-book accumulates volume and computes VWAP

The system MUST iterate levels in order, accumulate filled quantity up to target, and return the volume-weighted average price of consumed liquidity.

#### Scenario: Single level fully covers target

- GIVEN levels=[(50000, 0.025), (50005, 0.05)], target=0.02
- WHEN `Walk` is called
- THEN filled=0.02, vwap=50000.00, partial=false

#### Scenario: Two levels required, exact fill

- GIVEN levels=[(50000, 0.025), (50005, 0.05)], target=0.05
- WHEN `Walk` is called
- THEN filled=0.05, vwap=50002.50, partial=false

#### Scenario: Liquidity exhausted before target reached

- GIVEN levels=[(50000, 0.025), (50005, 0.05)], target=0.10
- WHEN `Walk` is called
- THEN filled=0.075, vwap≈50003.33, partial=true

**Test strategy**: unit — fixed price + qty, assert VWAP and partial flag.

---

### Requirement D4: Trade prices hold VWAP fill price

The system MUST set `Trade.BuyPrice` to the buy-side VWAP and `Trade.SellPrice` to the sell-side VWAP after walking the respective books.

#### Scenario: Buy price reflects VWAP

- GIVEN a walk-the-book result with vwap_buy=50002.50
- WHEN the executor records the trade
- THEN Trade.BuyPrice=50002.50

#### Scenario: Sell price reflects VWAP

- GIVEN a walk-the-book result with vwap_sell=50297.50
- WHEN the executor records the trade
- THEN Trade.SellPrice=50297.50

**Test strategy**: unit — seeded depth config, assert Trade fields against known VWAP.

---

### Requirement D5: Partial fill flag set on liquidity exhaustion

The system MUST set `Trade.PartialFill=true` and `Trade.RequestedVolume` to the originally requested quantity whenever available liquidity is less than requested.

#### Scenario: Partial fill recorded

- GIVEN requested=0.10 BTC, available_liquidity=0.075 BTC
- WHEN the executor walks the book
- THEN Trade.PartialFill=true
- AND Trade.RequestedVolume=0.10
- AND Trade.Volume=0.075

#### Scenario: Full fill leaves flag false

- GIVEN requested=0.02 BTC, available_liquidity >= 0.02 BTC
- WHEN the executor walks the book
- THEN Trade.PartialFill=false
- AND Trade.Volume=0.02

**Test strategy**: unit — liquidity-constrained depth config, assert flag + requested vs filled volumes.

---

### Requirement D6: Slippage reflects walk cost vs BBO

The system MUST compute slippage as the combined cost of walking past BBO on both legs: `Slippage = (vwapBuy - bboAsk) + (bboBid - vwapSell)`.

#### Scenario: Two-leg slippage calculation

- GIVEN vwap_buy=50002.50, bbo_ask=50000, bbo_bid=50300, vwap_sell=50297.50
- WHEN slippage is computed
- THEN Slippage=5.00

**Test strategy**: unit — known VWAP, known BBO, verify formula.

---

### Requirement D7: Wallet reversal on sell failure uses VWAP cost

The system MUST credit the wallet with the exact VWAP-accumulated buy cost when a sell leg fails, not the BBO-estimated cost.

#### Scenario: Reversal uses VWAP cost

- GIVEN buy succeeded: vwap_buy=50002.50, filled=0.05 BTC (cost=2500.125 USDT)
- WHEN the sell leg fails
- THEN wallet is credited 2500.125 USDT
- AND the BBO-estimated cost of 2500.00 USDT is NOT used

**Test strategy**: unit — asymmetric fill test where vwap > bbo, assert reversal credit matches VWAP cost, not BBO.

---

### Requirement D8: Demo defaults produce partial fills within 60 seconds

The system SHOULD produce at least one partial fill within 60 seconds of bot start when running with default depth config (5 levels, qty range [0.005, 0.025] BTC).

#### Scenario: Partial fill occurs in demo mode

- GIVEN default depth config and BTC price ~74000 USDT, MaxPositionUSDT=1000
- WHEN the bot runs for 60 seconds
- THEN at least one Trade with PartialFill=true is recorded

**Test strategy**: integration — observe live runtime behavior or skip in unit testing.

---

### Requirement D9: No regression on existing tests

The system MUST pass all pre-existing tests without modification.

#### Scenario: Existing test suite passes

- GIVEN the codebase before this change has 65 passing tests
- WHEN the order-book-depth change is applied
- THEN all 65 tests still pass

**Test strategy**: `go test ./...` after all changes.

---

### Requirement D10: Backward-compatible trade deserialization

The system MUST deserialize Trade records persisted before this change (without `partial_fill` or `requested_volume` JSON fields) using zero-values for missing fields.

#### Scenario: Old trade record loads cleanly

- GIVEN a SQLite row whose payload JSON has no `partial_fill` or `requested_volume` keys
- WHEN the row is decoded into `types.Trade`
- THEN Trade.PartialFill=false and Trade.RequestedVolume=0 with no error

**Test strategy**: unit — old-format JSON payloads, assert zero-valued fields on unmarshal.

---

## 12. Multi-Strategy Framework (`internal/strategy/`, engine refactor)

### Requirement M1: Strategy interface contract

The system MUST define a `strategy.Strategy` interface with exactly two methods: `Name() string` and `Detect(update, snapshot, now) []types.Opportunity`. Any type implementing this interface MUST be registerable with the engine without modifying engine internals.

#### Scenario: Empty Detect result produces no heap entries

- GIVEN a registered strategy whose Detect always returns `[]types.Opportunity{}`
- WHEN Engine.ProcessUpdate is called
- THEN no opportunity is added to the heap

#### Scenario: Two opportunities returned from Detect reach the heap

- GIVEN a registered strategy whose Detect returns 2 opportunities
- WHEN Engine.ProcessUpdate is called
- THEN both opportunities are present in the heap

#### Scenario: Fan-out across two registered strategies

- GIVEN two strategies registered on the same engine, each returning 1 opportunity
- WHEN Engine.ProcessUpdate is called once
- THEN 2 opportunities are in the heap (one per strategy)

**Test strategy**: unit — inject mock strategies, assert heap contents.

---

### Requirement M2: SpatialStrategy preserves byte-equivalent behavior

SpatialStrategy.Detect MUST produce results that are numerically identical to the pre-refactor engine behavior when given the same WS frames and config. No decimal arithmetic, ordering, or threshold comparisons MAY change during the extraction.

#### Scenario: Same input produces same trade

- GIVEN the same PriceUpdate sequence and FeeConfig that produced trade T pre-refactor
- WHEN the same sequence is processed through the refactored stack (SpatialStrategy + thin engine)
- THEN the resulting trade has identical NetProfit, Score, BuyExchange, and SellExchange as T

#### Scenario: Spread model updated regardless of profit

- GIVEN a PriceUpdate where both directions are below MinNetProfitPct
- WHEN Detect is called
- THEN the SpreadModel for that counterparty pair is still updated with the new sample

#### Scenario: Statistical scoring when model is ready

- GIVEN a spread model with IsReady=true, z_score=Z, net_pct=N, max_net_pct=M
- WHEN Score is computed inside Detect
- THEN Score = (N/M)*0.6 + sigmoid(Z)*0.4

**Test strategy**: unit — deterministic PriceUpdate sequences, assert opportunity field byte-equivalence.

---

### Requirement M3: SpatialStrategy stamps opportunities with strategy name

Every opportunity returned by SpatialStrategy.Detect MUST have `Strategy == "spatial"`. The engine MUST NOT overwrite this value.

#### Scenario: Detect stamps Strategy field

- GIVEN SpatialStrategy.Detect produces one opportunity
- WHEN the opportunity is inspected
- THEN opp.Strategy == "spatial"

**Test strategy**: unit — call Detect, assert Strategy field on returned opps.

---

### Requirement M5: SpatialStrategy typed setters are race-safe

SpatialStrategy MUST expose `SetMinNetProfitPct(float64)`, `SetFees(map[string]FeeConfig)`, and `SetMaxPositionUSDT(float64)`. Each setter MUST acquire a write lock before mutating internal state. Detect MUST acquire a read lock while reading the same fields.

#### Scenario: SetMinNetProfitPct takes effect on next Detect

- GIVEN SpatialStrategy configured with MinNetProfitPct=0.002
- WHEN SetMinNetProfitPct(0.001) is called and Detect is invoked with a spread that yields net_pct=0.0015
- THEN the opportunity is returned (threshold is now 0.001, not 0.002)

#### Scenario: SetFees takes effect on next Detect

- GIVEN fees configured with taker_fee=0.001
- WHEN SetFees is called with taker_fee=0.002 and Detect runs
- THEN cost calculations use taker_fee=0.002

#### Scenario: Concurrent setter and Detect do not race

- GIVEN SetFees is called from goroutine A while Detect is running in goroutine B
- WHEN both complete
- THEN the Go race detector reports no data race

**Test strategy**: unit with `-race` flag — concurrent goroutines on setters and Detect simultaneously.

---

### Requirement M6: Strategy field propagated through Opportunity and Trade

`types.Opportunity` and `types.Trade` MUST each contain `Strategy string \`json:"strategy,omitempty"\``. The executor MUST copy `opp.Strategy` into `trade.Strategy` before persisting.

#### Scenario: Executor propagates strategy name

- GIVEN an opportunity with Strategy="spatial" enters the executor
- WHEN execution completes successfully
- THEN trade.Strategy == "spatial"

#### Scenario: Trade JSON includes strategy field

- GIVEN a trade with Strategy="spatial"
- WHEN serialized to JSON
- THEN the payload contains `"strategy":"spatial"`

#### Scenario: Old trade with no strategy field deserializes cleanly

- GIVEN a SQLite row whose JSON payload has no "strategy" key
- WHEN decoded into types.Trade
- THEN Trade.Strategy == "" with no error

**Test strategy**: unit — backward-compat JSON decode with missing field, assert zero-value.

---

### Requirement M7: /api/pnl-by-strategy aggregates by strategy name

The endpoint MUST group all trades by `Trade.Strategy`, sum `NetProfit`, count trades, and return rows sorted by `total_pnl` descending. A trade with `Strategy==""` MUST be grouped under key `"unknown"`.

#### Scenario: Three trades same strategy

- GIVEN 3 trades each with Strategy="spatial" and NetProfit values P1, P2, P3
- WHEN GET /api/pnl-by-strategy is called
- THEN response contains 1 row: `{strategy:"spatial", trade_count:3, total_pnl:P1+P2+P3}`

#### Scenario: Two strategies sorted by total_pnl desc

- GIVEN 2 trades with Strategy="spatial" (sum=10) and 1 trade with Strategy="triangular" (sum=20)
- WHEN GET /api/pnl-by-strategy is called
- THEN response has 2 rows, "triangular" first (total_pnl=20), "spatial" second (total_pnl=10)

#### Scenario: Legacy trade with empty strategy grouped as unknown

- GIVEN 1 trade with Strategy=""
- WHEN GET /api/pnl-by-strategy is called
- THEN response contains 1 row with strategy="unknown"

**Test strategy**: integration — construct store with trades, hit endpoint, assert grouping and sort order.

---

### Requirement M8: StrategyPnL panel renders per-strategy rows

The frontend MUST include a `StrategyPnL` panel that reads from the `/api/pnl-by-strategy` endpoint (or the equivalent Zustand store slice), and renders one row per strategy showing total P&L, trade count, and win rate. The panel MUST be a sibling of the per-pair P&L panel, not a column inside it.

#### Scenario: Panel renders row for existing strategy

- GIVEN the store contains trades with strategy="spatial"
- WHEN StrategyPnL renders
- THEN a row for "spatial" is visible with total P&L and trade count populated

#### Scenario: Empty state message when no trades

- GIVEN the store has zero trades
- WHEN StrategyPnL renders
- THEN an empty-state message is displayed and no rows are rendered

**Test strategy**: unit with React Testing Library — mock store, assert render output.

---

### Requirement M10: No regression on existing tests

The refactored codebase MUST pass all 109 tests that existed before this change. The 12 engine tests covering spatial detection, scoring, and fees MUST be migrated to `internal/strategy/spatial/spatial_test.go` and continue to pass there.

#### Scenario: Full test suite green

- GIVEN the multi-strategy-framework change is fully applied
- WHEN `go test ./...` is run
- THEN all tests pass with no failures or data races

#### Scenario: engine.go logic budget

- GIVEN the refactored engine.go
- WHEN the file is inspected (excluding type definitions and trivial setters)
- THEN the logic line count does not exceed 60

**Test strategy**: integration with `go test ./... -race` after all changes; verify test count >= 127 (109 baseline + 18 new).

---

## 13. Triangular Arbitrage Strategy (`internal/strategy/triangular/`)

### Requirement TR1: Synthetic ETH/USDT seed per exchange

The system MUST seed an independent synthetic ETH/USDT reference price per exchange at startup in DemoMode with a base of 2000.0 USDT per ETH, offset by ±0.1% per-exchange noise. Each exchange MUST have a distinct reference value so that no two exchanges share the same seed.

#### Scenario: Independent per-exchange seeding

- GIVEN DemoMode is active and TriangularStrategy is initializing
- WHEN the strategy seeds the ETH/USDT reference
- THEN each exchange (e.g. binance, bybit, okx) receives a distinct value in range [1998.0, 2002.0]
- AND no two exchange references are equal at t=0

**Test strategy**: unit — deterministic seed, assert per-exchange refs differ.

---

### Requirement TR2: Bellman-Ford negative-cycle detection

The strategy MUST run Bellman-Ford on a 3-node directed graph {USDT, BTC, ETH} with edge weights = -log(rate) per exchange. An opportunity MUST be emitted when a negative cycle exists whose net gain after deducting 3 × taker_fee exceeds MinNetProfit. No opportunity MUST be emitted when the graph is balanced.

#### Scenario: Balanced graph — no opportunity

- GIVEN BTC/USDT = 50000, ETH/USDT = 2000, ETH/BTC = 0.04 (implied rate matches cross rate)
- WHEN Bellman-Ford is executed on the graph
- THEN no opportunity is emitted (cycle gain ≤ 3 × taker_fee)

#### Scenario: Divergent cross-rate — opportunity emitted

- GIVEN BTC/USDT = 50000, ETH/USDT = 2050, ETH/BTC = 0.04 (implied ETH/USDT via BTC = 2000, actual = 2050)
- WHEN Bellman-Ford detects a negative cycle
- THEN one opportunity is emitted with NetProfit > 0
- AND NetProfit = (cycle_gain - 3 × taker_fee) × notional

**Test strategy**: unit — inject fixture rates, verify cycle detection and cost model.

---

### Requirement TR3: Opportunity attribution

Opportunities produced by TriangularStrategy MUST carry `Strategy = "triangular"` and MUST set `BuyExchange == SellExchange` (intra-exchange cycle).

#### Scenario: Strategy and exchange fields

- GIVEN a detected negative cycle on exchange "binance"
- WHEN Detect returns the opportunity
- THEN opp.Strategy == "triangular"
- AND opp.BuyExchange == "binance" AND opp.SellExchange == "binance"

**Test strategy**: unit — call Detect, assert Strategy and exchange stamping.

---

### Requirement TR4: Configurable parameters with thread-safe setters

TriangularStrategy MUST expose TakerFee, NoisePct (NoiseRange), and RefreshEvery via its Config. SetTakerFee MUST be safe for concurrent use (RWMutex protected).

#### Scenario: Concurrent fee update

- GIVEN TriangularStrategy is running Detect concurrently
- WHEN SetTakerFee is called from another goroutine
- THEN no data race occurs and the next Detect call uses the updated fee

**Test strategy**: unit with `-race` — concurrent Detect + SetTakerFee from different goroutines.

---

## 14. Funding Rate Strategy (`internal/strategy/funding/`)

### Requirement FR1: Side-goroutine poller with context respect

FundingStrategy.Start(ctx) MUST launch a goroutine that refreshes the internal rate cache every 30s. The goroutine MUST exit cleanly when ctx.Done() is closed. Without calling Start, Detect MUST return an empty slice.

#### Scenario: Goroutine exits on cancellation

- GIVEN Start(ctx) has been called and the goroutine is polling
- WHEN ctx is cancelled
- THEN the goroutine exits without leak within one poll interval

#### Scenario: Detect without Start returns empty

- GIVEN a FundingStrategy that has never had Start called
- WHEN Detect is invoked
- THEN the result is an empty slice

**Test strategy**: unit — call Detect without Start (empty result), call Start then cancel ctx (verify exit), assert no goroutine leaks.

---

### Requirement FR2: Funding differential detection

Detect MUST emit an opportunity when the absolute differential between any two venues exceeds the configured Threshold. The venue with lower funding MUST be BuyExchange; the venue with higher funding MUST be SellExchange.

#### Scenario: Differential above threshold

- GIVEN binance funding = +0.02%, bybit funding = -0.005% (differential 0.025% > 0.015% threshold)
- WHEN Detect is called
- THEN an opportunity is emitted with BuyExchange = "bybit", SellExchange = "binance"
- AND opp.NetProfit = (0.02 - (-0.005)) × notional

#### Scenario: Differential below threshold — no emission

- GIVEN all venue funding differentials ≤ threshold
- WHEN Detect is called
- THEN result is empty

**Test strategy**: unit — inject rates manually, call Detect, assert emission and field values.

---

### Requirement FR3: lastEmitAt cooldown guard

Detect MUST NOT emit an opportunity for the same venue pair if the last emission for that pair was within the configured Cooldown duration (default 5s).

#### Scenario: Duplicate within cooldown suppressed

- GIVEN an opportunity was emitted for (bybit, binance) at T=0
- WHEN Detect is called again at T=3s with the same differential
- THEN result is empty

#### Scenario: Emission allowed after cooldown

- GIVEN the last emission for (bybit, binance) was at T=0
- WHEN Detect is called at T=6s (> 5s cooldown)
- THEN a new opportunity is emitted

**Test strategy**: unit — use injected clock, emit at T=0, suppress at T=3s, emit at T=6s.

---

### Requirement FR4: Strategy stamping and NetProfit

Opportunities from FundingStrategy MUST carry `Strategy = "funding"`. NetProfit MUST equal (funding_high - funding_low) × notional (single-shot model, no 8h normalization).

#### Scenario: Fields on emitted opportunity

- GIVEN a qualifying funding differential between bybit and binance
- WHEN Detect emits an opportunity
- THEN opp.Strategy == "funding"
- AND opp.NetProfit == (funding_high - funding_low) × notional

**Test strategy**: unit — call Detect with injected rates, verify Strategy field and NetProfit calculation.

---

## 15. Starter Interface (`internal/strategy/`)

### Requirement ST1: Starter interface definition

The package `internal/strategy` MUST define `type Starter interface { Start(ctx context.Context) error }`. FundingStrategy MUST implement Starter. TriangularStrategy and SpatialStrategy MUST NOT be required to implement Starter.

#### Scenario: Interface satisfaction

- GIVEN the Starter interface is defined
- WHEN FundingStrategy is compiled
- THEN `var _ strategy.Starter = (*FundingStrategy)(nil)` compiles without error

**Test strategy**: compile-time assertion in tests.

---

### Requirement ST2: main.go Starter wiring

`cmd/server/main.go` MUST iterate the registered strategy slice, type-assert each to `strategy.Starter`, and call `Start(ctx)` on matches before the engine runs. Non-Starter strategies MUST be skipped silently.

#### Scenario: Start called on Starter strategies only

- GIVEN strategies = [SpatialStrategy, TriangularStrategy, FundingStrategy]
- WHEN startup wiring executes
- THEN FundingStrategy.Start(ctx) is called exactly once
- AND SpatialStrategy and TriangularStrategy are not started via the Starter path

**Test strategy**: integration — assert Start called exactly once on FundingStrategy, zero on others.

---

## 16. Funding Executor (`internal/executor/`)

### Requirement FE1: FundingExecutor records trades without wallet impact

FundingExecutor.Execute(opp) MUST return a Trade with Strategy = "funding", PartialFill = false, Volume = 1.0. It MUST NOT call wallet.Debit or wallet.Credit. Trade.NetProfit MUST equal opp.NetProfit.

#### Scenario: Trade fields and no wallet touch

- GIVEN a funding opportunity with NetProfit = 12.5
- WHEN FundingExecutor.Execute(opp) is called
- THEN trade.Strategy == "funding", trade.PartialFill == false, trade.Volume == 1.0
- AND trade.NetProfit == 12.5
- AND no wallet mutation occurs

**Test strategy**: unit — call Execute, assert Trade fields and wallet.Debit/Credit call count = 0.

---

### Requirement FE2: Strategy-based dispatch in runProcessingLoop [ADR-3 override]

`runProcessingLoop` MUST route opportunities by opp.Strategy using a switch statement:
- `"funding"` → FundingExecutor (no wallet mutation, records synthetic funding P&L)
- `"triangular"` → TriangularExecutor (no wallet mutation, records 3-leg fee bundle)
- `"spatial"` or any other value → SpotExecutor (two-venue Debit/Credit via MultiWallet)

NOTE: Design ADR-3 overrides the earlier guidance ("triangular uses SpotExecutor as default"). TriangularExecutor is dedicated because SpotExecutor's two-venue Debit/Credit model would corrupt MultiWallet for intra-exchange round-trips. This override is intentional.

#### Scenario: Funding routed to FundingExecutor

- GIVEN opp.Strategy == "funding"
- WHEN runProcessingLoop processes the opportunity
- THEN FundingExecutor.Execute is called and SpotExecutor.Execute is not

#### Scenario: Triangular routed to TriangularExecutor

- GIVEN opp.Strategy == "triangular"
- WHEN runProcessingLoop processes the opportunity
- THEN TriangularExecutor.Execute is called and SpotExecutor.Execute is not

#### Scenario: Spatial routed to SpotExecutor

- GIVEN opp.Strategy == "spatial" (or any unrecognized value)
- WHEN runProcessingLoop processes the opportunity
- THEN SpotExecutor.Execute is called

**Test strategy**: integration — dispatch matching opportunities via runProcessingLoop, assert correct executor invoked.

---

## 17. Dashboard Integration (`web/`)

### Requirement DI1: StrategyPnL panel shows 3 strategies

`/api/pnl-by-strategy` MUST return at least 3 rows (spatial, triangular, funding) with non-zero trade counts after 60s of DemoMode runtime. The frontend StrategyPnL component MUST render one row per strategy returned by the API.

#### Scenario: 3-row response after demo window

- GIVEN the bot has been running in DemoMode for 60 seconds
- WHEN GET /api/pnl-by-strategy is called
- THEN the response contains rows for "spatial", "triangular", and "funding"
- AND each row has trade_count > 0

**Test strategy**: integration — run bot for 60s in DemoMode, poll endpoint, assert 3 rows with trade_count > 0.

---

## 18. No Regression (`triangular-and-funding-rate-strategies`)

### Requirement NR1: All baseline tests pass with no new dependencies

All 127 existing tests MUST pass after the change. The build MUST be clean (tsc + npm run build). go.mod and package.json MUST NOT gain new external dependencies.

#### Scenario: Full test suite green

- GIVEN the change is applied
- WHEN `go test ./... -race` is executed
- THEN exit code is 0 with no failures or data races

#### Scenario: Frontend build clean

- WHEN `tsc` and `npm run build` are executed in the web directory
- THEN both exit with code 0 and produce no type errors

**Test strategy**: integration — `go test -race ./...` (assert >= 127 tests PASS), `tsc --noEmit` (assert exit 0), npm build (assert vite succeeds).

---

## 19. Backtest Engine (`internal/recorder/`, `internal/backtest/`)

### Requirement R1: Frame persistence

The `Recorder` MUST persist every `PriceUpdate` passed to `RecordFrame` as a row in the `frames` table of `frames.db`.

#### Scenario: Single frame round-trip

- GIVEN a `Recorder` opened against a test data directory
- WHEN `RecordFrame` is called with `PriceUpdate{Exchange:"binance", Bid:50000, Ask:50001, Ts:T}`
- THEN `QueryFrames(T-1s, T+1s)` returns exactly 1 frame with matching Exchange, Bid, Ask, and Ts values

#### Scenario: Ordering guarantee

- GIVEN 100 `RecordFrame` calls with timestamps spread over a 1-hour window
- WHEN `QueryFrames(T-1h, T+1h)` is called
- THEN the returned slice has length 100 and each element's Ts is less than or equal to the next element's Ts

**Test strategy**: unit — deterministic writes and reads, verify ordering.

---

### Requirement R2: Funding rate persistence

The `Recorder` MUST persist every funding rate passed to `RecordFundingRate` as a row in the `funding_rates` table of `frames.db`.

#### Scenario: Single rate round-trip

- GIVEN a `Recorder` opened against a test data directory
- WHEN `RecordFundingRate("binance", 0.0001, T)` is called
- THEN `QueryFundingRates(T-1s, T+1s)` returns exactly 1 entry with Exchange "binance", Rate 0.0001, and Ts T

#### Scenario: Range ordering

- GIVEN multiple `RecordFundingRate` calls with different timestamps
- WHEN `QueryFundingRates(from, to)` is called for a range covering all of them
- THEN the returned slice is sorted by Ts ascending

**Test strategy**: unit — multiple writes, verify ordering and retrieval.

---

### Requirement R3: Storage isolation and schema

The `Recorder` MUST open a file named `frames.db` that is separate from the main `trades.db`. The database MUST use WAL journal mode. An index on `frames.ts` and an index on `funding_rates.ts` MUST exist.

#### Scenario: Separate file

- GIVEN a running system with recording enabled
- WHEN the data directory is inspected
- THEN `frames.db` and `trades.db` are distinct files; writes to `frames.db` do not acquire locks on `trades.db`

#### Scenario: WAL mode active

- GIVEN a `Recorder` opened successfully
- WHEN `PRAGMA journal_mode` is queried on the `frames.db` connection
- THEN the result is `wal`

#### Scenario: Index present

- GIVEN a `Recorder` opened successfully
- WHEN the sqlite_master table is queried
- THEN an index on `frames(ts)` and an index on `funding_rates(ts)` are present

**Test strategy**: unit — schema inspection, verify WAL and indexes.

---

### Requirement RR1: Fresh strategy instances per run

`Runner.Run` MUST construct new strategy instances via the registered factories for each run. No strategy state from a previous run or from the live engine MAY be shared.

#### Scenario: SpatialStrategy isolation

- GIVEN a `Runner` with a SpatialStrategy factory
- WHEN `Run` is called
- THEN SpatialStrategy is instantiated with a new empty `SpreadModel` map; the live SpatialStrategy instance is not referenced

#### Scenario: FundingStrategy replay mode

- GIVEN a `Runner` with a FundingStrategy factory
- WHEN `Run` is called
- THEN the FundingStrategy instance receives pre-recorded rates via injection; its live poller MUST NOT be started

**Test strategy**: unit — factories test, verify isolation.

---

### Requirement RR2: ReplayClock advances with frames

`ReplayClock.Now()` MUST return the `ReceivedAt` timestamp of the frame currently being processed.

#### Scenario: Clock matches frame time

- GIVEN a replay loop processing frames with timestamps T1, T2, T3
- WHEN frame at T1 is processed
- THEN `ReplayClock.Now()` == T1
- AND when frame at T2 is processed, `ReplayClock.Now()` == T2

#### Scenario: Strategies see frame time as now

- GIVEN a strategy that records the clock value it observes when `Process` is called
- WHEN a frame with Ts=T is dispatched through the replay engine
- THEN the strategy observes `now` == T

**Test strategy**: unit — mock runner, verify clock advancement.

---

### Requirement RR3: Speed control

`Runner.Run` MUST throttle playback when `BacktestSpec.Speed > 0` and MUST run at maximum CPU speed when `Speed == 0`.

#### Scenario: Real-time speed

- GIVEN a recording with 10 seconds of source data at `Speed = 1.0`
- WHEN `Run` completes
- THEN elapsed wall time is between 9s and 11s (within 10% tolerance)

#### Scenario: Accelerated speed

- GIVEN a recording with 60 seconds of source data at `Speed = 10.0`
- WHEN `Run` completes
- THEN elapsed wall time is at most 7 seconds

#### Scenario: Maximum speed

- GIVEN a recording with any source data at `Speed = 0`
- WHEN `Run` completes
- THEN no artificial sleep is introduced; the run completes as fast as the CPU allows

**Test strategy**: integration — run with injected timing, verify elapsed time bounds.

---

### Requirement RR4: Single concurrent run guard

`Runner.Run` MUST return `ErrAlreadyRunning` (mapped to HTTP 409) if a run is already in progress. A second run MUST start cleanly once the first has finished.

#### Scenario: Concurrent rejection

- GIVEN a `Runner` with a run in progress
- WHEN a second `Run` call is made concurrently
- THEN the second call returns `ErrAlreadyRunning` immediately without waiting

#### Scenario: Sequential re-run

- GIVEN a `Runner` where a previous run has completed
- WHEN a new `Run` call is made
- THEN it starts successfully and returns a non-error result

**Test strategy**: unit — concurrent Run calls, verify 409 on contention.

---

### Requirement RR5: Determinism with same seed

Two `Run` calls with identical `BacktestSpec` (same `From`, `To`, `Seed`, and `Strategies`) against the same recording MUST produce byte-identical `RunResult.Metrics`.

#### Scenario: Reproducible metrics

- GIVEN the same recording window, same `Seed`, and same strategy set
- WHEN `Run` is called twice with the same spec
- THEN `RunResult.Metrics` values are identical across both runs

#### Scenario: FundingStrategy uses recorded rates

- GIVEN a replay run with FundingStrategy enabled
- WHEN the replay loop processes frames
- THEN FundingStrategy reads rate values from the pre-recorded `funding_rates` rows, not from any live network source

**Test strategy**: integration — two identical runs, assert metrics byte-equivalence.

---

### Requirement M1: Total P&L per strategy

`StrategyMetrics.TotalPnL` MUST equal the arithmetic sum of `NetProfit` across all trades for that strategy.

#### Scenario: Multiple trades

- GIVEN 3 trades on strategy "spatial" with NetProfit values 5, 3, and -2
- WHEN metrics are computed
- THEN `TotalPnL` == 6.0

#### Scenario: No trades

- GIVEN 0 trades for a strategy
- WHEN metrics are computed
- THEN `TotalPnL` == 0.0

**Test strategy**: unit — construct trades, verify sum.

---

### Requirement M2: Max drawdown on equity curve

`StrategyMetrics.MaxDrawdown` MUST equal the maximum peak-to-trough decline on the cumulative P&L equity curve.

#### Scenario: Drawdown calculation

- GIVEN cumulative P&L sequence [10, 15, 8, 12, 5]
- WHEN metrics are computed
- THEN `MaxDrawdown` == 10.0 (peak 15, trough 5)

#### Scenario: Monotonically increasing equity

- GIVEN a cumulative P&L sequence that is strictly non-decreasing
- WHEN metrics are computed
- THEN `MaxDrawdown` == 0.0

**Test strategy**: unit — known equity curve, verify drawdown formula.

---

### Requirement M3: Sharpe ratio

`StrategyMetrics.Sharpe` MUST be computed as `mean(returns) / std(returns) * sqrt(annualization_factor)` where `annualization_factor` is derived from observed trades-per-second over the replay window. MUST return 0 when fewer than 2 trades exist.

#### Scenario: Valid Sharpe

- GIVEN 10 trades with per-trade returns r
- WHEN metrics are computed
- THEN `Sharpe == mean(r) / std(r) * sqrt(annualization_factor)` within floating-point tolerance

#### Scenario: Insufficient data

- GIVEN 0 or 1 trade for a strategy
- WHEN metrics are computed
- THEN `Sharpe` == 0.0

**Test strategy**: unit — deterministic trade set, verify Sharpe formula.

---

### Requirement M4: Hit rate

`StrategyMetrics.HitRate` MUST equal `winning_trades / total_trades`. MUST return 0 when `total_trades` is 0.

#### Scenario: Partial win rate

- GIVEN 4 winning trades and 6 losing trades
- WHEN metrics are computed
- THEN `HitRate` == 0.4

#### Scenario: No trades

- GIVEN 0 trades
- WHEN metrics are computed
- THEN `HitRate` == 0.0

**Test strategy**: unit — construct trades with mixed profitability, verify ratio.

---

### Requirement M5: Profit factor

`StrategyMetrics.ProfitFactor` MUST equal `sum(positive NetProfit) / abs(sum(negative NetProfit))`. MUST return 999 when there are no losing trades with non-zero losses. MUST return 0 when there are no trades.

#### Scenario: Normal profit factor

- GIVEN trades with sum of positive NetProfit == 15 and sum of negative NetProfit == -5
- WHEN metrics are computed
- THEN `ProfitFactor` == 3.0

#### Scenario: No losing trades

- GIVEN trades where all NetProfit values are positive
- WHEN metrics are computed
- THEN `ProfitFactor` == 999 (capped infinity)

#### Scenario: No trades

- GIVEN 0 trades
- WHEN metrics are computed
- THEN `ProfitFactor` == 0.0

**Test strategy**: unit — trades with various profitability, verify formula.

---

### Requirement API1: POST /api/backtest/start

The endpoint MUST accept a JSON body `{from, to, speed, strategies, seed}` and return 202 with `{run_id}` on success. It MUST return 409 if a run is in progress, and 400 for an invalid spec.

#### Scenario: Successful start

- GIVEN no run is in progress and a valid spec body
- WHEN `POST /api/backtest/start` is called
- THEN response status is 202 and body contains a non-empty `run_id`

#### Scenario: Conflict

- GIVEN a run is currently in progress
- WHEN `POST /api/backtest/start` is called
- THEN response status is 409 and body contains `{"error":"backtest already running"}`

#### Scenario: Invalid spec

- GIVEN a body where `from` is after `to`
- WHEN `POST /api/backtest/start` is called
- THEN response status is 400

**Test strategy**: integration — httptest server, POST with various payloads, verify status codes.

---

### Requirement API2: GET /api/backtest/status

The endpoint MUST return current run state including `state`, `progress` (0..1), and `current_ts` when running. MUST return `{state:"idle"}` when no run is active.

#### Scenario: Run in progress

- GIVEN a run is in progress and 45% of frames have been processed
- WHEN `GET /api/backtest/status` is called
- THEN response contains `state: "running"` and `progress` approximately 0.45

#### Scenario: No active run

- GIVEN no run is in progress
- WHEN `GET /api/backtest/status` is called
- THEN response contains `state: "idle"`

**Test strategy**: integration — mock runner, poll status, verify JSON structure.

---

### Requirement API3: GET /api/backtest/runs

The endpoint MUST return all historical run entries sorted by `started_at` descending.

#### Scenario: Multiple runs

- GIVEN 3 completed runs stored in `backtest_runs`
- WHEN `GET /api/backtest/runs` is called
- THEN response contains 3 entries ordered by `started_at` DESC

**Test strategy**: integration — seed DB with runs, verify ordering.

---

### Requirement API4: GET /api/backtest/results/:run_id

The endpoint MUST return the `RunResult` with per-strategy metrics for a completed run. MUST return 404 for an unknown run ID.

#### Scenario: Known run

- GIVEN a completed run with `run_id` R
- WHEN `GET /api/backtest/results/R` is called
- THEN response contains `run_id: R` and a `metrics` map with entries for each strategy that was run

#### Scenario: Unknown run

- GIVEN no run with the requested ID exists
- WHEN `GET /api/backtest/results/:run_id` is called
- THEN response status is 404

**Test strategy**: integration — save run, retrieve by ID, assert metrics structure.

---

### Requirement API5: POST /api/backtest/recording

The endpoint MUST toggle frame and funding-rate recording. `{enabled: true}` MUST start recording; `{enabled: false}` MUST stop it.

#### Scenario: Enable recording

- GIVEN recording is disabled
- WHEN `POST /api/backtest/recording` with body `{"enabled": true}` is called
- THEN response contains `{"recording": true}` and subsequent `RecordFrame` calls write to `frames.db`

#### Scenario: Disable recording

- GIVEN recording is enabled
- WHEN `POST /api/backtest/recording` with body `{"enabled": false}` is called
- THEN response contains `{"recording": false}` and subsequent engine updates produce no DB writes

**Test strategy**: integration — POST toggle, verify flag state via store.

---

### Requirement FP1: BacktestPanel renders controls

`BacktestPanel` MUST render date/time inputs for `from` and `to`, a speed control, strategy checkboxes or multi-select, a seed input, and a "Run" button.

#### Scenario: Control presence

- GIVEN the `BacktestPanel` component is rendered in isolation
- WHEN the component tree is inspected
- THEN date inputs for "from" and "to", a speed control, strategy selectors, a seed input, and a Run button are all present in the DOM

**Test strategy**: unit with React Testing Library — render component, verify DOM presence.

---

### Requirement FP2: BacktestPanel shows run history

`BacktestPanel` MUST display historical runs returned by `GET /api/backtest/runs` in a table.

#### Scenario: History table

- GIVEN `GET /api/backtest/runs` returns 3 entries
- WHEN `BacktestPanel` fetches and renders the data
- THEN the history table contains 3 rows

**Test strategy**: unit with React Testing Library — mock fetch, verify table rows.

---

### Requirement FP3: Results display per strategy

Selecting a completed run MUST display per-strategy metrics (TotalPnL, Sharpe, MaxDrawdown, HitRate, TradeCount).

#### Scenario: Metrics table populated

- GIVEN a completed run with metrics for strategies "spatial" and "triangular"
- WHEN the user selects that run in the history table
- THEN the panel displays TotalPnL, Sharpe, MaxDrawdown, HitRate, and TradeCount for each strategy

**Test strategy**: unit with React Testing Library — mock data, render metrics table, verify structure.

---

### Requirement NR1: Baseline test suite passes

All 151 pre-existing tests MUST continue to pass after the change is applied. New tests added for backtest capabilities MUST number at least 20.

#### Scenario: Baseline green

- GIVEN the full test suite is run after all changes are applied
- WHEN `go test ./...` completes
- THEN all 151 previously passing tests pass and at least 20 new tests covering recorder, runner, metrics, and factories also pass

**Test strategy**: integration — `go test ./... -race` after all changes, assert count >= 171.

---

### Requirement NR2: Live engine unchanged

The live `StrategyPnL` panel MUST continue to display live spatial, triangular, and funding strategy data when recording is disabled.

#### Scenario: Live PnL unaffected

- GIVEN the system is running with recording disabled
- WHEN the live WebSocket stream is active
- THEN `StrategyPnL` receives updates for all three strategies as before the change

**Test strategy**: integration — run with recording off, verify live data flow.

---

### Requirement NR3: No new external dependencies

No new Go modules outside the current `go.mod` MAY be introduced. Only `modernc.org/sqlite` (already vendored) and stdlib are permitted.

#### Scenario: Dependency check

- GIVEN all new packages are implemented
- WHEN `go mod tidy` and `go mod vendor` are run
- THEN `go.mod` and `vendor/` contain no new third-party modules

**Test strategy**: integration — diff `go.mod` against baseline, verify no new requires.

---

## Cross-Cutting: Strict TDD Notes

All unit tests MUST use `go test ./...`. Tests MUST be in `_test.go` files alongside the package under test. Integration tests for the HTTP layer MUST use `net/http/httptest`. Race detection MUST be enabled (`-race`) for all concurrent components.

The ready flag, circuit breaker state transitions, and wallet debit atomicity are the three highest-risk behaviors — write these tests FIRST before any implementation.
