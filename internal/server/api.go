package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// FeeInfo holds the active taker fee, slippage and withdrawal cost for a single exchange.
type FeeInfo struct {
	TakerFee       float64 `json:"taker_fee"`
	Slippage       float64 `json:"slippage"`
	WithdrawalBTC  float64 `json:"withdrawal_btc"`
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
	TakerFee      *float64 `json:"taker_fee,omitempty"`
	Slippage      *float64 `json:"slippage,omitempty"`
	WithdrawalBTC *float64 `json:"withdrawal_btc,omitempty"`
}

// apiHandler holds dependencies for the REST API.
type apiHandler struct {
	store         *store.Store
	risk          *risk.RiskManager
	spreadsFn     func() map[string]model.SpreadStats
	getConfigFn   func() ConfigSnapshot
	patchConfigFn func(ConfigPatch) ConfigSnapshot
	allowedOrigin string
	exchangeCount int
	mux           *http.ServeMux
}

// NewAPIHandler creates an http.Handler that serves all /api/* routes.
// spreadsFn is called on each /api/spreads request to get current per-pair statistics.
// getConfigFn and patchConfigFn power the /api/config endpoint.
func NewAPIHandler(
	st *store.Store,
	rm *risk.RiskManager,
	spreadsFn func() map[string]model.SpreadStats,
	getConfigFn func() ConfigSnapshot,
	patchConfigFn func(ConfigPatch) ConfigSnapshot,
	allowedOrigin string,
	exchangeCount int,
) http.Handler {
	h := &apiHandler{
		store:         st,
		risk:          rm,
		spreadsFn:     spreadsFn,
		getConfigFn:   getConfigFn,
		patchConfigFn: patchConfigFn,
		allowedOrigin: allowedOrigin,
		exchangeCount: exchangeCount,
		mux:           http.NewServeMux(),
	}
	h.mux.HandleFunc("/api/status", h.handleStatus)
	h.mux.HandleFunc("/api/trades", h.handleTrades)
	h.mux.HandleFunc("/api/opportunities", h.handleOpportunities)
	h.mux.HandleFunc("/api/pnl", h.handlePnL)
	h.mux.HandleFunc("/api/spreads", h.handleSpreads)
	h.mux.HandleFunc("/api/config", h.handleConfig)
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
