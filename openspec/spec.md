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

## Cross-Cutting: Strict TDD Notes

All unit tests MUST use `go test ./...`. Tests MUST be in `_test.go` files alongside the package under test. Integration tests for the HTTP layer MUST use `net/http/httptest`. Race detection MUST be enabled (`-race`) for all concurrent components.

The ready flag, circuit breaker state transitions, and wallet debit atomicity are the three highest-risk behaviors — write these tests FIRST before any implementation.
