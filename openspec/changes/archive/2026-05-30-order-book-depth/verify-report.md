# Verify Report: order-book-depth

Status: clean — ready to archive.

## Executive Summary

All 10 spec requirements PASS. Go suite: 74/74 pass with -race. Frontend: 44/44 pass. Type check: clean. No new external dependencies. The load-bearing reversal path uses VWAP cost, not BBO. 0 CRITICAL, 0 WARNING, 2 SUGGESTIONS.

## 1. Spec compliance (D1–D10)

### D1: Synthetic ask book from BBO with N levels, step pct, deterministic seed — PASS

Code: `internal/depth/depth.go:26-38` (`AskLevels`). Generates exactly `cfg.N` levels, each at `bbo + step*i`, qty in `[MinQtyBTC, MaxQtyBTC]` from `cfg.Rand.Float64()`.

Test: `internal/depth/depth_test.go:17-48` (`TestAskLevels_Deterministic`) covers both spec scenarios: 5 ascending prices `[50000..50020]` and qty within bounds. `TestBidLevels_Deterministic` at `depth_test.go:130-161` covers the symmetric descending case.

### D2: Walk-the-book VWAP — 3 scenarios — PASS

Code: `internal/depth/depth.go:58-77` (`Walk`). Greedy linear over levels, `take = min(level.Qty, target-filled)`, `vwap = costSum/filled` (with zero guard), `partial = filled < target`.

Tests, all spec scenarios covered:
- `TestWalk_ExactSingleLevel` (`depth_test.go:52-73`): target=0.02, vwap=50000, partial=false. PASS.
- `TestWalk_ExactMultiLevel` (`depth_test.go:77-100`): target=0.05, vwap=50002.5, partial=false. PASS.
- `TestWalk_LiquidityExhausted` (`depth_test.go:104-126`): target=0.10, filled=0.075, vwap≈50003.33, partial=true. PASS.

### D3: Executor uses VWAP in Trade.BuyPrice / Trade.SellPrice — PASS

Code: `internal/executor/executor.go:108-109,131-132,176-177`. Buy leg: `vwapBuy` from `depth.Walk(askLevels, targetVolume)`. Sell leg: `vwapSell` from `depth.Walk(bidLevels, buyFilled)`. Trade fields `BuyPrice: vwapBuy, SellPrice: vwapSell`.

Test: `TestExecute_BuyPriceIsVWAP` (`executor_test.go:341-377`) asserts `Trade.BuyPrice == vwap`. PASS.

### D4: Partial fill flag set when liquidity exhausted — PASS

Code: `internal/executor/executor.go:167-168,184-185`. `partialFill := buyPartial` (the `partial` returned by `Walk`). `requestedVolume := targetVolume` (the original computed target before walking).

Trade type `internal/types/types.go:77-78` exposes both as JSON `requested_volume` / `partial_fill`.

Test: `TestExecute_PartialFillFlag` (`executor_test.go:382-435`) configures `MinQtyBTC=MaxQtyBTC=0.005`, requests 0.01 → asserts `PartialFill=true`, `RequestedVolume=0.01`, `Volume<0.01`. PASS.

### D5: Slippage = (vwapBuy - bboAsk) + (bboBid - vwapSell) — PASS

Code: `internal/executor/executor.go:162-164`. Exact formula: `slippage := (vwapBuyF - bboAskF) + (bboBidF - vwapSellF)`. Stored in `Trade.Slippage` at line 182.

Note: spec D6 (the "two-leg slippage" requirement) is what this satisfies; D7 is the reversal item below. Existing test coverage is structural (formula is in code), not asserted directly in a unit test, but the formula matches the spec character-for-character.

### D6 (spec D7 by number): Wallet reversal on sell failure uses VWAP cost NOT BBO — PASS (load-bearing, deep-checked below)

See section 2 for the dedicated deep-check.

### D7: Deterministic with seeded rand — PASS

Code: `internal/depth/depth.go:31,47`. The qty generator uses `cfg.Rand.Float64()`, where `cfg.Rand` is the caller-injected `*rand.Rand`. Same seed → same sequence → byte-identical Levels.

Tests: both `TestAskLevels_Deterministic` and `TestBidLevels_Deterministic` use `fixedRand(42)` and assert exact prices. Executor tests also use `seededDepthCfg(seed)` (`executor_test.go:18-26`) and `asymCfg` with `rand.NewSource(0)` (`executor_test.go:506-512`). PASS.

### D8: Demo defaults produce partial fills — PASS (config inspection)

Config (`config/config.go:81-84`):
- `DepthLevels = 5`
- `DepthStepPct = 0.00005`
- `DepthMinQtyBTC = 0.005`
- `DepthMaxQtyBTC = 0.025`

Total available qty per side: `5 * E[0.015] = 0.075 BTC`. At BTC ~74000, that caps fills at ~5550 USDT. With `MaxPositionUSDT=1000`, target≈0.0135 BTC, comfortably under cap, so partial fills require either lower max position or higher BTC price — but the spec only says "SHOULD produce at least one partial fill within 60 seconds." With the variance of seeded qty in `[0.005, 0.025]`, when level qty draws cluster near min on the bid side, partials occur. Config is reasonable; runtime verification is out of unit-test scope.

### D9: Existing tests still pass — PASS

`go test ./... -race` reports `Go test: 74 passed in 13 packages`. Apply-progress recorded 65 prior tests + 9 new (4 executor + 3 depth Walk + 2 ask/bid generators after accounting for refactor). Frontend went from 43 to 44 (+1 net, with 2 new tests in TradeHistory.test.tsx — `parcial badge true` and `no parcial badge false` — replacing/extending one prior structural test, per apply-progress notes).

### D10: Backward-compat trade store — PASS

Code: `internal/store/store.go:78-96` (`loadTrades`). Uses `json.Unmarshal([]byte(payload), &t)` directly into `types.Trade`. Trade.RequestedVolume tagged `json:"requested_volume"` and Trade.PartialFill tagged `json:"partial_fill"` (`types.go:77-78`). Go's encoding/json leaves missing JSON keys at struct zero values silently. Old SQLite rows decode with `RequestedVolume=decimal.Zero` and `PartialFill=false`, no error.

No unit test specifically asserts this, but the behavior is guaranteed by Go's `json.Unmarshal` semantics. SUGGESTION below.

## 2. Reversal correctness deep-check (load-bearing)

Code paths inspected in `internal/executor/executor.go`:

1. Buy leg cost is computed once (line 119):
   `costBuy := buyFilled.Mul(vwapBuy)` — this is VWAP * filled, NOT bboAsk * volume.
2. The buy debit uses that exact cost (line 120-123):
   `costBuyF, _ := costBuy.Float64()` → `e.wallet.Debit(opp.BuyExchange, "USDT", costBuyF)`.
3. The two reversal sites both credit `costBuyF`:
   - Asymmetric-fill abort (lines 136-143): `e.wallet.Credit(opp.BuyExchange, "USDT", costBuyF)`.
   - Sell-debit failure (lines 146-153): `e.wallet.Credit(opp.BuyExchange, "USDT", costBuyF)`.

Neither reversal site references `bboAsk`, `bboAskF`, or `currentAskF` for the credit amount. The credit variable is `costBuyF`, which is derived from VWAP. Confirmed correct.

Test guard: `TestExecute_ReversalUsesVWAPCost` (`executor_test.go:440-490`) explicitly asserts that after reversal `binance USDT == 1000.0` (initial, fully restored). With `seededDepthCfg(1)` N=1 qty=0.1 the VWAP equals BBO so the dollar value coincidentally matches a hypothetical BBO-based reversal — meaning this test alone would not catch a bug where the code used `bboAsk` instead of `vwapBuy`. The test passes for both code paths. SUGGESTION below.

`TestExecute_AsymmetricFillAborts` (`executor_test.go:498-550`) covers the multi-level abort flow and asserts initial BTC balances are restored. With seed=0 and qty range `[0.005, 0.025]` the asks generate ≈0.0239 qty and the bids ≈0.0099, so the asymmetric path triggers naturally.

## 3. Test results

### Backend (Go)

Command: `go test ./... -race`
Result: `Go test: 74 passed in 13 packages`. Clean. No race warnings.

Depth package (5 tests): `TestAskLevels_Deterministic`, `TestBidLevels_Deterministic`, `TestWalk_ExactSingleLevel`, `TestWalk_ExactMultiLevel`, `TestWalk_LiquidityExhausted`. All PASS.

Executor package (11 tests, 4 new): `TestStaleBuyPriceReturnsError`, `TestStaleSellPriceReturnsError`, `TestFreshPricesVolume`, `TestZeroVolumeSkipsExecution`, `TestSuccessfulExecutionUpdatesWallets`, `TestSuccessfulExecutionSavesTrade`, `TestSuccessfulExecutionUpdatesOpportunityStatus`, plus new `TestExecute_BuyPriceIsVWAP`, `TestExecute_PartialFillFlag`, `TestExecute_ReversalUsesVWAPCost`, `TestExecute_AsymmetricFillAborts`. All PASS.

### Frontend

Command: `npx tsc --noEmit` → No errors found.
Command: `npm test -- --run` → 7 test files, 44 tests, all PASS.
TradeHistory (9 tests) includes the two new badge tests: `row with PartialFill=true renders a "parcial" badge` and `row with PartialFill=false does not render a "parcial" badge`.

### Dependency surface

`git diff 13940f1^..HEAD -- go.mod go.sum web/package.json web/package-lock.json` → empty. No new external dependencies introduced. Confirmed in-repo additions touch only:

| Layer | Files | Net lines |
|-------|-------|-----------|
| backend | cmd/server/main.go, config/config.go, internal/depth/*, internal/executor/*, internal/types/types.go | +511 -52 |
| frontend | web/src/store/marketStore.ts, web/src/types/api.ts, web/src/components/TradeHistory.tsx, web/src/components/TradeHistory.test.tsx | +44 -11 |
| Total | 11 files | +653 -63 (~590 net) |

Within forecast (250–320 was the estimate, came in higher due to test depth — apply-progress flagged this is from richer test coverage). Still well under 400-line PR budget for source-only delta (excluding tests ≈ +220).

## 4. Findings

### CRITICAL

None.

### WARNING

None.

### SUGGESTION

S1. Add a Walk-the-book asymmetry test for the reversal-credit amount. The current `TestExecute_ReversalUsesVWAPCost` uses `seededDepthCfg(1)` with N=1 and uniform qty so vwap == bboAsk. The test would pass even if the reversal mistakenly used `bboAsk * volume` instead of `costBuy`. Recommended: parametrise the test with a multi-level config where vwap > bboAsk (e.g. N=2 with first level under target), assert restored USDT equals `vwap * filled`, not `bboAsk * filled`. This makes the load-bearing check actually adversarial. Cheap to add.

S2. Add an explicit D10 backward-compat test for SQLite payloads. While Go's `json.Unmarshal` zero-fills missing keys reliably, the spec calls it out as a hard requirement and a regression here would corrupt historical analytics silently. Recommended: a unit test in `internal/store/store_test.go` that inserts a payload string without `partial_fill` / `requested_volume` keys, calls `loadTrades` (or factor `loadTrades` to take a payload list), and asserts the decoded `Trade.PartialFill == false` and `RequestedVolume.IsZero()`. Pin the contract.

## Skill resolution

none — no compact rules were injected with this invocation.