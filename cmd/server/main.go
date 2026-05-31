package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/config"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/depth"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/exchange"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/executor"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/feed"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/server"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/funding"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/triangular"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/uptime"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
)

var exchangeNames = []string{"binance", "kraken", "bybit", "okx", "gate", "mexc", "bitget", "htx", "cryptocom", "kucoin"}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// --- Infrastructure ---

	clk := types.RealClock{}

	st := store.NewStore(cfg.DataDir)
	defer st.Close()

	// --- Recorder (frames.db) ---

	rec, err := recorder.New(cfg.DataDir)
	if err != nil {
		slog.Warn("recorder: failed to open frames.db — recording disabled", "err", err)
		rec = nil
	}
	if rec != nil {
		defer rec.Close()
	}

	var recordingEnabled atomic.Bool

	w := wallet.NewMultiWallet(exchangeNames, map[string]float64{
		"USDT": cfg.InitialUSDTPerExchange,
		"BTC":  cfg.InitialBTCPerExchange,
	})

	// --- Exchange connectors ---

	connectors := []exchange.Connector{
		exchange.NewBinance(cfg.BinanceWSURL),
		exchange.NewKraken(cfg.KrakenWSURL),
		exchange.NewBybit(cfg.BybitWSURL),
		exchange.NewOKX(cfg.OKXWSURL),
		exchange.NewGate(cfg.GateWSURL),
		exchange.NewMEXC(cfg.MEXCWSURL),
		exchange.NewBitget(cfg.BitgetWSURL),
		exchange.NewHTX(cfg.HTXWSURL),
		exchange.NewCryptoCom(cfg.CryptoComWSURL),
		exchange.NewKuCoin(cfg.KuCoinAPIURL),
	}

	agg := feed.NewAggregator(connectors)
	if err := agg.Start(ctx); err != nil {
		slog.Error("failed to start aggregator", "err", err)
		os.Exit(1)
	}

	// --- Spread models (one per exchange pair) ---

	spreadModels := make(map[string]*model.SpreadModel)
	for _, buyEx := range exchangeNames {
		for _, sellEx := range exchangeNames {
			if buyEx == sellEx {
				continue
			}
			key := fmt.Sprintf("%s-%s", buyEx, sellEx)
			spreadModels[key] = model.NewSpreadModel()
		}
	}

	// --- SpatialStrategy ---

	exchangeFees := exchange.Fees
	if cfg.DemoMode {
		exchangeFees = exchange.DemoFees
	}

	spatFees := make(map[string]spatial.FeeConfig, len(exchangeFees))
	for name, f := range exchangeFees {
		spatFees[name] = spatial.FeeConfig{
			TakerFee:          f.TakerFee,
			SlippageFactor:    f.SlippageFactor,
			WithdrawalBTC:     f.WithdrawalBTC,
			NetworkLatencyBps: f.NetworkLatencyBps,
		}
	}

	spat := spatial.New(spatial.Config{
		Fees:               spatFees,
		MinNetProfitPct:    cfg.MinNetProfitPct,
		MaxPositionUSDT:    cfg.MaxPositionUSDT,
		StalenessThreshold: cfg.StalenessThreshold,
	}, spreadModels)

	// --- TriangularStrategy ---

	tri := triangular.New(triangular.Config{
		TakerFee:     cfg.TriangularTakerFee,
		NoiseRange:   cfg.TriangularNoiseRange,
		SeedRefPrice: cfg.TriangularSeedRefPrice,
		Notional:     cfg.TriangularNotional,
		MinNetProfit: 0.0,
		EmitCooldown: 5 * time.Second,
		Seed:         cfg.TriangularSeed,
	})

	// --- FundingStrategy ---

	fund := funding.New(funding.Config{
		Exchanges:        exchangeNames,
		Threshold:        cfg.FundingThreshold,
		PollInterval:     cfg.FundingPollInterval,
		EmitCooldown:     cfg.FundingEmitCooldown,
		Notional:         cfg.FundingNotional,
		BaseDifferential: cfg.FundingBaseDifferential,
		Seed:             cfg.FundingSeed,
	})

	// --- Starter wiring: call Start(ctx) on any strategy that implements strategy.Starter ---

	allStrategies := []strategy.Strategy{spat, tri, fund}
	for _, s := range allStrategies {
		if starter, ok := s.(strategy.Starter); ok {
			if err := starter.Start(ctx); err != nil {
				slog.Error("strategy start failed", "strategy", s.Name(), "err", err)
				os.Exit(1)
			}
		}
	}

	// --- Engine (thin coordinator) ---

	strategyIfaces := make([]engine.StrategyIface, 0, len(allStrategies))
	if cfg.TriangularEnabled {
		strategyIfaces = append(strategyIfaces, spat, tri)
	} else {
		strategyIfaces = append(strategyIfaces, spat)
	}
	if cfg.FundingEnabled {
		strategyIfaces = append(strategyIfaces, fund)
	}

	eng := engine.NewEngine(
		agg.Snapshot,
		clk,
		engine.Config{
			OpportunityTTL: cfg.OpportunityTTL,
		},
		strategyIfaces,
	)

	// Ensure SpatialStrategy satisfies strategy.Strategy at compile time.
	var _ strategy.Strategy = spat

	// --- Risk Manager ---

	rm := risk.NewRiskManager(risk.Config{
		MinNetProfitPct:  cfg.MinNetProfitPct,
		MaxPositionUSDT:  cfg.MaxPositionUSDT,
		LossThreshold:    cfg.CircuitBreakerLossPct,
		ConsecutiveLossN: cfg.CircuitBreakerN,
		PauseDuration:    time.Duration(cfg.CircuitBreakerPauseMins) * time.Minute,
	}, clk)

	// --- Executor ---

	depthCfg := depth.Config{
		N:         cfg.DepthLevels,
		StepPct:   cfg.DepthStepPct,
		MinQtyBTC: cfg.DepthMinQtyBTC,
		MaxQtyBTC: cfg.DepthMaxQtyBTC,
		Rand:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	exec := executor.NewExecutor(w, st, agg.Snapshot, clk, cfg.StalenessThreshold, depthCfg)
	fundingExec := executor.NewFundingExecutor(st, clk)
	triangularExec := executor.NewTriangularExecutor(st, clk, cfg.TriangularTakerFee, cfg.TriangularNotional)

	// --- WebSocket Hub ---

	hub := server.NewHub()
	go hub.Run()

	// --- Live config controller ---
	// Setter routing after refactor:
	//   MinNetProfitPct      → spat.SetMinNetProfitPct  (was eng)
	//   MaxPositionUSDT      → spat.SetMaxPositionUSDT  (was eng)
	//   StalenessThresholdMs → spat.SetStaleness        (was eng.SetStalenessThreshold)
	//   Fees / DemoMode swap → spat.SetFees             (was eng.SetFees)
	//   ExecutionInterval    → intervalCh               (unchanged)
	//   CircuitBreaker*      → rm                       (unchanged)

	intervalCh := make(chan time.Duration, 1)

	var cfgMu sync.Mutex
	liveCfg := server.ConfigSnapshot{
		DemoMode:             cfg.DemoMode,
		MinNetProfitPct:      cfg.MinNetProfitPct,
		MaxPositionUSDT:      cfg.MaxPositionUSDT,
		StalenessThresholdMs: int(cfg.StalenessThreshold / time.Millisecond),
		ExecutionIntervalMs:  int(cfg.ExecutionInterval / time.Millisecond),
		CircuitBreakerN:      cfg.CircuitBreakerN,
		CircuitBreakerLossPct: cfg.CircuitBreakerLossPct,
		Fees:                 toFeeInfoMap(exchangeFees),
	}

	getConfigFn := func() server.ConfigSnapshot {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		return liveCfg
	}

	patchConfigFn := func(patch server.ConfigPatch) server.ConfigSnapshot {
		cfgMu.Lock()
		defer cfgMu.Unlock()

		if patch.MinNetProfitPct != nil {
			liveCfg.MinNetProfitPct = *patch.MinNetProfitPct
			spat.SetMinNetProfitPct(*patch.MinNetProfitPct)
			rm.SetMinNetProfitPct(*patch.MinNetProfitPct)
		}
		if patch.MaxPositionUSDT != nil {
			liveCfg.MaxPositionUSDT = *patch.MaxPositionUSDT
			spat.SetMaxPositionUSDT(*patch.MaxPositionUSDT)
		}
		if patch.StalenessThresholdMs != nil {
			d := time.Duration(*patch.StalenessThresholdMs) * time.Millisecond
			liveCfg.StalenessThresholdMs = *patch.StalenessThresholdMs
			spat.SetStaleness(d)
		}
		if patch.ExecutionIntervalMs != nil {
			liveCfg.ExecutionIntervalMs = *patch.ExecutionIntervalMs
			select {
			case intervalCh <- time.Duration(*patch.ExecutionIntervalMs) * time.Millisecond:
			default:
			}
		}
		if patch.CircuitBreakerN != nil {
			liveCfg.CircuitBreakerN = *patch.CircuitBreakerN
			rm.SetConsecutiveLossN(*patch.CircuitBreakerN)
		}
		if patch.CircuitBreakerLossPct != nil {
			liveCfg.CircuitBreakerLossPct = *patch.CircuitBreakerLossPct
			rm.SetLossThreshold(*patch.CircuitBreakerLossPct)
		}
		if patch.DemoMode != nil {
			liveCfg.DemoMode = *patch.DemoMode
			var srcFees map[string]exchange.FeeConfig
			if *patch.DemoMode {
				srcFees = exchange.DemoFees
			} else {
				srcFees = exchange.Fees
			}
			newFees := make(map[string]spatial.FeeConfig, len(srcFees))
			for name, f := range srcFees {
				newFees[name] = spatial.FeeConfig{
					TakerFee:          f.TakerFee,
					SlippageFactor:    f.SlippageFactor,
					WithdrawalBTC:     f.WithdrawalBTC,
					NetworkLatencyBps: f.NetworkLatencyBps,
				}
			}
			spat.SetFees(newFees)
			liveCfg.Fees = toFeeInfoMap(srcFees)
		}

		if patch.Fees != nil {
			for name, fp := range patch.Fees {
				if fp == nil {
					continue
				}
				cur := liveCfg.Fees[name]
				if fp.TakerFee != nil {
					cur.TakerFee = *fp.TakerFee
				}
				if fp.Slippage != nil {
					cur.Slippage = *fp.Slippage
				}
				if fp.WithdrawalBTC != nil {
					cur.WithdrawalBTC = *fp.WithdrawalBTC
				}
				if fp.NetworkLatencyBps != nil {
					cur.NetworkLatencyBps = *fp.NetworkLatencyBps
				}
				liveCfg.Fees[name] = cur
			}
			newFees := make(map[string]spatial.FeeConfig, len(liveCfg.Fees))
			for name, f := range liveCfg.Fees {
				newFees[name] = spatial.FeeConfig{
					TakerFee:          f.TakerFee,
					SlippageFactor:    f.Slippage,
					WithdrawalBTC:     f.WithdrawalBTC,
					NetworkLatencyBps: f.NetworkLatencyBps,
				}
			}
			spat.SetFees(newFees)
		}

		return liveCfg
	}

	// --- Uptime tracker ---

	uptimeTracker := uptime.NewTracker()

	// --- Processing loop ---

	go runProcessingLoop(ctx, cfg, agg, eng, rm, exec, fundingExec, triangularExec, hub, st, spat, intervalCh, uptimeTracker, rec, &recordingEnabled)

	// --- Health snapshot ---

	healthFn := func() map[string]server.ExchangeHealth {
		out := make(map[string]server.ExchangeHealth, len(exchangeNames))
		snap := agg.Snapshot()
		now := time.Now()
		for _, ex := range exchangeNames {
			p, ok := snap[ex]
			h := server.ExchangeHealth{UptimePct: uptimeTracker.UptimePct(ex)}
			if ok {
				age := now.Sub(p.ReceivedAt)
				h.LastUpdateAt = p.ReceivedAt.UTC().Format(time.RFC3339)
				h.LastUpdateAgeMs = age.Milliseconds()
				h.Fresh = age < 10*time.Second
			}
			out[ex] = h
		}
		return out
	}

	// --- Backtest runner ---

	var backtestRunner server.BacktestRunnerIface
	if rec != nil {
		spatialCfg := spatial.Config{
			Fees:               spatFees,
			MinNetProfitPct:    cfg.MinNetProfitPct,
			MaxPositionUSDT:    cfg.MaxPositionUSDT,
			StalenessThreshold: cfg.StalenessThreshold,
		}
		triCfg := triangular.Config{
			TakerFee:     cfg.TriangularTakerFee,
			NoiseRange:   cfg.TriangularNoiseRange,
			SeedRefPrice: cfg.TriangularSeedRefPrice,
			Notional:     cfg.TriangularNotional,
			MinNetProfit: 0.0,
			EmitCooldown: 5 * time.Second,
			Seed:         cfg.TriangularSeed,
		}
		fundCfg := funding.Config{
			Exchanges:        exchangeNames,
			Threshold:        cfg.FundingThreshold,
			PollInterval:     cfg.FundingPollInterval,
			EmitCooldown:     cfg.FundingEmitCooldown,
			Notional:         cfg.FundingNotional,
			BaseDifferential: cfg.FundingBaseDifferential,
			Seed:             cfg.FundingSeed,
		}
		// factories: always include spatial and triangular; funding via replay.
		// The caller (POST /start) passes an empty factories slice to use all.
		_ = spatialCfg
		_ = triCfg
		_ = fundCfg
		btRunner := backtest.NewRunner(rec, st)
		// Wire default factories onto the runner so StartAsync can use them.
		// For this release factories are built per-run from the spec's Strategies field
		// inside the handler. Pre-building them here for reference only.
		backtestRunner = btRunner
	}

	// --- HTTP server ---

	apiHandler := server.NewAPIHandler(st, rm, func() map[string]model.SpreadStats { return spat.SpreadStats() }, getConfigFn, patchConfigFn, healthFn, cfg.AllowedOrigin, len(exchangeNames), backtestRunner, &recordingEnabled)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWS)
	mux.Handle("/api/", apiHandler)
	mux.Handle("/", spaHandler(http.Dir("web/dist")))

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	go func() {
		slog.Info("http server listening", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown error", "err", err)
	}
}

// runProcessingLoop consumes price updates from the aggregator, feeds the engine,
// and on each ExecutionInterval tick executes the top opportunity.
func runProcessingLoop(
	ctx context.Context,
	cfg *config.Config,
	agg *feed.Aggregator,
	eng *engine.Engine,
	rm *risk.RiskManager,
	exec *executor.Executor,
	fundingExec *executor.FundingExecutor,
	triangularExec *executor.TriangularExecutor,
	hub *server.Hub,
	st *store.Store,
	spat *spatial.SpatialStrategy,
	intervalCh <-chan time.Duration,
	uptimeTracker *uptime.Tracker,
	rec *recorder.Recorder,
	recordingEnabled *atomic.Bool,
) {
	ticker := time.NewTicker(cfg.ExecutionInterval)
	defer ticker.Stop()

	// Re-broadcast all known prices every 5s so the frontend keeps timestamps
	// fresh even for low-frequency exchanges (e.g. Kraken only ticks on change).
	snapshotTicker := time.NewTicker(5 * time.Second)
	defer snapshotTicker.Stop()

	// Publish latency stats to connected clients at 1 Hz.
	latencyTicker := time.NewTicker(1 * time.Second)
	defer latencyTicker.Stop()
	var prevProcessed uint64
	prevTickAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case newInterval := <-intervalCh:
			ticker.Reset(newInterval)

		case <-snapshotTicker.C:
			// price_snapshot is unthrottled: carries all exchange prices in one
			// message so the hub doesn't collapse them into a single entry.
			publishPriceSnapshot(hub, agg)

		case now := <-latencyTicker.C:
			cur := eng.ProcessedCount()
			elapsed := now.Sub(prevTickAt).Seconds()
			rate := 0.0
			if elapsed > 0 {
				rate = float64(cur-prevProcessed) / elapsed
			}
			prevProcessed = cur
			prevTickAt = now
			publishLatencyStats(hub, eng, rate)

			// Sample uptime: each exchange is "fresh" if its last update is < 10s old.
			snap := agg.Snapshot()
			for _, ex := range exchangeNames {
				p, ok := snap[ex]
				fresh := ok && now.Sub(p.ReceivedAt) < 10*time.Second
				uptimeTracker.Sample(ex, fresh)
			}
			publishUptimeStats(hub, uptimeTracker)

		case update, ok := <-agg.Updates():
			if !ok {
				return
			}

			// Record the frame before engine processing if recording is enabled.
			if rec != nil && recordingEnabled.Load() {
				if err := rec.RecordFrame(update); err != nil {
					slog.Debug("recorder: RecordFrame failed", "err", err)
				}
			}

			// Engine processes the update (fans out to SpatialStrategy which also
			// updates spread models per pair).
			eng.ProcessUpdate(update)

			// Publish price_update event to WebSocket clients (throttled by hub).
			publishPriceUpdate(hub, update)

			// Publish spread_stats so clients can compute z-scores (throttled by hub).
			publishSpreadStats(hub, spat)

		case <-ticker.C:
			// Attempt to execute the top-scoring opportunity.
			opp, ok := eng.DequeueTop()
			if !ok {
				continue
			}

			if !rm.Evaluate(*opp) {
				opp.Status = types.StatusSkipped
				st.Save(*opp)

				// Publish circuit-breaker event if paused.
				if rm.State() == risk.StatePaused {
					publishCircuitBreaker(hub, rm)
				}
				continue
			}

			var execErr error
			switch opp.Strategy {
			case "funding":
				execErr = fundingExec.Execute(opp)
			case "triangular":
				execErr = triangularExec.Execute(opp)
			default:
				execErr = exec.Execute(opp)
			}
			if execErr != nil {
				slog.Debug("execution failed", "err", execErr)
				continue
			}

			// Record trade result for circuit-breaker tracking.
			netPct, _ := opp.NetProfitPct.Float64()
			rm.RecordTradeResult(netPct)

			// Retrieve the persisted trade for broadcast.
			trades := st.AllTrades()
			if len(trades) > 0 {
				trade := trades[len(trades)-1]
				publishTradeExecuted(hub, trade)
				publishPnL(hub, st)
			}

			publishOpportunity(hub, opp)
		}
	}
}

// spaHandler serves static files from root and falls back to index.html for
// unknown paths so the React router can handle client-side navigation.
func spaHandler(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := fs.Open(r.URL.Path)
		if err != nil {
			// Path not found — serve SPA entry point.
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

// --- Event publishers ---

func publishPriceUpdate(hub *server.Hub, u types.PriceUpdate) {
	hub.Publish(server.Event{
		Type: "price_update",
		Data: map[string]interface{}{
			"exchange": u.Exchange,
			"bid":      u.Bid,
			"ask":      u.Ask,
		},
	})
}

// publishPriceSnapshot sends all known exchange prices in a single unthrottled
// event so the hub does not collapse per-exchange updates into one entry.
func publishPriceSnapshot(hub *server.Hub, agg *feed.Aggregator) {
	snapshot := agg.Snapshot()
	if len(snapshot) == 0 {
		return
	}
	data := make(map[string]interface{}, len(snapshot))
	for name, u := range snapshot {
		data[name] = map[string]interface{}{
			"exchange": u.Exchange,
			"bid":      u.Bid.String(),
			"ask":      u.Ask.String(),
		}
	}
	hub.Publish(server.Event{Type: "price_snapshot", Data: data})
}

func publishOpportunity(hub *server.Hub, opp *types.Opportunity) {
	hub.Publish(server.Event{Type: "opportunity", Data: opp})
}

func publishTradeExecuted(hub *server.Hub, trade types.Trade) {
	hub.Publish(server.Event{Type: "trade_executed", Data: trade})
}

func publishCircuitBreaker(hub *server.Hub, rm *risk.RiskManager) {
	hub.Publish(server.Event{
		Type: "circuit_breaker",
		Data: map[string]string{"state": rm.State().String()},
	})
}

func publishSpreadStats(hub *server.Hub, spat *spatial.SpatialStrategy) {
	stats := spat.SpreadStats()
	result := make([]model.SpreadStats, 0, len(stats))
	for _, s := range stats {
		result = append(result, s)
	}
	hub.Publish(server.Event{Type: "spread_stats", Data: result})
}

func publishPnL(hub *server.Hub, st *store.Store) {
	trades := st.AllTrades()
	total := decimal.Zero
	wins := 0
	for _, t := range trades {
		total = total.Add(t.NetProfit)
		if t.NetProfit.IsPositive() {
			wins++
		}
	}
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(wins) / float64(len(trades))
	}
	b, _ := json.Marshal(map[string]interface{}{
		"total_pnl":   total,
		"trade_count": len(trades),
		"win_rate":    winRate,
	})
	hub.Publish(server.Event{Type: "pnl_update", Data: json.RawMessage(b)})
}

func publishUptimeStats(hub *server.Hub, tracker *uptime.Tracker) {
	hub.Publish(server.Event{
		Type: "uptime_stats",
		Data: tracker.Snapshot(),
	})
}

func publishLatencyStats(hub *server.Hub, eng *engine.Engine, updatesPerSec float64) {
	p50us, p99us, samples := eng.LatencyStats()
	hub.Publish(server.Event{
		Type: "latency_stats",
		Data: map[string]interface{}{
			"p50_us":          p50us,
			"p99_us":          p99us,
			"samples":         samples,
			"updates_per_sec": updatesPerSec,
		},
	})
}

// toFeeInfoMap converts exchange fee configs to the API response shape.
func toFeeInfoMap(src map[string]exchange.FeeConfig) map[string]server.FeeInfo {
	out := make(map[string]server.FeeInfo, len(src))
	for name, f := range src {
		out[name] = server.FeeInfo{TakerFee: f.TakerFee, Slippage: f.SlippageFactor, WithdrawalBTC: f.WithdrawalBTC, NetworkLatencyBps: f.NetworkLatencyBps}
	}
	return out
}
