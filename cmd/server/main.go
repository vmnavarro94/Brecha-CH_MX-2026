package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/config"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/exchange"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/executor"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/feed"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/server"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/wallet"
)

var exchangeNames = []string{"binance", "kraken", "bybit"}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// --- Infrastructure ---

	clk := types.RealClock{}

	st := store.NewStore()

	w := wallet.NewMultiWallet(exchangeNames, map[string]float64{
		"USDT": cfg.InitialUSDTPerExchange,
		"BTC":  cfg.InitialBTCPerExchange,
	})

	// --- Exchange connectors ---

	connectors := []exchange.Connector{
		exchange.NewBinance(cfg.BinanceWSURL),
		exchange.NewKraken(cfg.KrakenWSURL),
		exchange.NewBybit(cfg.BybitWSURL),
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

	// spreadStats builds a SpreadStats snapshot from the current model state.
	spreadStatsFn := func() map[string]model.SpreadStats {
		out := make(map[string]model.SpreadStats, len(spreadModels))
		for k, m := range spreadModels {
			out[k] = model.SpreadStats{
				Pair:    k,
				Mean:    m.Mean(),
				Std:     m.Std(),
				Samples: m.N(),
			}
		}
		return out
	}

	// --- Engine ---

	eng := engine.NewEngine(
		agg.Snapshot,
		spreadModels,
		clk,
		engine.Config{
			Fees: map[string]engine.FeeConfig{
				"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
				"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
				"bybit":   {TakerFee: 0.001, SlippageFactor: 0.0002},
			},
			MinNetProfitPct:    cfg.MinNetProfitPct,
			OpportunityTTL:     cfg.OpportunityTTL,
			StalenessThreshold: cfg.StalenessThreshold,
		},
	)

	// --- Risk Manager ---

	rm := risk.NewRiskManager(risk.Config{
		MinNetProfitPct:  cfg.MinNetProfitPct,
		MaxPositionUSDT:  cfg.MaxPositionUSDT,
		LossThreshold:    cfg.CircuitBreakerLossPct,
		ConsecutiveLossN: cfg.CircuitBreakerN,
		PauseDuration:    time.Duration(cfg.CircuitBreakerPauseMins) * time.Minute,
	}, clk)

	// --- Executor ---

	exec := executor.NewExecutor(w, st, agg.Snapshot, clk, cfg.StalenessThreshold)

	// --- WebSocket Hub ---

	hub := server.NewHub()
	go hub.Run()

	// --- Processing loop ---

	go runProcessingLoop(ctx, cfg, agg, eng, rm, exec, hub, st, spreadModels)

	// --- HTTP server ---

	apiHandler := server.NewAPIHandler(st, rm, spreadStatsFn, cfg.AllowedOrigin)

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

// runProcessingLoop consumes price updates from the aggregator, feeds the engine and
// spread models, and on each ExecutionInterval tick executes the top opportunity.
func runProcessingLoop(
	ctx context.Context,
	cfg *config.Config,
	agg *feed.Aggregator,
	eng *engine.Engine,
	rm *risk.RiskManager,
	exec *executor.Executor,
	hub *server.Hub,
	st *store.Store,
	spreadModels map[string]*model.SpreadModel,
) {
	ticker := time.NewTicker(cfg.ExecutionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case update, ok := <-agg.Updates():
			if !ok {
				return
			}

			// Feed spread models with the net profit percentage approximation.
			// Use ask as a proxy spread contribution; the model learns over time.
			ask, _ := update.Ask.Float64()
			bid, _ := update.Bid.Float64()
			if ask > 0 {
				midSpread := (ask - bid) / ask
				for key, sm := range spreadModels {
					// Only update models relevant to this exchange.
					if containsExchange(key, update.Exchange) {
						sm.Update(midSpread)
					}
				}
			}

			// Engine processes the update.
			eng.ProcessUpdate(update)

			// Publish price_update event to WebSocket clients (throttled by hub).
			publishPriceUpdate(hub, update)

			// Publish spread_stats so clients can compute z-scores (throttled by hub).
			publishSpreadStats(hub, spreadModels)

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

			err := exec.Execute(opp)
			if err != nil {
				slog.Debug("execution failed", "err", err)
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

// containsExchange returns true if the pair key "A-B" contains the exchange name.
func containsExchange(key, exchange string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] == '-' {
			if key[:i] == exchange || key[i+1:] == exchange {
				return true
			}
		}
	}
	return false
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

func publishSpreadStats(hub *server.Hub, spreadModels map[string]*model.SpreadModel) {
	result := make([]model.SpreadStats, 0, len(spreadModels))
	for k, m := range spreadModels {
		result = append(result, model.SpreadStats{
			Pair:    k,
			Mean:    m.Mean(),
			Std:     m.Std(),
			Samples: m.N(),
		})
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
