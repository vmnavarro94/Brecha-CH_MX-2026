# Tasks: triangular-and-funding-rate-strategies

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 650–800 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 (Groups A–C: Starter + TriangularStrategy) → PR 2 (Groups D–E: Executors) → PR 3 (Groups F–H: Wiring, config, spec update) |
| Delivery strategy | ask-on-risk |
| Chain strategy | feature-branch-chain |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Starter interface + TriangularStrategy + FundingStrategy | PR 1 | Base: feat/funding-rate-arbitrage; all unit tests included |
| 2 | FundingExecutor + TriangularExecutor | PR 2 | Base: PR 1 branch; no wallet deps |
| 3 | Spec FE2 update (ADR-3) + main.go wiring + config | PR 3 | Base: PR 2 branch; integration smoke included |

---

## Phase 1: Foundation — Starter Interface (Group A)

- [ ] 1.1 RED: Write `TestStarterInterface_FundingStrategyImplements` in `internal/strategy/starter_test.go` — compile-time assert `var _ Starter = (*funding.FundingStrategy)(nil)` fails until Starter exists
- [ ] 1.2 Create `internal/strategy/starter.go` — `type Starter interface { Start(ctx context.Context) error }`
- [ ] 1.3 Verify 1.1 GREEN and `go test ./internal/strategy/...` passes; commit: `feat(strategy): add Starter interface`

## Phase 2: Core — TriangularStrategy (Group B)

- [ ] 2.1 RED: Write `TestTriangular_NoArbWhenRatiosMatch` in `internal/strategy/triangular/triangular_test.go` — BTC/USDT=50000, ETH/USDT=2000, ETH/BTC=0.04 → empty slice
- [ ] 2.2 Create `internal/strategy/triangular/triangular.go` — `Config` struct, `TriangularStrategy` struct with `mu sync.RWMutex`, `New(cfg Config) *TriangularStrategy`, `Name() string`, `Detect(...)` skeleton returning nil
- [ ] 2.3 Implement `Detect`: seed `refs[exchange]` lazily on first call (SeedRefPrice ± NoiseRange, per-exchange via rng); compute explicit 3-cycle ratioA + ratioB; cycleGain = max(ratioA,ratioB)-1.0
- [ ] 2.4 Implement cost model inside `Detect`: netGainPerUSDT = cycleGain - 3*TakerFee; netProfit = netGainPerUSDT * Notional; emit if netProfit > MinNetProfit AND cooldown elapsed
- [ ] 2.5 Implement `SetTakerFee(v float64)` with `mu.Lock()` guard; implement `SetNoiseRange(v float64)` similarly
- [ ] 2.6 Stamp `opp.Strategy = "triangular"`, `opp.BuyExchange = exchange`, `opp.SellExchange = exchange` before appending to result slice
- [ ] 2.7 RED: Write `TestTriangular_DetectsCycleWhenRatioDiverges` — ETH/USDT=2050, BTC/USDT=50000 → opp.NetProfit > 0 (spec TR2 scenario 2)
- [ ] 2.8 RED: Write `TestTriangular_CostModelSubtracts3TakerFees` — verify netProfit == (cycleGain - 3*fee)*notional (spec TR2)
- [ ] 2.9 RED: Write `TestTriangular_StampsStrategyName` — opp.Strategy == "triangular" (spec TR3)
- [ ] 2.10 RED: Write `TestTriangular_IntraExchange` — opp.BuyExchange == opp.SellExchange == "binance" (spec TR3)
- [ ] 2.11 Make all 2.x tests GREEN; `go test -race ./internal/strategy/triangular/...`
- [ ] 2.12 Commit: `feat(strategy): add TriangularStrategy with 3-cycle explicit ratio check`

## Phase 3: Core — FundingStrategy (Group C)

- [ ] 3.1 RED: Write `TestFunding_DetectEmptyCacheNoOpps` — new FundingStrategy, call Detect without Start → empty slice (spec FR1 scenario 2)
- [ ] 3.2 Create `internal/strategy/funding/funding.go` — `Config` struct, `FundingStrategy` struct (`mu sync.RWMutex`, `rates map[string]float64`, `lastEmit map[string]time.Time`, `started bool`, `rng *rand.Rand`, `ticks uint64`), `New`, `Name`, `Detect` skeleton returning nil, `Start` skeleton returning nil
- [ ] 3.3 Implement `Detect` empty-cache path: `mu.RLock`, if `len(rates)==0` return nil
- [ ] 3.4 RED: Write `TestFunding_StartLaunchesPoller` — call `Start(ctx)`, wait 2×PollInterval, call `Detect` → non-empty rates populated (spec FR1)
- [ ] 3.5 Implement `Start`: launch goroutine; seed rates once at t=0 using synthetic wave generator (tick++, wave=sin(ticks/3+i)*BaseDiff, jitter=(rng.Float64()*2-1)*BaseDiff*0.5); loop with `time.After(PollInterval)` and `ctx.Done()`
- [ ] 3.6 RED: Write `TestFunding_StartRespectsCtxCancel` — Start, cancel ctx, wait 2×PollInterval, verify goroutine exited (no leak) (spec FR1 scenario 1)
- [ ] 3.7 Verify cancel path in `Start` goroutine exits on `<-ctx.Done()`
- [ ] 3.8 RED: Write `TestFunding_DetectEmitsWhenDiffExceedsThreshold` — inject rates manually (binance=+0.02%, bybit=-0.005%), threshold=0.015% → opp emitted with BuyExchange="bybit" (spec FR2)
- [ ] 3.9 Implement `Detect` emission: snapshot rates under RLock; find maxEx/minEx; diff=max-min; if diff<Threshold return nil; check cooldown; emit `Opportunity{Strategy:"funding", BuyExchange:minEx, SellExchange:maxEx, NetProfit:diff*Notional}`; set lastEmit[pairKey]
- [ ] 3.10 RED: Write `TestFunding_CooldownGuardSuppresses5sRapidCalls` — emit at T=0, call Detect at T=3s → empty; call at T=6s → emits (spec FR3)
- [ ] 3.11 Implement `lastEmit` cooldown guard inside Detect (pair key = minEx+"->"+maxEx)
- [ ] 3.12 RED: Write `TestFunding_StampsStrategyName` — opp.Strategy == "funding" (spec FR4)
- [ ] 3.13 RED: Write `TestFunding_BuyExchangeIsLowFunding` — lower funding rate venue == BuyExchange (spec FR2)
- [ ] 3.14 Make all 3.x tests GREEN; `go test -race ./internal/strategy/funding/...`
- [ ] 3.15 Commit: `feat(strategy): add FundingStrategy with side-goroutine rate poller`

## Phase 4: Executors (Groups D + E)

- [ ] 4.1 RED: Write `TestFundingExecutor_RecordsTradeWithoutWallet` in `internal/executor/funding_executor_test.go` — Execute(opp) persists Trade, no wallet.Debit/Credit called (spec FE1)
- [ ] 4.2 Create `internal/executor/funding_executor.go` — `FundingExecutor` struct (fields: `store *store.Store`, `clock types.Clock`); `NewFundingExecutor(st, clk)`; `Execute(opp *types.Opportunity) error` skeleton
- [ ] 4.3 Implement `Execute`: build `types.Trade{Strategy:"funding", Volume:1.0, PartialFill:false, Fees:0, GrossProfit:opp.NetProfit, NetProfit:opp.NetProfit}`; call `store.SaveTrade`; set `opp.Status = StatusExecuted`; no wallet calls
- [ ] 4.4 RED: Write `TestFundingExecutor_NetProfitEqualsOppNetProfit` — trade.NetProfit == opp.NetProfit (spec FE1)
- [ ] 4.5 RED: Write `TestFundingExecutor_StampsStrategy` — trade.Strategy == "funding"
- [ ] 4.6 Make 4.1–4.5 GREEN; commit: `feat(executor): add FundingExecutor`
- [ ] 4.7 RED: Write `TestTriangularExecutor_RecordsTradeWithoutWallet` in `internal/executor/triangular_executor_test.go` (ADR-3; design TriangularExecutor spec)
- [ ] 4.8 Create `internal/executor/triangular_executor.go` — `TriangularExecutor` struct (fields: `store`, `clock`, `takerFee float64`, `notional float64`); `NewTriangularExecutor(st, clk, takerFee, notional)`; `Execute` skeleton
- [ ] 4.9 Implement `Execute`: `fees = 3*takerFee*notional`; `grossProfit = opp.NetProfit + fees`; build `Trade{Strategy:"triangular", Volume:notional, Fees:fees, GrossProfit:grossProfit, NetProfit:opp.NetProfit, PartialFill:false}`; `store.SaveTrade`; `opp.Status=StatusExecuted`; no wallet calls
- [ ] 4.10 RED: Write `TestTriangularExecutor_NetProfitEqualsOppNetProfit`
- [ ] 4.11 RED: Write `TestTriangularExecutor_StampsStrategy` — trade.Strategy == "triangular"
- [ ] 4.12 Make 4.7–4.11 GREEN; `go test -race ./internal/executor/...`; commit: `feat(executor): add TriangularExecutor`

## Phase 5: Spec Update + Wiring (Groups F + G + H)

- [ ] 5.1 Open `openspec/changes/triangular-and-funding-rate-strategies/spec.md`; update requirement FE2 routing matrix to: `"funding"→FundingExecutor`, `"triangular"→TriangularExecutor`, `"spatial"/default→SpotExecutor` (ADR-3 override)
- [ ] 5.2 Commit: `docs(spec): update FE2 routing matrix per ADR-3 (TriangularExecutor override)`
- [ ] 5.3 Add to `config/config.go` `Config` struct: `TriangularEnabled bool`, `TriangularNoiseRange float64`, `TriangularSeedRefPrice float64`, `TriangularNotional float64`, `TriangularSeed int64`; and `FundingEnabled bool`, `FundingThreshold float64`, `FundingPollInterval time.Duration`, `FundingBaseDifferential float64`, `FundingNotional float64`, `FundingSeed int64`
- [ ] 5.4 Wire defaults in `config.Load()`: TriangularNoiseRange=0.001, TriangularSeedRefPrice=2000.0, TriangularNotional=1000.0, TriangularSeed=42; FundingThreshold=0.00015, FundingPollInterval=5s, FundingBaseDifferential=0.002, FundingNotional=10000.0, FundingSeed=99 — values tuned so all 3 strategies emit within 60s in DemoMode
- [ ] 5.5 In `cmd/server/main.go`: import `triangular` and `funding` packages; construct `tri := triangular.New(triangular.Config{...cfg fields...})`; construct `fund := funding.New(funding.Config{...cfg fields...})`
- [ ] 5.6 Add Starter wiring loop after strategies are constructed and before `engine.NewEngine`: iterate `strategies []strategy.Strategy`, type-assert to `strategy.Starter`, call `Start(ctx)` on matches; return error on failure (spec ST2)
- [ ] 5.7 Change engine registration to `[]engine.StrategyIface{spat, tri, fund}`
- [ ] 5.8 Construct `fundingExec := executor.NewFundingExecutor(st, clk)` and `triangularExec := executor.NewTriangularExecutor(st, clk, takerFee, cfg.TriangularNotional)` after existing `exec` construction
- [ ] 5.9 Replace `exec.Execute(opp)` call in `runProcessingLoop` with switch on `opp.Strategy`: `"funding"→fundingExec.Execute`, `"triangular"→triangularExec.Execute`, `default→exec.Execute`
- [ ] 5.10 `go build ./...` must succeed; `go test -race ./...` must show ≥127 tests passing with 0 failures
- [ ] 5.11 Commit: `feat(main): wire triangular+funding strategies, executors, and Starter loop`
- [ ] 5.12 Add env-var reads for all new config fields in `config.Load()` (`getEnvFloat`, `getEnvBool`, `getEnvDuration` as needed)
- [ ] 5.13 `go test ./...` final green gate; commit: `feat(config): add triangular and funding config fields with demo defaults`
