# Order Book Depth Specification

## Purpose

Defines behavior of the synthetic L2 order book generator, walk-the-book executor, partial-fill detection, VWAP pricing, and slippage calculation introduced by the `order-book-depth` change.

## Requirements

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

### Requirement D2: Deterministic generation with seeded source

The system MUST produce identical level prices and quantities when given the same `rand.Source` seed, BBO, and config.

#### Scenario: Same seed reproduces same book

- GIVEN a fixed seed S, bbo_ask=50000, and depth config C
- WHEN `AskLevels` is called twice with a freshly-seeded source each time
- THEN both calls return byte-identical slices

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

### Requirement D6: Slippage reflects walk cost vs BBO

The system MUST compute slippage as the combined cost of walking past BBO on both legs: `Slippage = (vwapBuy - bboAsk) + (bboBid - vwapSell)`.

#### Scenario: Two-leg slippage calculation

- GIVEN vwap_buy=50002.50, bbo_ask=50000, bbo_bid=50300, vwap_sell=50297.50
- WHEN slippage is computed
- THEN Slippage=5.00

### Requirement D7: Wallet reversal on sell failure uses VWAP cost

The system MUST credit the wallet with the exact VWAP-accumulated buy cost when a sell leg fails, not the BBO-estimated cost.

#### Scenario: Reversal uses VWAP cost

- GIVEN buy succeeded: vwap_buy=50002.50, filled=0.05 BTC (cost=2500.125 USDT)
- WHEN the sell leg fails
- THEN wallet is credited 2500.125 USDT
- AND the BBO-estimated cost of 2500.00 USDT is NOT used

### Requirement D8: Demo defaults produce partial fills within 60 seconds

The system SHOULD produce at least one partial fill within 60 seconds of bot start when running with default depth config (5 levels, qty range [0.005, 0.025] BTC).

#### Scenario: Partial fill occurs in demo mode

- GIVEN default depth config and BTC price ~74000 USDT, MaxPositionUSDT=1000
- WHEN the bot runs for 60 seconds
- THEN at least one Trade with PartialFill=true is recorded

### Requirement D9: No regression on existing tests

The system MUST pass all pre-existing tests without modification.

#### Scenario: Existing test suite passes

- GIVEN the codebase before this change has 65 passing tests
- WHEN the order-book-depth change is applied
- THEN all 65 tests still pass

### Requirement D10: Backward-compatible trade deserialization

The system MUST deserialize Trade records persisted before this change (without `partial_fill` or `requested_volume` JSON fields) using zero-values for missing fields.

#### Scenario: Old trade record loads cleanly

- GIVEN a SQLite row whose payload JSON has no `partial_fill` or `requested_volume` keys
- WHEN the row is decoded into `types.Trade`
- THEN Trade.PartialFill=false and Trade.RequestedVolume=0 with no error