# Exploration: Synthetic Order Book Depth + Partial Fills

## Current State

**Executor** `internal/executor/executor.go`: single-level BBO fill. Volume = `min(opp.MaxVolume, usdtBalance/currentAsk)`. Both legs execute at exactly one price (BBO ask for buy, BBO bid for sell). `Trade.Slippage` always `decimal.Zero`. No partial-fill concept.

**Engine** `internal/engine/engine.go` line 178: `MaxVolume = cfg.MaxPositionUSDT / buyAsk` — config/price ratio, no liquidity input. `PriceUpdate.BidSize`/`AskSize` exist but never read.

**Wallet** `internal/wallet/wallet.go`: already supports partial fills via `Debit` + `ErrInsufficientFunds`. No changes needed.

**Types** `internal/types/types.go`: `PriceUpdate` has BBO + size (single level). `Trade` has Volume/Buy/SellPrice/Slippage/Gross/Net/Fees — no RequestedVolume, no PartialFill.

**Store** `internal/store/store.go`: trades serialized as JSON payload — new fields auto-handle; old rows decode with zero-values (semantically correct).

**Frontend**: `Trade` mirrors Go struct; `parseTrade` in marketStore.ts uses parseFloat; TradeHistory renders 7 columns. Adding fields touches api.ts + parseTrade + TradeHistory.

## Affected Areas

- `internal/types/types.go` — add OrderBookLevel; extend Trade with RequestedVolume, PartialFill
- `internal/executor/executor.go` — replace single BBO with walk-the-book; VWAP price; partial detection; fix reversal to use accumulated cost
- `internal/depth/depth.go` (new) — SyntheticBook, AskLevels, BidLevels, Walk
- `internal/engine/engine.go` — no mandatory change
- `internal/store/store.go` — no change needed
- `web/src/types/api.ts` — extend RawTrade + Trade
- `web/src/store/marketStore.ts` — update parseTrade
- `web/src/components/TradeHistory.tsx` — partial-fill badge

## Approaches

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| 1 | Central depth module — executor generates levels from BBO + config | Zero connector changes. Self-contained. Deterministic with seeded rand. | Levels regenerated per execution. | Low-Medium |
| 2 | Per-exchange cached book — aggregator carries L2 array | Book stable across detection→execution | Touches every connector + every PriceUpdate constructor + tests. | High |
| 3 | Depth cap in engine — MaxVolume baked with liquidity | Minimal change | No walk-the-book, no VWAP. Does NOT satisfy reto's partial-fill criterion. | Low (wrong) |

## Recommendation

**Approach 1**. New `internal/depth` package:
- `AskLevels(bbo, cfg)`, `BidLevels(bbo, cfg)` → `[]Level`
- `Walk(levels, target)` → `(filled decimal, vwapPrice decimal, partial bool)`

Executor calls Walk for both sides, uses accumulated cost for wallet debit, records VWAP as BuyPrice/SellPrice, sets PartialFill + RequestedVolume on the trade.

**Demo tuning concern**: default MaxPositionUSDT=1000 at BTC=50k → ~0.02 BTC requested. If levels hold 0.05–0.5 BTC, level 1 always covers. Either raise MaxPositionUSDT or shrink level qty in demo defaults so partial fills are observable.

## Open Questions for Proposal

1. Expose depth config (N levels, step_pct, qty range) via PATCH /api/config or startup-only?
2. Seed strategy: fixed global seed (tests easy), per-execution random (realistic, non-deterministic tests), injected `rand.Source` (testable + realistic)?
3. New `StatusPartialFill` opportunity status or just flag on Trade?
4. `Trade.BuyPrice`/`SellPrice` hold VWAP (overloaded) or keep BBO + add AvgFillPrice fields?
5. Populate `Trade.Slippage` with `avgFillPrice - bboPrice`?

## Risks

- Walk-the-book over-fill bug — loop guard `filled ≤ requested` must be exact; test boundary cases (exact fill at level N, exhaustion before target)
- Reversal logic on sell failure must use VWAP-accumulated cost not original BBO — most likely regression in existing executor
- Test determinism — inject seeded `rand.Source` into depth config or executor constructor
- Demo default sizing must produce observable partial fills (otherwise feature is invisible)
- SQLite backward compat — new fields backward-compatible via JSON payload, but store must not break on old rows
- Frontend type drift — RawTrade must add new fields or parseTrade produces undefined/NaN

## Ready for Proposal

Yes. Approach clear, scope bounded (1 new package + 5 file changes), open questions concrete.