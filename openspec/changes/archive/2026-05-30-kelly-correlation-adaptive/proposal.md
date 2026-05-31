# Proposal: Kelly + Correlation + Adaptive Thresholds (Spatial)

## Intent

Spatial strategy gets quant-grade position sizing (Kelly), risk-aware exposure (correlation penalty), and market-aware filtering (adaptive thresholds). Replace static `MaxPositionUSDT` cap and fixed `MinNetProfitPct` with per-pair statistical sizing and volatility-aware thresholds. Triangular and funding (Notional-based) deferred.

## Scope

### In Scope

- New package `internal/sizing/` with `KellyEstimator` (per-pair Welford over return fractions)
- `SpatialStrategy.RecordTradeReturn(buyEx, sellEx, netPct)` method
- `SpatialStrategy.kellyEstimators map[string]*KellyEstimator` field
- `SpatialStrategy.recentTrades []recentTrade` ring buffer (cap 50) for same-exchange overlap
- MaxVolume formula in `Detect`: `kellyFraction * (mean/variance) * notional`; fallback to `MaxPositionUSDT` when N < 10
- Adaptive threshold in `Detect`: `adaptiveMin = baseMin + adaptCoeff * spreadModel.Std()` replacing static check
- Correlation penalty: scan `recentTrades` for same-exchange matches; scale MaxVolume by `(1 - corrPenaltyWeight * matches/window)`; floor 0.1
- `SpatialConfig` adds: `KellyMinSamples`, `KellyFraction`, `BaseMinNetProfitPct`, `AdaptiveCoeff`, `CorrPenaltyWeight`, `CorrWindowN`
- `main.go`: SpotExecutor calls `spatial.RecordTradeReturn` after a successful trade
- `RiskManager` wired to `BaseMinNetProfitPct` floor (not the adaptive value)
- 6 new env vars

### Out of Scope

- Kelly for triangular and funding strategies (Notional-based, deferred)
- Full Pearson correlation matrix (Approach A, deferred)
- Persistence of Kelly estimator state across restarts (cold-start every boot)
- Frontend UI for Kelly/correlation diagnostics (data lives in trades, no panel)

## Capabilities

### New Capabilities
- `kelly-sizing`: per-pair Kelly Criterion position sizing with fractional cap and cold-start fallback
- `correlation-penalty`: same-exchange overlap penalty derived from recent-trades ring buffer
- `adaptive-thresholds`: per-pair volatility-aware min-net-profit threshold

### Modified Capabilities
- `spatial-strategy`: `Detect` now computes sizing via Kelly + correlation penalty and filters via adaptive threshold
- `risk-management`: `MinNetProfitPct` floor sourced from `BaseMinNetProfitPct` (no-op when adaptive passed)

## Approach

Implementation order minimizes risk: F3 (adaptive threshold formula) first, F2 (same-exchange ring-buffer penalty) second, F1 (KellyEstimator package + callback + sizing) last. All three live entirely inside `internal/strategy/spatial/` plus the new `internal/sizing/` package. Single test file grows incrementally. `SpreadModel.Std()` is reused for F3, zero new model state.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/sizing/kelly.go` | New | `KellyEstimator` struct + `New/Record/Fraction/Samples` |
| `internal/strategy/spatial/spatial.go` | Modified | sizing, threshold, ring buffer, `RecordTradeReturn` |
| `internal/risk/risk.go` | Modified | wire floor to `BaseMinNetProfitPct` |
| `config/config.go` | Modified | 6 new env vars |
| `cmd/server/main.go` | Modified | executor calls `RecordTradeReturn` post-trade |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Kelly cold-start variance instability | Med | Min-samples=10 + fractional cap 0.25 |
| Kelly + correlation stacking over-shrinks volume | Med | 0.1 floor on volume scale |
| RiskManager floor misconfigured | Low | Wire to `BaseMinNetProfitPct` explicitly + test |
| Adaptive coefficient miscalibration | Low | Conservative default 0.5; env-overridable |
| Same-exchange penalty zeroing active pairs | Low | 0.1 floor |

## Rollback Plan

Set env: `KELLY_MIN_SAMPLES=99999` (forces cold-start fallback), `ADAPTIVE_THRESHOLD_COEFF=0` (disables adaptive add), `CORR_PENALTY_WEIGHT=0` (disables penalty). Behavior reverts to current static `MaxPositionUSDT / buyAsk` + static `MinNetProfitPct`. No schema or persistence changes to revert.

## Dependencies

None new. Reuses existing `SpreadModel`, `store.AllTrades`, decimal lib.

## Success Criteria

- [ ] Cold-start (no trades) → MaxVolume == MaxPositionUSDT / buyAsk (current behavior preserved)
- [ ] After 10 trades with positive edge → Kelly-scaled volume > 0 and < MaxPositionUSDT
- [ ] Same-exchange concentration → correlation penalty reduces MaxVolume; test with 5 binance-* trades verifies
- [ ] High `SpreadModel.Std()` → adaptive threshold > base; test with known std verifies formula
- [ ] All 207 baseline tests pass + new tests
- [ ] No new external dependencies
