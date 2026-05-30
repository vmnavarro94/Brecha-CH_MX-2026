# Design: Synthetic Order Book Depth + Partial Fills

## Technical Approach

Introduce a stateless `internal/depth` package that generates synthetic L2 levels around the latest BBO and walks those levels to a target volume, returning a VWAP fill price and a partial-fill flag. The executor stops trusting BBO for the whole `MaxVolume`: it asks `depth.AskLevels` / `depth.BidLevels` for both legs, walks each leg, then drives wallet debits/credits from the accumulated walk cost rather than `bbo * volume`. Determinism is preserved by injecting a `*rand.Rand` into `depth.Config` at construction time, so tests use a fixed seed while demo uses wall-clock seed. New `Trade` fields (`RequestedVolume`, `PartialFill`) ride inside the existing SQLite `payload` JSON blob — no schema migration.

## Architecture Decisions

| Decision | Choice | Rejected | Rationale |
|---|---|---|---|
| Asymmetric fill handling (buy fills 0.05, sell only 0.03) | Option B: fail both legs, reverse buy using VWAP cost | A: keep residual BTC at buy exchange; C: scale both legs to `min(buyFilled, sellFilled)` | A leaks inventory across exchanges, breaking the closed-arbitrage invariant the executor guarantees today. C requires a second walk pass and re-computing VWAPs, doubling code paths. B is atomic-or-nothing, matches the existing "reverse buy on sell-debit failure" pattern in `executor.go:108-114`, and gives the jury a clean partial-fill story (the trade is recorded against the smaller side; we don't half-execute). |
| Walk algorithm | Greedy linear, no backtracking | Backtracking to find best-cost subset | Walking a synthetic book is monotonic in price; greedy minimises VWAP by definition. Backtracking adds complexity for zero benefit. |
| Money math at depth boundary | `shopspring/decimal` in `depth.Level{Price,Qty}` and `Walk` outputs | `float64` end-to-end | Proposal locks `Trade.BuyPrice/SellPrice` to decimal VWAP; level math must match to avoid rounding artefacts in golden tests. Wallet still consumes `float64` (`wallet.Debit` signature unchanged); conversion happens at the executor boundary via `.Float64()`. |
| Rand source | Injected `*rand.Rand` in `depth.Config` | Package-level `math/rand.Float64()` | Existing executor tests use `fixedClock`; depth must follow the same pattern so qty randomisation is reproducible. |
| Partial-fill marker | `Trade.PartialFill bool` only | New `StatusPartialFill` opportunity status | Proposal lock #3. Status remains `Executed` because the trade did execute, just below requested volume. |
| Reversal amount on sell-side debit failure | VWAP-accumulated USDT cost (`costBuy`) | `bboBuyAsk * buyFilled` | If the buy leg walked 2 levels at worse prices, debiting the wallet with the BBO price would credit back less than was charged, leaking USDT. Load-bearing correctness fix. |

## Data Flow

    Engine ─→ Opportunity ─→ Executor.Execute
                                  │
                                  ├─ snapshot[buyEx]   ──→ depth.AskLevels(bboAsk, cfg) ─→ depth.Walk(target=MaxVolume) ─→ (buyFilled, vwapBuy, partial)
                                  │                                                                                              │
                                  │                                                                          wallet.Debit(buyEx, USDT, vwapBuy*buyFilled)
                                  │                                                                          wallet.Credit(buyEx, BTC, buyFilled)
                                  │                                                                                              │
                                  ├─ snapshot[sellEx]  ──→ depth.BidLevels(bboBid, cfg) ─→ depth.Walk(target=buyFilled) ─→ (sellFilled, vwapSell, partial)
                                  │                                                                                              │
                                  │      if sellFilled < buyFilled  ──→ reverse: Debit(buyEx,BTC,buyFilled) + Credit(buyEx,USDT,costBuy)  ──→ Skipped
                                  │                                                                                              │
                                  │                                                                          wallet.Debit(sellEx, BTC, sellFilled)
                                  │                                                                          wallet.Credit(sellEx, USDT, vwapSell*sellFilled)
                                  │
                                  └─→ store.SaveTrade(Trade{BuyPrice=vwapBuy, SellPrice=vwapSell, Volume=sellFilled, RequestedVolume=MaxVolume, PartialFill=…, Slippage=(vwapBuy-bboAsk)+(bboBid-vwapSell)})

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/depth/depth.go` | Create | `Level`, `Config`, `AskLevels`, `BidLevels`, `Walk`. Pure functions, no I/O. |
| `internal/depth/depth_test.go` | Create | Seeded `AskLevels`/`BidLevels` golden, `Walk` for 3 boundary cases (exact fill, partial, single-level overflow). |
| `internal/types/types.go` | Modify | Add `OrderBookLevel{Price,Qty decimal.Decimal}`; extend `Trade` with `RequestedVolume decimal.Decimal` + `PartialFill bool` (JSON tags `requested_volume`, `partial_fill`). |
| `internal/executor/executor.go` | Modify | Accept `depth.Config` in `NewExecutor`; replace single-price fill with two `Walk` calls; reversal uses `costBuy = vwapBuy*buyFilled`; populate new `Trade` fields and `Slippage`. |
| `internal/executor/executor_test.go` | Modify | Plumb seeded `rand.Source` into constructor; assert VWAP, partial flag, reversal credits VWAP cost (not BBO). |
| `config/config.go` | Modify | Add `DepthLevels`, `DepthStepPct`, `DepthMinQtyBTC`, `DepthMaxQtyBTC` env vars with defaults `5 / 0.00005 / 0.005 / 0.025`. |
| `cmd/.../main.go` (engine wiring) | Modify | Build `depth.Config{Rand: rand.New(rand.NewSource(time.Now().UnixNano()))}` and pass to `NewExecutor`. |
| `web/src/types/api.ts` | Modify | `RawTrade` gains `RequestedVolume: string; PartialFill: boolean`; `Trade` gains parsed equivalents. |
| `web/src/store/marketStore.ts` | Modify | `parseTrade` runs `parseFloat(RequestedVolume)` and propagates `PartialFill`. |
| `web/src/components/TradeHistory.tsx` | Modify | Render `parcial` badge when `PartialFill === true`. |

## Interfaces / Contracts

```go
// internal/depth/depth.go
type Level struct {
    Price decimal.Decimal
    Qty   decimal.Decimal
}

type Config struct {
    N         int
    StepPct   float64
    MinQtyBTC float64
    MaxQtyBTC float64
    Rand      *rand.Rand
}

func AskLevels(bbo decimal.Decimal, cfg Config) []Level // prices rising
func BidLevels(bbo decimal.Decimal, cfg Config) []Level // prices falling
func Walk(levels []Level, target decimal.Decimal) (filled, vwap decimal.Decimal, partial bool)
```

Walk pseudocode (greedy, monotonic, exact):

    filled, costSum := decimal.Zero, decimal.Zero
    for _, lvl := range levels {
        remaining := target.Sub(filled)
        if remaining.Sign() <= 0 { break }
        take := decimal.Min(lvl.Qty, remaining)
        costSum = costSum.Add(take.Mul(lvl.Price))
        filled  = filled.Add(take)
    }
    partial = filled.LessThan(target)
    if filled.IsZero() { vwap = decimal.Zero } else { vwap = costSum.Div(filled) }

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit (`depth`) | `AskLevels`/`BidLevels` shape with fixed seed; `Walk` exact-fill, partial-fill, zero-target, empty-levels | Pass `rand.New(rand.NewSource(42))`; assert decimal values to 8dp. |
| Unit (`executor`) | VWAP populated, `PartialFill` flag, reversal credits VWAP cost (not BBO), `RequestedVolume` preserved | Extend existing `fixedClock` fixtures; inject seeded `depth.Config`. |
| Integration | `/trades` JSON exposes new fields; old SQLite rows still decode (extra JSON keys ignored on read) | Existing store tests cover backward-compat path; add one trade round-trip with `PartialFill=true`. |
| Frontend | `TradeHistory` renders `parcial` badge | Vitest snapshot or RTL assertion on existing component test. |

## Migration / Rollout

No DB migration. `Trade` is serialised as JSON inside the existing `payload` blob, and unknown fields on old rows decode to zero values (`RequestedVolume = decimal.Zero`, `PartialFill = false`). Rollout is a single deploy; rollback is a code revert plus optional removal of the dead `internal/depth` package.

## Open Questions

- None blocking. Wallet remains `float64`; depth boundary converts via `.Float64()`. If a future change moves the wallet to decimal, the conversion shim disappears without altering depth semantics.