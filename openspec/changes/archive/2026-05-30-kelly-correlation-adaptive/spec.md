# Spec: Kelly + Correlation + Adaptive Thresholds (Spatial)

Change: `kelly-correlation-adaptive`

---

## New Capability: `kelly-sizing`

### Requirement K1: KellyEstimator initialization and sampling

The `sizing` package MUST expose `New() *KellyEstimator` returning an instance with zero samples. Callers MUST be able to record return fractions via `Record(netPct float64)` and query state via `Samples() int` and `Fraction() float64`.

**Test strategy**: unit — deterministic numeric sequences, verify Welford update and Fraction output.

---

### Requirement K2: Cold-start fallback

`Fraction()` MUST return 0 when `Samples() < KellyMinSamples (10)`. When `Fraction()` returns 0, `SpatialStrategy.Detect` MUST fall back to `MaxPositionUSDT / buyAsk` as the effective MaxVolume (current behavior preserved).

**Test strategy**: unit — verify `Fraction()==0` below minN and cold-start MaxVolume matches pre-change formula.

---

### Requirement K3: Fractional Kelly cap

`Fraction()` MUST NOT exceed `KellyFraction`. Values below the cap pass through unchanged.

**Test strategy**: unit — feed returns producing raw Kelly > cap and < cap; verify capping.

---

### Requirement K4: Per-pair estimator isolation

`SpatialStrategy` MUST maintain separate `KellyEstimator` instances per exchange-pair key. Recording a return for one pair MUST NOT affect estimators for other pairs.

**Test strategy**: unit — record trades for pair A, verify pair B estimator unaffected.

---

## New Capability: `correlation-penalty`

### Requirement C1: Recent-trades ring buffer

`SpatialStrategy` MUST maintain a ring buffer of length `CorrWindowN (50)` storing recent trade records. `RecordTradeReturn` MUST append to this buffer; the oldest entry MUST be evicted when the buffer is full.

**Test strategy**: unit — push beyond capacity, verify eviction and length invariant.

---

### Requirement C2: Same-exchange overlap penalty

Before emitting an opportunity, `Detect` MUST count how many entries in `recentTrades` share a buy or sell exchange with the candidate. The effective MaxVolume MUST be scaled by `penalty = max(1 - CorrPenaltyWeight * matches / CorrWindowN, 0.1)`.

**Test strategy**: unit — OR-semantics with multiple edge cases, verify floor at 0.1.

---

### Requirement C3: Volume scaling via penalty

The final MaxVolume passed into the opportunity MUST be `kellyOrFallback * correlationPenalty`.

**Test strategy**: unit — verify proportional scaling formula.

---

## New Capability: `adaptive-thresholds`

### Requirement A1: Per-pair adaptive minimum

The minimum-net-profit threshold used inside `Detect` for a given pair MUST be computed as `adaptiveMin = BaseMinNetProfitPct + AdaptiveCoeff * spreadModel.Std()`. When `spreadModel.Std() == 0`, `adaptiveMin` MUST equal `BaseMinNetProfitPct`.

**Test strategy**: unit — inject known Std values, verify formula.

---

### Requirement A2: Adaptive filter applied in Detect

Opportunities MUST pass `netPct > adaptiveMin` to be emitted. Opportunities that exceed `BaseMinNetProfitPct` but not `adaptiveMin` MUST be rejected.

**Test strategy**: unit — verify rejection at adaptive layer.

---

### Requirement A3: RiskManager floor uses base threshold only

`RiskManager.Evaluate` MUST use `BaseMinNetProfitPct`, not the adaptive value. An opportunity that passes the adaptive filter MUST also pass the RiskManager base floor.

**Test strategy**: unit — verify RiskManager checks only base floor.

---

## Modified Capability: `spatial-strategy`

### Requirement M-SS1: RecordTradeReturn method

`SpatialStrategy` MUST expose `RecordTradeReturn(buyEx, sellEx string, netPct float64)`. The method MUST update both the per-pair Kelly estimator and the ring buffer. The method MUST be concurrency-safe (write-lock before mutation).

**Test strategy**: unit + race detector — verify concurrent access.

---

## Modified Capability: `risk-management`

### Requirement M-RM1: MinNetProfitPct floor sourced from BaseMinNetProfitPct

`RiskManager` MUST source its `MinNetProfitPct` floor from `SpatialConfig.BaseMinNetProfitPct`. The adaptive threshold computed in `Detect` MUST NOT flow into `RiskManager`.

**Test strategy**: unit — verify wiring and behavior.

---

## Integration & Integration Executor

### Requirement I1: SpotExecutor calls RecordTradeReturn post-trade

After `SpotExecutor.Execute` succeeds for an opportunity with `Strategy == "spatial"`, it MUST call `spatial.RecordTradeReturn(buyEx, sellEx, computedNetPct)`. Non-spatial trades (funding, triangular) MUST NOT trigger this call.

**Test strategy**: unit — mock reporter, verify call for spatial trades only.

---

### Requirement I2: TradeReturnReporter interface and wiring

`SpatialStrategy` MUST satisfy a `TradeReturnReporter` interface with `RecordTradeReturn` method. The executor constructor MUST accept an optional `TradeReturnReporter` via functional option or config field.

**Test strategy**: unit — verify structural interface satisfaction and wiring.

---

## No-Regression Requirements

### Requirement NR1: Baseline test suite preserved

All 207 pre-existing tests MUST continue to pass after the change is applied.

**Test strategy**: regression — run full test suite, verify 228 total (207 baseline + 21 new) passing.

---

### Requirement NR2: No new external dependencies

The change MUST NOT introduce any new Go module dependencies outside the existing `go.mod`.

**Test strategy**: git diff — verify `go.mod` unchanged.

---

### Requirement NR3: Cold-start behavior matches current

A fresh `SpatialStrategy` (no recorded trades) MUST produce identical MaxVolume decisions as the pre-change implementation.

**Test strategy**: unit — verify fresh strategy yields `MaxPositionUSDT / buyAsk`.

---

### Requirement NR4: Per-instance state isolation

Kelly estimators and the ring buffer MUST be scoped to the `SpatialStrategy` instance. Creating a new instance MUST start with zero samples and an empty buffer (safe for backtests).

**Test strategy**: unit — verify new instances start clean.
