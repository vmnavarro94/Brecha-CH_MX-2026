# Proposal: Synthetic Order Book Depth + Partial Fills

## Intent

The current executor fills every opportunity at a single BBO price with zero slippage, which masks the realistic execution cost the jury explicitly wants to see (walk-the-book, partial fills, VWAP price). We introduce a synthetic L2 book and a walk-the-book executor so trades reflect liquidity exhaustion across multiple levels and surface partial fills as a first-class concept.

## Scope

### In Scope
- New package `internal/depth` (synthetic book generator + walker, deterministic via injected `rand.Source`).
- Extend `types.Trade` with `RequestedVolume decimal.Decimal` and `PartialFill bool`. `BuyPrice`/`SellPrice` repurposed to hold VWAP fill price.
- Add `OrderBookLevel{Price, Qty decimal.Decimal}` to `internal/types`.
- Refactor `internal/executor/executor.go` to walk levels, compute VWAP, populate `Slippage` properly, and respect partial-fill wallet semantics (debits/credits use accumulated VWAP cost, reversal mirrors that).
- Startup-only depth config in `config/config.go` via env vars (no PATCH endpoint).
- Frontend: extend `RawTrade`/`Trade` in `web/src/types/api.ts`, parse new fields in `web/src/store/marketStore.ts`, render PARTIAL badge in `web/src/components/TradeHistory.tsx`.

### Out of Scope
- Real L2 WebSocket feeds from any of the 10 connectors.
- Per-exchange cached order books in the aggregator.
- Runtime depth tuning via `PATCH /api/config`.
- A new `StatusPartialFill` opportunity status (flag-only on Trade).
- SQLite schema migration (new fields serialize into the existing `payload` JSON blob).

## Capabilities

### New Capabilities
- `order-book-depth`: synthetic L2 book generation, walk-the-book fills, partial-fill detection, VWAP pricing, slippage cost.

### Modified Capabilities
- None at spec level. Existing executor behavior is internal to this new capability.

## Approach

Approach 1 from exploration (central depth module). The executor consults a `depth.SyntheticBook` at execution time instead of trusting BBO for the whole volume.

Locked decisions:
1. Depth config startup-only via env vars.
2. `rand.Source` injected into depth config so tests are deterministic and demo can use a wall-clock seed.
3. Partial fills are flag-only on `Trade` (`PartialFill bool` + `RequestedVolume`), no new opportunity status.
4. `Trade.BuyPrice`/`Trade.SellPrice` hold the VWAP fill price (volume-weighted across walked levels). Semantics: "actual fill price".
5. Slippage formula: `Slippage = (vwapBuy - bboBuyAsk) + (bboSellBid - vwapSell)`. Captures cost of walking past BBO on both legs.

## Public Contract

`internal/depth` package:
- `Config{Levels int, StepPct float64, MinLevelQty, MaxLevelQty float64, Rand *rand.Rand}`
- `AskLevels(bbo decimal.Decimal, cfg Config) []types.OrderBookLevel`
- `BidLevels(bbo decimal.Decimal, cfg Config) []types.OrderBookLevel`
- `Walk(levels []types.OrderBookLevel, target decimal.Decimal) (filled decimal.Decimal, vwap decimal.Decimal, partial bool)`

`types.OrderBookLevel`:
```go
type OrderBookLevel struct {
    Price decimal.Decimal `json:"price"`
    Qty   decimal.Decimal `json:"qty"`
}
```

`types.Trade` additions (JSON tags):
```go
RequestedVolume decimal.Decimal `json:"requested_volume"`
PartialFill     bool            `json:"partial_fill"`
```

## Demo Defaults (locked)

- `Levels = 5` above ask, `5` below bid.
- `StepPct = 0.00005` (0.005% worse per step).
- Level qty range: `[0.005, 0.025]` BTC randomized per level.
- Seed: wall-clock in demo, fixed seed in tests.

Sizing rationale: requested ~0.0135 BTC against max ~0.025 BTC per level means 1 level sometimes covers, 2–3 sometimes required. Partial fills will trigger naturally inside 60s of bot start.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/depth/depth.go` | New | Synthetic book + walker. |
| `internal/types/types.go` | Modified | Add `OrderBookLevel`, extend `Trade`. |
| `internal/executor/executor.go` | Modified | Walk-the-book, VWAP, partial-fill flag, wallet reversal uses VWAP cost. |
| `config/config.go` | Modified | Depth env vars at startup. |
| `web/src/types/api.ts` | Modified | New `Trade` fields. |
| `web/src/store/marketStore.ts` | Modified | `parseTrade` handles new fields. |
| `web/src/components/TradeHistory.tsx` | Modified | PARTIAL badge. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Walk-the-book overflow (over-fills past target). | Med | Boundary tests: exactly-fill at level N, exhaust at N-1, single level covers all. |
| VWAP reversal bug: sell-side debit failure reverses wrong amount. | Med | Reversal uses accumulated cost from walk, not BBO price. Test reversal explicitly. |
| Demo invisibility (partials never trigger). | Med | Demo defaults sized so requested volume straddles single-level capacity. |
| Test determinism: random qty breaks existing executor tests. | High | Inject `rand.Source` into depth config; tests pass fixed seed. |
| Frontend type drift: new fields not parsed. | Low | Add to `parseTrade` + interface in same PR. |

## Rollback Plan

Revert the executor and `types.Trade` changes; the `internal/depth` package can stay as dead code. SQLite rows persisted during the change are forward-compatible (extra JSON fields are ignored by older Go structs). Frontend changes are revertable independently. No DB migration to roll back.

## Dependencies

- No new external Go modules. Uses `math/rand` and existing `shopspring/decimal`.

## Success Criteria

- [ ] At least one partial fill produced within 60 seconds of bot start in demo mode.
- [ ] `Trade.PartialFill` flag visible in `/trades` JSON and rendered as PARTIAL badge in trade history UI.
- [ ] Walking 3 levels with a fixed seed produces the exact expected VWAP in a unit test.
- [ ] All 65 existing tests still pass.
- [ ] No new external dependencies introduced (`go.mod` only changes if a stdlib import is added).