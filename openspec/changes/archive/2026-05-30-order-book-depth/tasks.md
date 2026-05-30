# Tasks: Order Book Depth

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 250–320 |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | single-pr |
| Chain strategy | size-exception |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | All backend + config + frontend | PR 1 | Atomic; frontend badge is cosmetic-only risk |

---

## Phase 1: Foundation — depth package + types extension

- [ ] 1.1 Create `internal/depth/depth_test.go` — write failing `TestAskLevels_Deterministic` (fixed seed 42, 5 levels from bbo=50000, step=0.0001; assert prices + count) [spec D1, D2]
- [ ] 1.2 Create `internal/depth/depth.go` — define `Level`, `Config` structs and implement `AskLevels` (rising prices, rand qty in [MinQtyBTC, MaxQtyBTC]) to make 1.1 green
- [ ] 1.3 Add failing `TestBidLevels_Deterministic` to `depth_test.go` (prices falling, same seed invariant) [spec D1, D2]
- [ ] 1.4 Implement `BidLevels` in `depth.go` (symmetric: prices descending) to make 1.3 green
- [ ] 1.5 Add `OrderBookLevel` struct to `internal/types/types.go`; add `RequestedVolume decimal.Decimal` + `PartialFill bool` (json tags `requested_volume`, `partial_fill`) to `Trade` [spec D5, D10]
- [ ] 1.6 Run `go test ./...` — verify zero regressions (spec D9)
- [ ] 1.7 Commit `feat(depth): synthetic order book with seeded levels`

## Phase 2: Core — Walk implementation

- [ ] 2.1 Add failing `TestWalk_ExactSingleLevel` to `depth_test.go`: levels=[(50000,0.025),(50005,0.05)], target=0.02 → filled=0.02, vwap=50000, partial=false [spec D3 scenario 1]
- [ ] 2.2 Add failing `TestWalk_ExactMultiLevel`: target=0.05 → filled=0.05, vwap=50002.50, partial=false [spec D3 scenario 2]
- [ ] 2.3 Add failing `TestWalk_LiquidityExhausted`: target=0.10 → filled=0.075, vwap≈50003.33, partial=true [spec D3 scenario 3]
- [ ] 2.4 Implement `Walk` in `depth.go` (greedy linear per design pseudocode) to make 2.1–2.3 green
- [ ] 2.5 Commit `feat(depth): walk-the-book VWAP + partial fill`

## Phase 3: Executor refactor

- [ ] 3.1 Add `TestExecute_BuyPriceIsVWAP` to `executor_test.go` (seeded depth.Config, single-level book, assert Trade.BuyPrice=vwap_buy) [spec D4]
- [ ] 3.2 Refactor `NewExecutor` in `executor.go` to accept `depth.Config`; make 3.1 green
- [ ] 3.3 Refactor `Execute` to call `depth.AskLevels` + `Walk` for buy leg; wallet debit uses `vwapBuy * buyFilled` [spec D4, D6]
- [ ] 3.4 Add failing `TestExecute_PartialFillFlag` — assert Trade.PartialFill=true + RequestedVolume when liquidity < target [spec D5]
- [ ] 3.5 Make 3.4 green (populate Trade fields in Execute)
- [ ] 3.6 Add failing `TestExecute_ReversalUsesVWAPCost` — sell leg fails; assert wallet credited `vwapBuy*buyFilled`, NOT `bboAsk*buyFilled` [spec D7]
- [ ] 3.7 Refactor reversal block in `Execute` to credit `costBuy`; make 3.6 green
- [ ] 3.8 Add failing `TestExecute_AsymmetricFillAborts` — sellFilled < buyFilled → result is Skipped, no net BTC change [design: atomic-or-nothing]
- [ ] 3.9 Make 3.8 green; run `go test ./...`
- [ ] 3.10 Commit `feat(executor): walk-the-book + VWAP + atomic reversal on asymmetric fill`

## Phase 4: Config + wiring

- [ ] 4.1 Add `DepthLevels int`, `DepthStepPct float64`, `DepthMinQtyBTC float64`, `DepthMaxQtyBTC float64` to `config/config.go` with env vars and defaults `5 / 0.00005 / 0.005 / 0.025`
- [ ] 4.2 In `cmd/server/main.go` construct `depth.Config{N: cfg.DepthLevels, ..., Rand: rand.New(rand.NewSource(time.Now().UnixNano()))}` and pass to `NewExecutor`
- [ ] 4.3 Run `go test ./...` + `go build ./...` — must pass clean
- [ ] 4.4 Commit `feat(main): wire depth config from env + seed rand`

## Phase 5: Frontend

- [ ] 5.1 Add `RequestedVolume: string; PartialFill: boolean` to `RawTrade` in `web/src/types/api.ts`; add parsed fields to `Trade` type
- [ ] 5.2 Update `parseTrade` in `web/src/store/marketStore.ts` — map `RequestedVolume` via `parseFloat`, propagate `PartialFill`
- [ ] 5.3 Add failing test in `web/src/components/TradeHistory.test.tsx` — row with `PartialFill=true` renders a `parcial` badge
- [ ] 5.4 Render `parcial` badge in `web/src/components/TradeHistory.tsx` when `trade.partialFill === true`; make 5.3 green
- [ ] 5.5 Run `npx tsc --noEmit && npm run build` — clean
- [ ] 5.6 Commit `feat(ui): show partial-fill flag in trade history`