# Exploration: kelly-correlation-adaptive

Day 5 final optimization layer. 3 features: Kelly sizing, correlation penalty, adaptive thresholds.

## Current State

- `spatial.go:148` sets `MaxVolume = MaxPositionUSDT / buyAsk` — fixed cap
- `spatial.go:120` checks `MinNetProfitPct` — static config
- `SpreadModel` (Welford rolling mean+variance, 500-sample ring) already exists. `Mean()`, `Std()`, `ZScore()`, `IsReady()` (≥100), `N()` exported and concurrency-safe.
- `store.AllTrades()` returns historical trades with NetProfit/Volume/Strategy — cold-start source for Kelly
- `RiskManager.Evaluate()` re-checks `MinNetProfitPct` post-detection — coupling risk
- Triangular and funding use `Notional` dollar config, not percent-based. Kelly + adaptive = **spatial-only** for Day 5.

## Affected Areas

- `internal/strategy/spatial/spatial.go` — MaxVolume + threshold logic
- `internal/strategy/spatial/spatial.go` — new `RecordTradeReturn` callback
- New: `internal/sizing/kelly.go` — `KellyEstimator` per-pair Welford over returns
- `internal/risk/risk.go` — wire `BaseMinNetProfitPct` floor
- `config/config.go` — 4-6 new env vars
- `cmd/server/main.go` — register RecordTradeReturn hook in executor

## Approaches

### F1 — Kelly Criterion

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| **A** | Per-pair `KellyEstimator` in `internal/sizing`, fractional 0.25×, cold-start fallback | Correct, per-pair calibration, testable, reuses Welford | Needs RecordTradeReturn callback wiring | Medium ~100 lines |
| B | Single global Kelly | Simpler, works day 0 | Pools 3 strategies' P&L, noisy | Low ~40 lines |
| C | `sigmoid(score) * MaxPositionUSDT` | Trivial | Not Kelly | Very Low ~5 lines |

**Recommend A.** Formula: `f* = mean_return / variance_return` (continuous Kelly). Cap at `kellyFraction` default 0.25. Cold-start: N < `KellyMinSamples` → fall back to `MaxPositionUSDT`.

### F2 — Cross-Pair Correlation

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| A | Full Pearson matrix of per-pair returns | Rigorous | O(N²); needs timestamp-aligned returns; complex | High ~200 lines |
| **B** | Same-exchange overlap penalty from recent-N trades ring buffer | O(N_recent); captures real risk; explainable | Not true correlation; exchange-axis only | Low ~50 lines |
| C | Background goroutine recomputing Pearson every 10s | Decoupled | Stale; goroutine lifecycle | High |

**Recommend B.** Document A as follow-up. Real-world: same-exchange concentration IS the main risk to mitigate.

### F3 — Adaptive Thresholds

| # | Approach | Pros | Cons | Effort |
|---|---|---|---|---|
| **A** | `adaptiveMin = baseMin + adaptCoeff * spreadModel.Std()` | 3 lines; reuses SpreadModel; per-pair | Coefficient calibration | Very Low ~30 lines |
| B | Quantile-based (P75 of recent spreads) | Robust to outliers | New tracker | Medium ~60 lines |

**Recommend A.** Coefficient tunable via env, conservative default 0.5.

## Architecture Insight

All 3 features share ONE new extension point: `RecordTradeReturn(buyEx, sellEx string, netPct float64)` method on `SpatialStrategy`. Executor calls it post-trade. Feeds:
- `KellyEstimator` (per-pair Welford over return fractions)
- `recentTrades` ring buffer (correlation penalty)

`SpreadModel.Std()` already exists for F3. Zero new model state.

## Config Additions (spatial.Config)

- `KellyMinSamples int` (default 10)
- `KellyFraction float64` (default 0.25)
- `BaseMinNetProfitPct float64` (floor, replaces `MinNetProfitPct` semantics)
- `AdaptiveCoeff float64` (default 0.5)
- `CorrPenaltyWeight float64` (default 0.3)
- `CorrWindowN int` (default 50)

## Risks

1. **Kelly cold start (MEDIUM)** — fractional cap + min-samples fallback mitigates
2. **Kelly variance instability at small N (MEDIUM)** — 0.25× cap prevents extreme sizing
3. **RiskManager `MinNetProfitPct` coupling (LOW)** — must wire to `BaseMinNetProfitPct` floor in main; otherwise RiskManager rejects opportunities that passed adaptive filter
4. **Adaptive coefficient calibration (LOW)** — conservative default, env-overridable
5. **Same-exchange penalty over-penalizing (LOW)** — 0.1 floor on volume scale prevents zeroing out

## Suggested PR Slicing

Single PR ~180 lines. Smaller than Days 3-4. No chained needed.

Phases:
- F3 adaptive thresholds (~30 lines, smallest)
- F2 same-exchange correlation penalty (~50 lines)
- F1 Kelly per-pair sizing + RecordTradeReturn hook (~100 lines, biggest)

## Open Questions

1. **Kelly scope**: spatial only (recommended) OR all 3 strategies?
2. **Correlation**: simple Approach B (recommended) OR full matrix?
3. **Adaptive thresholds**: per-pair (recommended) OR global?
4. **Kelly min-samples**: 10? 30?
5. **Fractional Kelly multiplier**: 0.25 (conservative) or 0.5 (aggressive)?
6. **RecordTradeReturn hook location**: in spatial strategy, or shared interface across strategies?

## Ready for Proposal

Yes. All hooks confirmed in real code. Single new callback + reuse SpreadModel + new sizing package = clean architecture.
