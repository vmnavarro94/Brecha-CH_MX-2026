package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	st := store.NewStore("")
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
	noop := server.ConfigSnapshot{}
	return server.NewAPIHandler(
		st, rm, spreadsFn,
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		nil,
		"http://localhost:3000", 3,
	)
}

func newAPIWithConfigFn(
	t *testing.T,
	st *store.Store,
	rm *risk.RiskManager,
	getCfg func() server.ConfigSnapshot,
	patchCfg func(server.ConfigPatch) server.ConfigSnapshot,
) http.Handler {
	t.Helper()
	return server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		getCfg,
		patchCfg,
		nil,
		"http://localhost:3000", 3,
	)
}

func patchJSON(t *testing.T, handler http.Handler, path string, body string) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	resp := w.Result()
	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp, v
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

	if body["circuit_breaker_state"] != "active" {
		t.Errorf("expected circuit_breaker_state=active, got %v", body["circuit_breaker_state"])
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

// --- GET /api/health ---

func TestAPIHealth_PerExchange(t *testing.T) {
	st, rm := newTestComponents(t)

	healthFn := func() map[string]server.ExchangeHealth {
		return map[string]server.ExchangeHealth{
			"binance": {LastUpdateAt: "2026-05-30T12:00:00Z", LastUpdateAgeMs: 120, Fresh: true, UptimePct: 0.99},
			"kraken":  {LastUpdateAt: "2026-05-30T11:59:50Z", LastUpdateAgeMs: 10000, Fresh: false, UptimePct: 0.85},
		}
	}

	noop := server.ConfigSnapshot{}
	handler := server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		healthFn,
		"http://localhost:3000", 3,
	)

	resp, body := getJSON(t, handler, "/api/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	exchanges, ok := body["exchanges"].(map[string]interface{})
	if !ok {
		t.Fatal("expected exchanges map in response")
	}
	bn, ok := exchanges["binance"].(map[string]interface{})
	if !ok {
		t.Fatal("expected binance in exchanges")
	}
	if bn["fresh"] != true {
		t.Errorf("binance fresh: got %v, want true", bn["fresh"])
	}
	if bn["uptime_pct"].(float64) != 0.99 {
		t.Errorf("binance uptime_pct: got %v, want 0.99", bn["uptime_pct"])
	}
	kr := exchanges["kraken"].(map[string]interface{})
	if kr["fresh"] != false {
		t.Errorf("kraken fresh: got %v, want false", kr["fresh"])
	}
}

// --- PATCH /api/config (fees) ---

func TestAPIConfig_PatchFees(t *testing.T) {
	st, rm := newTestComponents(t)

	snap := server.ConfigSnapshot{
		Fees: map[string]server.FeeInfo{
			"binance": {TakerFee: 0.001, Slippage: 0.0002},
		},
	}

	handler := newAPIWithConfigFn(t, st, rm,
		func() server.ConfigSnapshot { return snap },
		func(p server.ConfigPatch) server.ConfigSnapshot {
			if p.Fees != nil {
				for name, fp := range p.Fees {
					if fp == nil {
						continue
					}
					cur := snap.Fees[name]
					if fp.TakerFee != nil {
						cur.TakerFee = *fp.TakerFee
					}
					if fp.Slippage != nil {
						cur.Slippage = *fp.Slippage
					}
					snap.Fees[name] = cur
				}
			}
			return snap
		},
	)

	resp, body := patchJSON(t, handler, "/api/config",
		`{"fees":{"binance":{"taker_fee":0.0005}}}`,
	)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	fees, ok := body["fees"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees key in response")
	}
	binance, ok := fees["binance"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees.binance in response")
	}
	if binance["taker_fee"].(float64) != 0.0005 {
		t.Errorf("expected taker_fee=0.0005, got %v", binance["taker_fee"])
	}
	if binance["slippage"].(float64) != 0.0002 {
		t.Errorf("expected slippage=0.0002 (unchanged), got %v", binance["slippage"])
	}
}

func TestAPIConfig_PatchFees_SlippageOnly(t *testing.T) {
	st, rm := newTestComponents(t)

	snap := server.ConfigSnapshot{
		Fees: map[string]server.FeeInfo{
			"okx": {TakerFee: 0.001, Slippage: 0.0002},
		},
	}

	handler := newAPIWithConfigFn(t, st, rm,
		func() server.ConfigSnapshot { return snap },
		func(p server.ConfigPatch) server.ConfigSnapshot {
			if p.Fees != nil {
				for name, fp := range p.Fees {
					if fp == nil {
						continue
					}
					cur := snap.Fees[name]
					if fp.TakerFee != nil {
						cur.TakerFee = *fp.TakerFee
					}
					if fp.Slippage != nil {
						cur.Slippage = *fp.Slippage
					}
					snap.Fees[name] = cur
				}
			}
			return snap
		},
	)

	resp, body := patchJSON(t, handler, "/api/config",
		`{"fees":{"okx":{"slippage":0.0005}}}`,
	)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	fees, ok := body["fees"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees key in response")
	}
	okx, ok := fees["okx"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fees.okx in response")
	}

	if okx["taker_fee"].(float64) != 0.001 {
		t.Errorf("expected taker_fee=0.001 (unchanged), got %v", okx["taker_fee"])
	}
	if okx["slippage"].(float64) != 0.0005 {
		t.Errorf("expected slippage=0.0005, got %v", okx["slippage"])
	}
}
