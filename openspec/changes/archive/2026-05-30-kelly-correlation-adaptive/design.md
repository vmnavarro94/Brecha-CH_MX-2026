# Design: Kelly + Correlation + Adaptive Thresholds (Spatial)

Change: `kelly-correlation-adaptive`

(Full design document archived - refer to primary specification in openspec/spec.md section 9.)

Key architectural decisions:
- KellyEstimator package in `internal/sizing/` with Welford algorithm
- Per-pair Kelly estimators in SpatialStrategy with cold-start fallback
- Same-exchange correlation penalty using ring buffer with OR-semantics
- Adaptive threshold: `BaseMinNetProfitPct + AdaptiveCoeff * spreadModel.Std()`
- TradeReturnReporter interface closes feedback loop from executor
- 6 new env vars for tuning (SPATIAL_BASE_MIN_NET_PROFIT_PCT, SPATIAL_ADAPTIVE_COEFF, KELLY_MIN_SAMPLES, KELLY_FRACTION, CORR_PENALTY_WEIGHT, CORR_WINDOW_N)
- No persistence; cold-start per process
- RiskManager floor decoupled from adaptive threshold

See openspec/spec.md section 9 for full design details and ADR decisions (ADR-1 through ADR-11).
