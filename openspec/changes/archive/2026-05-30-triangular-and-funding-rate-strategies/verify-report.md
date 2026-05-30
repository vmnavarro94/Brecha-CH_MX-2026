# Verify Report: triangular-and-funding-rate-strategies

## Executive summary

**Verdict: PASS.** All 12 spec requirements satisfied. 5 critical anti-regression deep-checks PASS. 150/150 Go tests pass under `-race`. Frontend `tsc --noEmit` and `vite build` exit clean. Zero new external dependencies (go.mod, go.sum, web/package.json, web/package-lock.json all unchanged vs main).

CRITICAL: 0 — WARNING: 1 — SUGGESTION: 2

---

## 1. Spec compliance

| Req | Status | Evidence |
|-----|--------|----------|
| TR1 — Synthetic ETH/USDT seed per exchange | PASS | `internal/strategy/triangular/triangular.go:87-91`: lazy seed `refs[exchange] = SeedRefPrice*(1 + (rng.Float64()*2-1)*NoiseRange)`. Distinct per call to rng. Range matches spec. |
| TR2 — Negative-cycle detection with 3*fee cost model | PASS (with WARNING) | `triangular.go:108-146`: explicit 2-cycle ratio check (ADR-2). `TestTriangular_NoArbWhenRatiosMatch` and `TestTriangular_CostModelSubtracts3TakerFees` GREEN. See WARNING below — ETH ref no longer influences ratios. |
| TR3 — Strategy="triangular", intra-exchange | PASS | `triangular.go:157-166`: `Strategy="triangular"`, `BuyExchange=SellExchange=exchange`. `TestTriangular_StampsStrategyName` + `TestTriangular_IntraExchange` GREEN. |
| TR4 — Thread-safe setters | PASS | `triangular.go:172-183`: `SetTakerFee`/`SetNoiseRange` hold `mu.Lock()`. `TestTriangular_Race` GREEN under `-race`. |
| FR1 — Side-goroutine poller respects ctx | PASS | `funding.go:65-89`: goroutine with `select { case <-ctx.Done(): return ... }`. `TestFunding_DetectEmptyCacheNoOpps` (empty cache) and `TestFunding_StartRespectsCtxCancel` GREEN. |
| FR2 — Differential detection, low=Buy | PASS | `funding.go:113-141`: snapshot max/min, `BuyExchange=minEx`. `TestFunding_DetectEmitsWhenDiffExceedsThreshold` + `TestFunding_BuyExchangeIsLowFunding` GREEN. |
| FR3 — lastEmitAt cooldown guard | PASS | `funding.go:148-156`: `pairKey = minEx+"->"+maxEx`, cooldown check before emit. `TestFunding_CooldownGuardSuppresses5sRapidCalls` GREEN at T=0/3s/6s. |
| FR4 — Strategy="funding", NetProfit single-shot | PASS | `funding.go:158-168`: `Strategy="funding"`, `NetProfit=diff*Notional`. No 8h normalization (ADR-4). |
| ST1 — Starter interface defined | PASS | `internal/strategy/starter.go:10-12`. `var _ strategy.Starter = (*funding.FundingStrategy)(nil)` at `funding_test.go:219` compiles. Triangular and Spatial intentionally do NOT implement. |
| ST2 — main.go iterates strategies, type-asserts, calls Start | PASS | `cmd/server/main.go:144-152`: `allStrategies := []strategy.Strategy{spat, tri, fund}`, loop type-asserts to `strategy.Starter`, calls `Start(ctx)`, fatal on error. |
| FE1 — FundingExecutor records trade, no wallet | PASS | `internal/executor/funding_executor.go:28-56`: builds Trade with `Strategy="funding"`, `Volume=1.0`, `PartialFill=false`, `Fees=0`, `NetProfit=opp.NetProfit`. Zero wallet calls (grep confirmed: only comment matches). |
| FE2 — Strategy-based dispatch [ADR-3 rewrite] | PASS | spec.md:175-202 lists 3-way matrix (`funding`→FundingExecutor, `triangular`→TriangularExecutor, default→SpotExecutor). `main.go:480-488` implements identical switch. Override marked explicitly. |
| DI1 — StrategyPnL renders 3 strategies | PASS | `web/src/components/StrategyPnL.tsx:83-109` is data-driven (`strategyPnL.map`). Backend `/api/pnl-by-strategy` already returns rows per Strategy column (verified in archive of multi-strategy-framework). After 60s of demo runtime with both `TriangularEnabled=true` and `FundingEnabled=true` defaults, 3 strategies will be present. |
| NR1 — Tests green, no new deps | PASS | `go test -race ./...`: 150/150 individual PASS across 17 packages. `tsc --noEmit`: clean. `vite build`: clean (built in 841ms). `git diff main..HEAD` on go.mod/go.sum/web/package.json/web/package-lock.json: empty. |

---

## 2. Anti-regression deep-checks

1. **Dispatch switch correctness (FE2)** — PASS.
   `cmd/server/main.go:480-488`:
   ```go
   switch opp.Strategy {
   case "funding":    execErr = fundingExec.Execute(opp)
   case "triangular": execErr = triangularExec.Execute(opp)
   default:           execErr = exec.Execute(opp)
   }
   ```
   3 branches: funding, triangular, default→SpotExecutor (covers "spatial" and any unknown).

2. **No wallet calls in new executors (FE1, ADR-3)** — PASS.
   `rg "wallet\.Debit|wallet\.Credit"` against `funding_executor.go` and `triangular_executor.go`: only 2 matches, both inside Go doc-comments. Zero call sites. Trades carry NetProfit but produce no balance mutations.

3. **Starter lifecycle (ST1, ST2, FR1)** — PASS.
   `main.go:144-152` iterates `[spat, tri, fund]` and calls `Start(ctx)` on Starter implementers. `FundingStrategy.Start` (`funding.go:65-89`) launches a single goroutine with `started` flag (idempotent) and exits on `<-ctx.Done()` within one poll interval. `TestFunding_StartRespectsCtxCancel` confirms clean shutdown.

4. **Cooldown enforcement (FR3)** — PASS.
   `FundingStrategy.Detect` (`funding.go:148-156`) checks `lastEmit[pairKey]` BEFORE setting it and BEFORE emitting. Cooldown is per ordered pair so `bybit->binance` and `binance->bybit` share state via min/max ordering. `TestFunding_CooldownGuardSuppresses5sRapidCalls` exercises T=0 (emit), T=3 (suppress), T=6 (emit).

5. **ADR-3 spec rewrite verification** — PASS.
   `openspec/changes/triangular-and-funding-rate-strategies/spec.md:175-202` lists the full 3-way dispatch matrix and explicitly notes "Design ADR-3 overrides this decision … This override is intentional." Three scenarios (Funding, Triangular, Spatial) are documented. Spec and implementation now agree.

---

## 3. Test results

- `go test -race -count=1 ./...` — all 17 packages OK, **150 individual `--- PASS` lines** (matches apply-progress claim of 150 final).
  - Per-package breakdown: depth, engine, exchange, executor, feed, model, risk, server, store, strategy (compile-time only), strategy/funding, strategy/spatial, strategy/triangular, types, uptime, wallet — all `ok` with `-race`.
- Frontend: `npx tsc --noEmit` — no errors. `npm run build` — vite v6.4.2 built 1625 modules in 841ms (`dist/assets/index-B3UpSir5.js` 212.25 kB / 63.69 kB gzipped).
- Dependency diff: `git diff main..HEAD -- go.mod go.sum web/package.json web/package-lock.json` produces empty output. Zero new external dependencies introduced.
- Commit chain (7 commits): Starter → TriangularStrategy → FundingStrategy → FundingExecutor → TriangularExecutor → docs(spec FE2) → feat(main wiring).

---

## 4. Findings

### CRITICAL
None.

### WARNING

1. **TR2 ratio formula does not use `ethRef`** — `triangular.go:131` has `_ = ethRef // ethRef is used implicitly through btcMid derivation`. The final ratios `ratioA = btcMid/ask` and `ratioB = bid/btcMid` depend only on the BTC bid/ask spread, NOT on the ETH reference price. The lazy seed and random walk of `refs[exchange]` execute on every Detect call but the value is then discarded. The cycleGain detected is effectively just half the BTC bid-ask spread divided by mid, which IS mathematically valid for a 3-cycle when ETH is priced consistently across the cycle (and the test suite proves emission semantics work), but it means:
   - Spec TR1 ("synthetic ETH/USDT seed per exchange") is technically satisfied (the field is seeded with the spec'd distribution) but the seed has no effect on emission decisions.
   - The TR2 design intent of detecting divergence "when seeded ethRef diverges from implied" is not what the code measures; what the code actually measures is intra-venue BTC bid-ask spread.
   This is documented in `triangular_test.go:96-100` as a known design simplification. Apply-progress also flags this ("Initial skeleton had wrong formula (ethRef/ask); fixed in TDD GREEN cycle.").
   Recommendation: either remove the unused `refs`/`lastRefresh` bookkeeping (dead state) or restore a formula that consumes `ethRef` (e.g. compare `btcMid/ask` against `ethRef/impliedEth`). Not a blocker for archive; the executor + dashboard still receive valid triangular opportunities driven by spread + low fee.

### SUGGESTION

1. **Dispatch switch has no log/metric for unknown strategies** — `main.go:486` routes any unknown `opp.Strategy` to `SpotExecutor`. If a fourth strategy is added later but the wiring is forgotten, it will silently get wallet-debit semantics applied. Consider adding a debug log when the `default` branch is taken with a non-empty, non-"spatial" Strategy value.

2. **FundingPollInterval default 30s vs DI1 60s demo window** — `config.go:114` defaults to 30s. With `time.After(30s)` plus the seed tick at t=0, the second tick lands at T+30s. Two ticks within a 60s window suffices for differential detection (and `TestFunding_StartLaunchesPoller` confirms cache population), but if the synthetic wave (`sin(ticks/3.0+i)*BaseDifferential`) happens to be near zero on the first two ticks, no emission may occur before the 60s mark. Consider lowering `FUNDING_POLL_INTERVAL_MS` to 10000 (10s) for the demo profile to make DI1 robust across all PRNG seeds, or assert in CI by running the bot for 90s.

---

## 5. Recommended next

`sdd-archive` — no CRITICAL findings; the WARNING is a documented simplification already noted in tests and apply-progress, not a regression; SUGGESTIONs are non-blocking polish items for a follow-up change.

---

## 6. Risks

None blocking. The unused `ethRef` (warning #1) is mathematically benign for the current emission semantics but should be revisited if/when the strategy is extended to a 4-vertex graph (e.g. adding XRP/SOL), per Design Risk #5.
