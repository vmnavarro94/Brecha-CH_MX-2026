# Exploration: triangular-and-funding-rate-strategies

## Current State

Day 1 framework: `Strategy` interface has `Name()` + `Detect(update, snapshot, now)`. Engine fans out `ProcessUpdate` to every entry in `[]StrategyIface` — no registry, no ordering, just a slice. Adding a strategy is a one-liner in `main.go`.

All 10 connectors subscribe to a single symbol: BTC/USDT bookTicker. Aggregator (`internal/feed/aggregator.go`) keys state `map[string]PriceUpdate` by exchange name only — no symbol field anywhere in `PriceUpdate` or `Snapshot()`. **Central constraint that affects both new strategies differently.**

Executor is spot-specific: debit USDT → credit BTC on buy, debit BTC → credit USDT on sell, walk-the-book. No concept of perpetual positions or time-accruing P&L.

`/api/pnl-by-strategy` and `StrategyPnL` panel already work for any strategy name stamped on the trade. Zero frontend work needed.

## Affected Areas

- `internal/types/types.go` — `PriceUpdate` has no `Symbol` field
- `internal/feed/aggregator.go` — snapshot keyed by exchange only
- `internal/exchange/*` — 10 connectors hardcoded single-pair
- `internal/engine/engine.go` — unaffected for F1/T1
- `internal/executor/executor.go` — incompatible with perp positions
- `cmd/server/main.go` — register strategies, wire Start lifecycle
- `config/config.go` — `TRIANGULAR_ENABLED`, `FUNDING_ENABLED`, `FUNDING_POLL_INTERVAL_S`, `DEMO_FUNDING_DIFFERENTIAL`
- New: `internal/strategy/triangular/`, `internal/strategy/funding/`

## Triangular — Approaches

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| **T1** | Synthetic prices — derive ETH/BTC from REST ETH/USDT + streaming BTC/USDT, Bellman-Ford on 3 vertices | Zero connector/aggregator changes. Demo ready. Algorithm tested e2e. | Reference goes stale. Not real arb. | Low |
| T2 | Extend connectors + aggregator multi-symbol (`map[exchange]map[symbol]BBO`) | Real market data, full accuracy | Breaking change to 10 connectors + aggregator + spatial + engine. Huge blast radius. | High |
| T3 | Separate multi-pair connectors, internal state | Real data, zero interface change | Duplicates connector pattern. Detect ignores injected snapshot (awkward). | Medium |

**Recommendation: T1 for Day 2.** Bellman-Ford algorithm is identical regardless of data source; T1 tests the algorithm e2e with synthetic prices and zero blast radius. T3 is production path for a later day.

## Funding Rate — Approaches

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| **F1** | Side goroutine + internal rate cache. Constructor starts poller (30s, Binance + Bybit REST). `Detect()` reads from internal `map[exchange]float64` under RWMutex, emits when differential > threshold. `lastEmitAt` guard prevents flooding. | No interface or engine changes. Self-contained. Works today. | `Detect` called every price update but emits rarely — needs rate-limit. Lifecycle wired via type assertion in main.go. | Low-Medium |
| F2 | Extend `Strategy` interface with optional `Tick(ctx, now)` | Clean separation event vs time-driven | Engine refactor. Over-engineered for Day 2. Scope creep. | Medium-High |
| F3 | Synthetic `PriceUpdate` injection | No interface changes | Pollutes snapshot. Spatial sees "binance-funding" as exchange. Type misuse. | Low (wrong) |

**Recommendation: F1.** Side-goroutine poller + internal cache + `lastEmitAt` guard. `fund.Start(ctx)` via local `Starter` interface assertion in main.go. No interface or engine changes.

## Executor Compatibility for Funding Rate

**Biggest risk.** Spot semantics (buy BTC on A, sell BTC on B) do not model perp positions.

Options:
1. Reuse existing executor as-is. "Buy" = long perp entry, "sell" = short perp entry. P&L wrong (no 8h funding accrual) but pipeline runs e2e.
2. Skip executor: `FundingRateStrategy` emits to heap, separate funding executor records synthetic trade with correct funding-differential P&L. Dispatch logic in main.go inspects `opp.Strategy`.

For 60s demo: option 2 cleaner. For Day 2 speed: option 1 acceptable if labeled.

## Demo Seeding

- Triangular: DemoFees near-zero, synthetic ETH/USDT seeded at startup with ±0.1% noise per exchange. Any non-unity cycle produces opportunity immediately.
- Funding: Mock rates at construction (`binance = +0.01%, bybit = -0.005%`, configurable via `DEMO_FUNDING_DIFFERENTIAL`). Opportunities appear immediately without real REST poll.

## Open Questions for Proposal

1. T1 vs T3 for triangular: confirm synthetic OK for Day 2, or invest in separate connectors
2. Executor path for funding: reuse (faster) or separate dispatch (cleaner)
3. `Start(ctx)` lifecycle: type assertion in main.go or add `Starter` interface in `internal/strategy`?
4. Per-strategy enable/disable at runtime: needed for Day 2 or static registration?
5. Funding P&L model: single-shot entry vs position-open-until-settlement

## Risks

1. **Executor incompatibility funding arb** — HIGHEST. Spot produces wrong P&L for perp. OK for demo only.
2. **Synthetic triangular prices stale** — REST ETH/USDT goes stale. Mitigate 30s TTL + stale guard.
3. **Heap flooding from funding Detect()** — Without `lastEmitAt`, emits every tick (~50/sec). Mandatory rate-limit.
4. **PriceUpdate no Symbol field** — Real multi-symbol triangular requires breaking aggregator change. T1 avoids but document as demo scaffolding.
5. **No runtime strategy toggle** — Acceptable for 60s demo.

## Ready for Proposal

Yes. Both strategies have clear scopes, confirmed approaches (T1 + F1), open questions are about executor dispatch and data fidelity that proposal can resolve.
