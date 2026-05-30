# Spec: triangular-and-funding-rate-strategies

## Capability: triangular-strategy

### Requirement TR1: Synthetic ETH/USDT seed per exchange

The system MUST seed an independent synthetic ETH/USDT reference price per exchange at startup in DemoMode with a base of 2000.0 USDT per ETH, offset by ±0.1% per-exchange noise. Each exchange MUST have a distinct reference value so that no two exchanges share the same seed.

#### Scenario: Independent per-exchange seeding

- GIVEN DemoMode is active and TriangularStrategy is initializing
- WHEN the strategy seeds the ETH/USDT reference
- THEN each exchange (e.g. binance, bybit, okx) receives a distinct value in range [1998.0, 2002.0]
- AND no two exchange references are equal at t=0

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

---

### Requirement TR3: Opportunity attribution

Opportunities produced by TriangularStrategy MUST carry `Strategy = "triangular"` and MUST set `BuyExchange == SellExchange` (intra-exchange cycle).

#### Scenario: Strategy and exchange fields

- GIVEN a detected negative cycle on exchange "binance"
- WHEN Detect returns the opportunity
- THEN opp.Strategy == "triangular"
- AND opp.BuyExchange == "binance" AND opp.SellExchange == "binance"

---

### Requirement TR4: Configurable parameters with thread-safe setters

TriangularStrategy MUST expose TakerFee, NoisePct (NoiseRange), and RefreshEvery via its Config. SetTakerFee MUST be safe for concurrent use (RWMutex protected).

#### Scenario: Concurrent fee update

- GIVEN TriangularStrategy is running Detect concurrently
- WHEN SetTakerFee is called from another goroutine
- THEN no data race occurs and the next Detect call uses the updated fee

---

## Capability: funding-rate-strategy

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

---

### Requirement FR4: Strategy stamping and NetProfit

Opportunities from FundingStrategy MUST carry `Strategy = "funding"`. NetProfit MUST equal (funding_high - funding_low) × notional (single-shot model, no 8h normalization).

#### Scenario: Fields on emitted opportunity

- GIVEN a qualifying funding differential between bybit and binance
- WHEN Detect emits an opportunity
- THEN opp.Strategy == "funding"
- AND opp.NetProfit == (funding_high - funding_low) × notional

---

## Capability: starter-interface

### Requirement ST1: Starter interface definition

The package `internal/strategy` MUST define `type Starter interface { Start(ctx context.Context) error }`. FundingStrategy MUST implement Starter. TriangularStrategy and SpatialStrategy MUST NOT be required to implement Starter.

#### Scenario: Interface satisfaction

- GIVEN the Starter interface is defined
- WHEN FundingStrategy is compiled
- THEN `var _ strategy.Starter = (*FundingStrategy)(nil)` compiles without error

---

### Requirement ST2: main.go Starter wiring

`cmd/server/main.go` MUST iterate the registered strategy slice, type-assert each to `strategy.Starter`, and call `Start(ctx)` on matches before the engine runs. Non-Starter strategies MUST be skipped silently.

#### Scenario: Start called on Starter strategies only

- GIVEN strategies = [SpatialStrategy, TriangularStrategy, FundingStrategy]
- WHEN startup wiring executes
- THEN FundingStrategy.Start(ctx) is called exactly once
- AND SpatialStrategy and TriangularStrategy are not started via the Starter path

---

## Capability: funding-executor

### Requirement FE1: FundingExecutor records trades without wallet impact

FundingExecutor.Execute(opp) MUST return a Trade with Strategy = "funding", PartialFill = false, Volume = 1.0. It MUST NOT call wallet.Debit or wallet.Credit. Trade.NetProfit MUST equal opp.NetProfit.

#### Scenario: Trade fields and no wallet touch

- GIVEN a funding opportunity with NetProfit = 12.5
- WHEN FundingExecutor.Execute(opp) is called
- THEN trade.Strategy == "funding", trade.PartialFill == false, trade.Volume == 1.0
- AND trade.NetProfit == 12.5
- AND no wallet mutation occurs

---

### Requirement FE2: Strategy-based dispatch in runProcessingLoop [ADR-3 override]

`runProcessingLoop` MUST route opportunities by opp.Strategy using a switch statement:
- `"funding"` → FundingExecutor (no wallet mutation, records synthetic funding P&L)
- `"triangular"` → TriangularExecutor (no wallet mutation, records 3-leg fee bundle)
- `"spatial"` or any other value → SpotExecutor (two-venue Debit/Credit via MultiWallet)

NOTE: The original spec said "triangular uses SpotExecutor as default". Design ADR-3 overrides
this decision. TriangularExecutor is dedicated because SpotExecutor's two-venue Debit/Credit
model would corrupt MultiWallet for intra-exchange round-trips. This override is intentional.

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

---

## Capability: dashboard-integration

### Requirement DI1: StrategyPnL panel shows 3 strategies

`/api/pnl-by-strategy` MUST return at least 3 rows (spatial, triangular, funding) with non-zero trade counts after 60s of DemoMode runtime. The frontend StrategyPnL component MUST render one row per strategy returned by the API.

#### Scenario: 3-row response after demo window

- GIVEN the bot has been running in DemoMode for 60 seconds
- WHEN GET /api/pnl-by-strategy is called
- THEN the response contains rows for "spatial", "triangular", and "funding"
- AND each row has trade_count > 0

---

## Capability: no-regression

### Requirement NR1: All baseline tests pass with no new dependencies

All 127 existing tests MUST pass after the change. The build MUST be clean (tsc + npm run build). go.mod and package.json MUST NOT gain new external dependencies.

#### Scenario: Full test suite green

- GIVEN the change is applied
- WHEN `go test ./... -race` is executed
- THEN exit code is 0 with no failures or data races

#### Scenario: Frontend build clean

- WHEN `tsc` and `npm run build` are executed in the web directory
- THEN both exit with code 0 and produce no type errors
