package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// BacktestRunnerIface is the minimal interface consumed by the REST handlers.
// *backtest.Runner satisfies it; test fakes can implement it without importing
// the full runner.
type BacktestRunnerIface interface {
	// Run executes a synchronous replay. Returns ErrAlreadyRunning immediately if
	// another run is in progress.
	Run(ctx context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory) (backtest.RunResult, error)
	// StartAsync attempts to acquire the run lock and starts the replay in a
	// background goroutine. Returns (runID, nil) immediately on success, or
	// ("", ErrAlreadyRunning) if already running. The completed RunResult is sent
	// to resultCh (may be nil).
	StartAsync(ctx context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory, resultCh chan<- backtest.RunResult) (string, error)
	// Status returns the current run state atomically.
	Status() backtest.RunStatus
}

// ExchangeHealth describes the live connection state of a single exchange.
type ExchangeHealth struct {
	LastUpdateAt      string  `json:"last_update_at"`
	LastUpdateAgeMs   int64   `json:"last_update_age_ms"`
	Fresh             bool    `json:"fresh"`
	UptimePct         float64 `json:"uptime_pct"`
	ParseLatencyP50Us int64   `json:"parse_p50_us"`
	ParseLatencyP99Us int64   `json:"parse_p99_us"`
	HasL2             bool    `json:"has_l2"`
}

// FeeInfo holds the active trading costs for a single exchange.
type FeeInfo struct {
	TakerFee          float64 `json:"taker_fee"`
	Slippage          float64 `json:"slippage"`
	WithdrawalBTC     float64 `json:"withdrawal_btc"`
	NetworkLatencyBps float64 `json:"network_latency_bps"`
}

// ConfigSnapshot holds the current values for all mutable demo parameters.
type ConfigSnapshot struct {
	DemoMode             bool               `json:"demo_mode"`
	MinNetProfitPct      float64            `json:"min_net_profit_pct"`
	MaxPositionUSDT      float64            `json:"max_position_usdt"`
	StalenessThresholdMs int                `json:"staleness_threshold_ms"`
	ExecutionIntervalMs  int                `json:"execution_interval_ms"`
	CircuitBreakerN      int                `json:"circuit_breaker_n"`
	CircuitBreakerLossPct float64           `json:"circuit_breaker_loss_pct"`
	Fees                 map[string]FeeInfo `json:"fees"`
}

// ConfigPatch carries a partial update for mutable demo parameters.
// Only non-nil fields are applied.
type ConfigPatch struct {
	DemoMode              *bool                    `json:"demo_mode"`
	MinNetProfitPct       *float64                 `json:"min_net_profit_pct"`
	MaxPositionUSDT       *float64                 `json:"max_position_usdt"`
	StalenessThresholdMs  *int                     `json:"staleness_threshold_ms"`
	ExecutionIntervalMs   *int                     `json:"execution_interval_ms"`
	CircuitBreakerN       *int                     `json:"circuit_breaker_n"`
	CircuitBreakerLossPct *float64                 `json:"circuit_breaker_loss_pct"`
	Fees                  map[string]*FeeInfoPatch `json:"fees,omitempty"`
}

// FeeInfoPatch carries a partial update for a single exchange's fee config.
// Only non-nil fields are applied; the others keep their current values.
type FeeInfoPatch struct {
	TakerFee          *float64 `json:"taker_fee,omitempty"`
	Slippage          *float64 `json:"slippage,omitempty"`
	WithdrawalBTC     *float64 `json:"withdrawal_btc,omitempty"`
	NetworkLatencyBps *float64 `json:"network_latency_bps,omitempty"`
}

// apiHandler holds dependencies for the REST API.
type apiHandler struct {
	store             *store.Store
	risk              *risk.RiskManager
	spreadsFn         func() map[string]model.SpreadStats
	getConfigFn       func() ConfigSnapshot
	patchConfigFn     func(ConfigPatch) ConfigSnapshot
	healthFn          func() map[string]ExchangeHealth
	allowedOrigin     string
	exchangeCount     int
	mux               *http.ServeMux
	backtestRunner    BacktestRunnerIface
	recordingEnabled  *atomic.Bool
	// factoryBuilder converts a BacktestSpec (strategies + seed + time range)
	// into a slice of StrategyFactory instances suitable for Runner.StartAsync.
	// nil → handler passes nil factories (empty replay; mainly used by unit tests).
	factoryBuilder func(spec backtest.BacktestSpec) []backtest.StrategyFactory
}

// NewAPIHandler creates an http.Handler that serves all /api/* routes.
// spreadsFn supplies per-pair spread statistics; healthFn supplies per-exchange
// connection state (nil disables /api/health).
// runner and recordingEnabled are optional (pass nil to disable backtest endpoints).
func NewAPIHandler(
	st *store.Store,
	rm *risk.RiskManager,
	spreadsFn func() map[string]model.SpreadStats,
	getConfigFn func() ConfigSnapshot,
	patchConfigFn func(ConfigPatch) ConfigSnapshot,
	healthFn func() map[string]ExchangeHealth,
	allowedOrigin string,
	exchangeCount int,
	runner BacktestRunnerIface,
	recordingEnabled *atomic.Bool,
	factoryBuilder func(spec backtest.BacktestSpec) []backtest.StrategyFactory,
) http.Handler {
	h := &apiHandler{
		store:            st,
		risk:             rm,
		spreadsFn:        spreadsFn,
		getConfigFn:      getConfigFn,
		patchConfigFn:    patchConfigFn,
		healthFn:         healthFn,
		allowedOrigin:    allowedOrigin,
		exchangeCount:    exchangeCount,
		mux:              http.NewServeMux(),
		backtestRunner:   runner,
		recordingEnabled: recordingEnabled,
		factoryBuilder:   factoryBuilder,
	}
	h.mux.HandleFunc("/api/status", h.handleStatus)
	h.mux.HandleFunc("/api/trades", h.handleTrades)
	h.mux.HandleFunc("/api/opportunities", h.handleOpportunities)
	h.mux.HandleFunc("/api/pnl", h.handlePnL)
	h.mux.HandleFunc("/api/spreads", h.handleSpreads)
	h.mux.HandleFunc("/api/config", h.handleConfig)
	h.mux.HandleFunc("/api/health", h.handleHealth)
	h.mux.HandleFunc("/api/pnl-by-pair", h.handlePnLByPair)
	h.mux.HandleFunc("/api/pnl-by-strategy", h.handlePnLByStrategy)
	h.mux.HandleFunc("/api/backtest/start", h.handleBacktestStart)
	h.mux.HandleFunc("/api/backtest/status", h.handleBacktestStatus)
	h.mux.HandleFunc("/api/backtest/runs", h.handleBacktestRuns)
	h.mux.HandleFunc("/api/backtest/results/", h.handleBacktestResults)
	h.mux.HandleFunc("/api/backtest/recording", h.handleBacktestRecording)
	return h
}

// ServeHTTP sets CORS headers and delegates to the internal mux.
func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
	w.Header().Set("Content-Type", "application/json")
	h.mux.ServeHTTP(w, r)
}

// writeJSON serialises v as JSON to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// handleHealth returns per-exchange connection state. Returns 503 if healthFn is nil.
func (h *apiHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if h.healthFn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "health not configured"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exchanges": h.healthFn(),
	})
}

// handleStatus returns system-level status information.
func (h *apiHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	trades := h.store.AllTrades()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"circuit_breaker_state": h.risk.State().String(),
		"exchange_count":        h.exchangeCount,
		"trade_count":           len(trades),
	})
}

// handleTrades returns all trades with optional ?limit=N&offset=M pagination.
func (h *apiHandler) handleTrades(w http.ResponseWriter, r *http.Request) {
	trades := h.store.AllTrades()
	paginated := paginate(len(trades), r, func(i int) interface{} { return trades[i] })
	writeJSON(w, http.StatusOK, paginated)
}

// handleOpportunities returns opportunities with optional ?status= filter.
func (h *apiHandler) handleOpportunities(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")

	var opps []types.Opportunity
	if statusFilter != "" {
		opps = h.store.QueryByStatus(types.OpportunityStatus(statusFilter))
	} else {
		// Return all opportunities across all statuses.
		for _, s := range []types.OpportunityStatus{
			types.StatusDetected, types.StatusExecuted,
			types.StatusSkipped, types.StatusExpired,
		} {
			opps = append(opps, h.store.QueryByStatus(s)...)
		}
	}

	if opps == nil {
		opps = []types.Opportunity{}
	}

	result := make([]interface{}, len(opps))
	for i, o := range opps {
		result[i] = o
	}
	writeJSON(w, http.StatusOK, result)
}

// pnlResponse holds the /api/pnl payload.
type pnlResponse struct {
	TotalPnL   decimal.Decimal `json:"total_pnl"`
	TradeCount int             `json:"trade_count"`
	WinRate    float64         `json:"win_rate"`
}

// handlePnL returns aggregate profit/loss statistics.
func (h *apiHandler) handlePnL(w http.ResponseWriter, r *http.Request) {
	trades := h.store.AllTrades()
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
	writeJSON(w, http.StatusOK, pnlResponse{
		TotalPnL:   total,
		TradeCount: len(trades),
		WinRate:    winRate,
	})
}

// pairStats holds aggregated P&L for one exchange pair.
type pairStats struct {
	Pair        string  `json:"pair"`
	TotalPnL    float64 `json:"total_pnl"`
	TradeCount  int     `json:"trade_count"`
	WinRate     float64 `json:"win_rate"`
	TotalVolume float64 `json:"total_volume"`
}

// handlePnLByPair groups all trades by (buyEx -> sellEx) and returns the
// aggregate P&L per pair, sorted by total_pnl descending.
func (h *apiHandler) handlePnLByPair(w http.ResponseWriter, r *http.Request) {
	trades := h.store.AllTrades()
	type agg struct {
		net    decimal.Decimal
		vol    decimal.Decimal
		count  int
		wins   int
	}
	by := make(map[string]*agg)
	for _, t := range trades {
		key := string(t.BuyExchange) + "->" + string(t.SellExchange)
		a, ok := by[key]
		if !ok {
			a = &agg{}
			by[key] = a
		}
		a.net = a.net.Add(t.NetProfit)
		a.vol = a.vol.Add(t.Volume)
		a.count++
		if t.NetProfit.IsPositive() {
			a.wins++
		}
	}

	result := make([]pairStats, 0, len(by))
	for k, a := range by {
		netF, _ := a.net.Float64()
		volF, _ := a.vol.Float64()
		wr := 0.0
		if a.count > 0 {
			wr = float64(a.wins) / float64(a.count)
		}
		result = append(result, pairStats{
			Pair:        k,
			TotalPnL:    netF,
			TradeCount:  a.count,
			WinRate:     wr,
			TotalVolume: volF,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalPnL > result[j].TotalPnL
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pairs": result,
	})
}

// handleSpreads returns the current per-pair spread statistics.
func (h *apiHandler) handleSpreads(w http.ResponseWriter, r *http.Request) {
	spreadsMap := h.spreadsFn()
	result := make([]model.SpreadStats, 0, len(spreadsMap))
	for _, s := range spreadsMap {
		result = append(result, s)
	}
	writeJSON(w, http.StatusOK, result)
}

// handleConfig handles GET and PATCH /api/config for live parameter tweaking.
func (h *apiHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.getConfigFn())
	case http.MethodPatch:
		var patch ConfigPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		writeJSON(w, http.StatusOK, h.patchConfigFn(patch))
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// strategyStats holds aggregated P&L for one strategy bucket.
type strategyStats struct {
	Strategy    string  `json:"strategy"`
	TotalPnL    float64 `json:"total_pnl"`
	TradeCount  int     `json:"trade_count"`
	WinRate     float64 `json:"win_rate"`
	TotalVolume float64 `json:"total_volume"`
}

// handlePnLByStrategy groups all trades by Trade.Strategy and returns aggregate P&L
// per strategy sorted by total_pnl descending. Trades with Strategy=="" are
// legacy records persisted before strategy tagging was introduced and are
// skipped entirely so they do not pollute the dashboard.
func (h *apiHandler) handlePnLByStrategy(w http.ResponseWriter, r *http.Request) {
	trades := h.store.AllTrades()
	type agg struct {
		net   decimal.Decimal
		vol   decimal.Decimal
		count int
		wins  int
	}
	by := make(map[string]*agg)
	for _, t := range trades {
		if t.Strategy == "" {
			continue
		}
		a, ok := by[t.Strategy]
		if !ok {
			a = &agg{}
			by[t.Strategy] = a
		}
		a.net = a.net.Add(t.NetProfit)
		a.vol = a.vol.Add(t.Volume)
		a.count++
		if t.NetProfit.IsPositive() {
			a.wins++
		}
	}

	result := make([]strategyStats, 0, len(by))
	for k, a := range by {
		netF, _ := a.net.Float64()
		volF, _ := a.vol.Float64()
		wr := 0.0
		if a.count > 0 {
			wr = float64(a.wins) / float64(a.count)
		}
		result = append(result, strategyStats{
			Strategy:    k,
			TotalPnL:    netF,
			TradeCount:  a.count,
			WinRate:     wr,
			TotalVolume: volF,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalPnL > result[j].TotalPnL
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"strategies": result,
	})
}

// paginate slices the elements [0, total) according to ?limit= and ?offset= query params
// and returns a []interface{} of the requested window.
func paginate(total int, r *http.Request, elem func(i int) interface{}) []interface{} {
	q := r.URL.Query()

	limit := total
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	if offset >= total {
		return []interface{}{}
	}

	end := offset + limit
	if end > total {
		end = total
	}

	result := make([]interface{}, 0, end-offset)
	for i := offset; i < end; i++ {
		result = append(result, elem(i))
	}
	return result
}
