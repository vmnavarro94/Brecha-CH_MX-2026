package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/server"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// --- helpers ---

// fakeClock is a Clock implementation that returns a fixed time.
type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

func newTestComponents(t *testing.T) (*store.Store, *risk.RiskManager) {
	t.Helper()
	st := store.NewStore()
	rm := risk.NewRiskManager(risk.Config{
		MinNetProfitPct:  0.0015,
		MaxPositionUSDT:  1000,
		LossThreshold:    -0.005,
		ConsecutiveLossN: 5,
		PauseDuration:    5 * time.Minute,
	}, fakeClock{time.Now()})
	return st, rm
}

func newAPI(t *testing.T, st *store.Store, rm *risk.RiskManager, spreadsFn func() map[string]model.SpreadStats) http.Handler {
	t.Helper()
	return server.NewAPIHandler(st, rm, spreadsFn, "http://localhost:3000")
}

func getJSON(t *testing.T, handler http.Handler, path string) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp, v
}

func getJSONArray(t *testing.T, handler http.Handler, path string) (*http.Response, []interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	var v []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp, v
}

// --- GET /api/status ---

func TestAPIStatus_200(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil }, )

	resp, body := getJSON(t, handler, "/api/status")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	for _, key := range []string{"circuit_breaker_state", "exchange_count", "trade_count"} {
		if _, ok := body[key]; !ok {
			t.Errorf("expected key %q in response", key)
		}
	}
}

func TestAPIStatus_CircuitBreakerState(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	_, body := getJSON(t, handler, "/api/status")

	if body["circuit_breaker_state"] != "Active" {
		t.Errorf("expected circuit_breaker_state=Active, got %v", body["circuit_breaker_state"])
	}
}

// --- GET /api/trades ---

func TestAPITrades_EmptyArray(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, arr := getJSONArray(t, handler, "/api/trades")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if arr == nil {
		t.Error("expected non-nil array")
	}
}

func TestAPITrades_Pagination(t *testing.T) {
	st, rm := newTestComponents(t)

	// Seed 5 trades.
	for i := 0; i < 5; i++ {
		st.SaveTrade(types.Trade{
			ID:          fmt.Sprintf("trade-%d", i),
			ExecutedAt:  time.Now(),
			GrossProfit: decimal.NewFromFloat(float64(i) * 10),
		})
	}

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	_, arr := getJSONArray(t, handler, "/api/trades?limit=2&offset=1")

	if len(arr) != 2 {
		t.Errorf("expected 2 items with limit=2&offset=1, got %d", len(arr))
	}
}

// --- GET /api/opportunities ---

func TestAPIOpportunities_EmptyArray(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, arr := getJSONArray(t, handler, "/api/opportunities")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if arr == nil {
		t.Error("expected non-nil array")
	}
}

func TestAPIOpportunities_StatusFilter(t *testing.T) {
	st, rm := newTestComponents(t)

	st.Save(types.Opportunity{ID: "o1", Status: types.StatusExecuted})
	st.Save(types.Opportunity{ID: "o2", Status: types.StatusDetected})
	st.Save(types.Opportunity{ID: "o3", Status: types.StatusExecuted})

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	_, arr := getJSONArray(t, handler, "/api/opportunities?status=executed")

	if len(arr) != 2 {
		t.Errorf("expected 2 executed opportunities, got %d", len(arr))
	}
}

// --- GET /api/pnl ---

func TestAPIPnL_Fields(t *testing.T) {
	st, rm := newTestComponents(t)

	// Seed a profitable trade.
	st.SaveTrade(types.Trade{
		ID:         "t1",
		NetProfit:  decimal.NewFromFloat(15.5),
		ExecutedAt: time.Now(),
	})

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	for _, key := range []string{"total_pnl", "trade_count", "win_rate"} {
		if _, ok := body[key]; !ok {
			t.Errorf("expected key %q in pnl response", key)
		}
	}
	// trade_count should reflect the seeded trade.
	if body["trade_count"].(float64) != 1 {
		t.Errorf("expected trade_count=1, got %v", body["trade_count"])
	}
}

// --- GET /api/spreads ---

func TestAPISpreads_Fields(t *testing.T) {
	st, rm := newTestComponents(t)

	spreadsFn := func() map[string]model.SpreadStats {
		return map[string]model.SpreadStats{
			"binance-kraken": {Pair: "binance-kraken", Mean: 0.002, Std: 0.0003, Samples: 200},
		}
	}

	handler := newAPI(t, st, rm, spreadsFn)

	resp, arr := getJSONArray(t, handler, "/api/spreads")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(arr) != 1 {
		t.Errorf("expected 1 spread entry, got %d", len(arr))
	}
}

// --- CORS header ---

func TestAPIStatus_CORSHeader(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "http://localhost:3000" {
		t.Errorf("expected CORS origin=http://localhost:3000, got %q", origin)
	}
}
