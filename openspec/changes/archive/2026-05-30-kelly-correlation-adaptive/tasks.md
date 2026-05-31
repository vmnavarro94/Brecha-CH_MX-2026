# Tasks: Kelly + Correlation + Adaptive Thresholds (Spatial)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 280-340 |
| 400-line budget risk | Medium |
| Chained PRs recommended | No |
| Suggested split | Single PR — all groups sequential within one feature branch |
| Delivery strategy | ask-on-risk |

Decision needed before apply: No
Chained PRs recommended: No
400-line budget risk: Medium

## Implementation Summary

7 phases executed under strict TDD mode:

1. **Foundation — KellyEstimator** (sizing package, Welford algorithm)
2. **Foundation — recentTradeRing** (correlation ring buffer)
3. **Core — Adaptive thresholds** (per-pair volatility-aware filtering)
4. **Core — Kelly sizing** (per-pair position scaling)
5. **Core — Correlation penalty** (same-exchange overlap penalty)
6. **Integration — TradeReturnReporter** (executor post-trade callback)
7. **Wiring — Config & main.go** (6 env vars + dependency injection)

All 7 phases completed. Final state:
- 228 tests passing (207 baseline + 21 new)
- Zero external dependency changes
- Race detector clean
- Cold-start behavior preserves pre-change MaxVolume formula

See verify-report for compliance details.
