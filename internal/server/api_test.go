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
		nil, nil, nil,
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
		nil, nil, nil,
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

// --- GET /api/pnl-by-pair ---

func TestAPIPnLByPair_AggregatesByExchangePair(t *testing.T) {
	st, rm := newTestComponents(t)

	// 3 trades on binance->kraken (one losing), 1 trade on okx->binance.
	st.SaveTrade(types.Trade{
		ID: "t1", BuyExchange: "binance", SellExchange: "kraken",
		NetProfit: decimal.NewFromFloat(5.0), Volume: decimal.NewFromFloat(0.01),
		ExecutedAt: time.Now(),
	})
	st.SaveTrade(types.Trade{
		ID: "t2", BuyExchange: "binance", SellExchange: "kraken",
		NetProfit: decimal.NewFromFloat(3.0), Volume: decimal.NewFromFloat(0.01),
		ExecutedAt: time.Now(),
	})
	st.SaveTrade(types.Trade{
		ID: "t3", BuyExchange: "binance", SellExchange: "kraken",
		NetProfit: decimal.NewFromFloat(-1.5), Volume: decimal.NewFromFloat(0.01),
		ExecutedAt: time.Now(),
	})
	st.SaveTrade(types.Trade{
		ID: "t4", BuyExchange: "okx", SellExchange: "binance",
		NetProfit: decimal.NewFromFloat(2.0), Volume: decimal.NewFromFloat(0.01),
		ExecutedAt: time.Now(),
	})

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl-by-pair")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	pairs, ok := body["pairs"].([]interface{})
	if !ok {
		t.Fatal("expected pairs array in response")
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 distinct pairs, got %d", len(pairs))
	}

	// Sorted by total_pnl descending — binance->kraken total = 6.5 first.
	first := pairs[0].(map[string]interface{})
	if first["pair"] != "binance->kraken" {
		t.Errorf("first pair: got %v, want binance->kraken", first["pair"])
	}
	if first["total_pnl"].(float64) != 6.5 {
		t.Errorf("binance->kraken total_pnl: got %v, want 6.5", first["total_pnl"])
	}
	if first["trade_count"].(float64) != 3 {
		t.Errorf("binance->kraken trade_count: got %v, want 3", first["trade_count"])
	}
	// 2 of 3 winners → 0.6666...
	if wr := first["win_rate"].(float64); wr < 0.66 || wr > 0.67 {
		t.Errorf("binance->kraken win_rate: got %v, want ~0.667", wr)
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
		nil, nil, nil,
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

// --- GET /api/pnl-by-strategy ---

func TestAPIPnLByStrategy_Empty(t *testing.T) {
	st, rm := newTestComponents(t)
	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl-by-strategy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	strategies, ok := body["strategies"].([]interface{})
	if !ok {
		t.Fatal("expected strategies array in response")
	}
	if len(strategies) != 0 {
		t.Errorf("expected empty strategies array, got %d entries", len(strategies))
	}
}

func TestAPIPnLByStrategy_SingleStrategy(t *testing.T) {
	st, rm := newTestComponents(t)

	for i := 0; i < 3; i++ {
		st.SaveTrade(types.Trade{
			ID:         fmt.Sprintf("t%d", i),
			Strategy:   "spatial",
			NetProfit:  decimal.NewFromFloat(float64(i+1) * 10),
			Volume:     decimal.NewFromFloat(0.01),
			ExecutedAt: time.Now(),
		})
	}

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl-by-strategy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	strategies, ok := body["strategies"].([]interface{})
	if !ok {
		t.Fatal("expected strategies array in response")
	}
	if len(strategies) != 1 {
		t.Fatalf("expected 1 strategy row, got %d", len(strategies))
	}

	row := strategies[0].(map[string]interface{})
	if row["strategy"] != "spatial" {
		t.Errorf("strategy: got %v, want spatial", row["strategy"])
	}
	if row["trade_count"].(float64) != 3 {
		t.Errorf("trade_count: got %v, want 3", row["trade_count"])
	}
	// total = 10+20+30 = 60
	if row["total_pnl"].(float64) != 60.0 {
		t.Errorf("total_pnl: got %v, want 60", row["total_pnl"])
	}
}

func TestAPIPnLByStrategy_TwoStrategiesSortedDesc(t *testing.T) {
	st, rm := newTestComponents(t)

	// spatial: 2 trades summing to 10
	st.SaveTrade(types.Trade{
		ID: "s1", Strategy: "spatial", NetProfit: decimal.NewFromFloat(6.0), ExecutedAt: time.Now(),
	})
	st.SaveTrade(types.Trade{
		ID: "s2", Strategy: "spatial", NetProfit: decimal.NewFromFloat(4.0), ExecutedAt: time.Now(),
	})
	// triangular: 1 trade summing to 20
	st.SaveTrade(types.Trade{
		ID: "t1", Strategy: "triangular", NetProfit: decimal.NewFromFloat(20.0), ExecutedAt: time.Now(),
	})

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl-by-strategy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	strategies, ok := body["strategies"].([]interface{})
	if !ok {
		t.Fatal("expected strategies array in response")
	}
	if len(strategies) != 2 {
		t.Fatalf("expected 2 strategy rows, got %d", len(strategies))
	}

	// triangular (20) must come first (higher total_pnl).
	first := strategies[0].(map[string]interface{})
	if first["strategy"] != "triangular" {
		t.Errorf("first strategy: got %v, want triangular (sorted desc)", first["strategy"])
	}
	if first["total_pnl"].(float64) != 20.0 {
		t.Errorf("triangular total_pnl: got %v, want 20", first["total_pnl"])
	}
	second := strategies[1].(map[string]interface{})
	if second["strategy"] != "spatial" {
		t.Errorf("second strategy: got %v, want spatial", second["strategy"])
	}
	if second["total_pnl"].(float64) != 10.0 {
		t.Errorf("spatial total_pnl: got %v, want 10", second["total_pnl"])
	}
}

func TestAPIPnLByStrategy_LegacyEmptyStrategySkipped(t *testing.T) {
	st, rm := newTestComponents(t)

	// Legacy trade with no strategy set — should be filtered out so the
	// dashboard does not show a confusing "unknown" bucket.
	st.SaveTrade(types.Trade{
		ID:         "old-1",
		Strategy:   "",
		NetProfit:  decimal.NewFromFloat(5.0),
		ExecutedAt: time.Now(),
	})
	// A tagged trade so the response is not empty.
	st.SaveTrade(types.Trade{
		ID:         "new-1",
		Strategy:   "spatial",
		NetProfit:  decimal.NewFromFloat(2.0),
		ExecutedAt: time.Now(),
	})

	handler := newAPI(t, st, rm, func() map[string]model.SpreadStats { return nil })

	resp, body := getJSON(t, handler, "/api/pnl-by-strategy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	strategies, ok := body["strategies"].([]interface{})
	if !ok {
		t.Fatal("expected strategies array in response")
	}
	if len(strategies) != 1 {
		t.Fatalf("expected 1 strategy row (legacy filtered), got %d", len(strategies))
	}
	row := strategies[0].(map[string]interface{})
	if row["strategy"] != "spatial" {
		t.Errorf("expected only spatial row, got %v", row["strategy"])
	}
}

// --- GET /api/health parse latency fields ---

func TestAPIHealth_IncludesParseLatency(t *testing.T) {
	st, rm := newTestComponents(t)

	healthFn := func() map[string]server.ExchangeHealth {
		return map[string]server.ExchangeHealth{
			"binance": {
				LastUpdateAt: "2026-05-30T12:00:00Z", LastUpdateAgeMs: 100, Fresh: true, UptimePct: 0.99,
				ParseLatencyP50Us: 42, ParseLatencyP99Us: 99,
			},
			"kraken": {
				LastUpdateAt: "2026-05-30T12:00:00Z", LastUpdateAgeMs: 500, Fresh: true, UptimePct: 0.90,
				ParseLatencyP50Us: 0, ParseLatencyP99Us: 0, // cold-start
			},
		}
	}

	noop := server.ConfigSnapshot{}
	handler := server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		healthFn,
		"http://localhost:3000", 2,
		nil, nil, nil,
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
	if _, exists := bn["parse_p50_us"]; !exists {
		t.Error("expected parse_p50_us field in binance health")
	}
	if _, exists := bn["parse_p99_us"]; !exists {
		t.Error("expected parse_p99_us field in binance health")
	}
	if bn["parse_p50_us"].(float64) != 42 {
		t.Errorf("binance parse_p50_us: got %v, want 42", bn["parse_p50_us"])
	}
	if bn["parse_p99_us"].(float64) != 99 {
		t.Errorf("binance parse_p99_us: got %v, want 99", bn["parse_p99_us"])
	}

	kr, ok := exchanges["kraken"].(map[string]interface{})
	if !ok {
		t.Fatal("expected kraken in exchanges")
	}
	if kr["parse_p50_us"].(float64) != 0 {
		t.Errorf("kraken parse_p50_us cold-start: got %v, want 0", kr["parse_p50_us"])
	}
}

// TestAPIHealth_HasL2Field verifies that has_l2 is true for binance/bybit/okx
// and false for the remaining seven exchanges. Spec: D5.
func TestAPIHealth_HasL2Field(t *testing.T) {
	st, rm := newTestComponents(t)

	allExchanges := []string{"binance", "kraken", "bybit", "okx", "gate", "mexc", "bitget", "htx", "cryptocom", "kucoin"}
	l2Exchanges := map[string]bool{"binance": true, "bybit": true, "okx": true}

	healthFn := func() map[string]server.ExchangeHealth {
		out := make(map[string]server.ExchangeHealth, len(allExchanges))
		for _, ex := range allExchanges {
			out[ex] = server.ExchangeHealth{
				Fresh:   true,
				HasL2:   l2Exchanges[ex],
			}
		}
		return out
	}

	noop := server.ConfigSnapshot{}
	handler := server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		healthFn,
		"http://localhost:3000", 10,
		nil, nil, nil,
	)

	resp, body := getJSON(t, handler, "/api/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	exchanges, ok := body["exchanges"].(map[string]interface{})
	if !ok {
		t.Fatal("expected exchanges map in response")
	}

	for _, ex := range allExchanges {
		entry, ok := exchanges[ex].(map[string]interface{})
		if !ok {
			t.Fatalf("expected %s in exchanges", ex)
		}
		wantL2 := l2Exchanges[ex]
		gotL2, exists := entry["has_l2"]
		if !exists {
			t.Errorf("%s: expected has_l2 field", ex)
			continue
		}
		if gotL2.(bool) != wantL2 {
			t.Errorf("%s has_l2: got %v, want %v", ex, gotL2, wantL2)
		}
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
