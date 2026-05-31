package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/risk"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/server"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
)

// --- fakes ---

// fakeRunner implements server.BacktestRunnerIface for testing.
type fakeRunner struct {
	runFn        func(context.Context, backtest.BacktestSpec, []backtest.StrategyFactory) (backtest.RunResult, error)
	startAsyncFn func(context.Context, backtest.BacktestSpec, []backtest.StrategyFactory, chan<- backtest.RunResult) (string, error)
	statusFn     func() backtest.RunStatus
}

func (f *fakeRunner) Run(ctx context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory) (backtest.RunResult, error) {
	if f.runFn != nil {
		return f.runFn(ctx, spec, factories)
	}
	return backtest.RunResult{RunID: "fake-run-id"}, nil
}

func (f *fakeRunner) StartAsync(ctx context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory, resultCh chan<- backtest.RunResult) (string, error) {
	if f.startAsyncFn != nil {
		return f.startAsyncFn(ctx, spec, factories, resultCh)
	}
	// Default: succeed immediately with a known ID.
	return "fake-run-id", nil
}

func (f *fakeRunner) Status() backtest.RunStatus {
	if f.statusFn != nil {
		return f.statusFn()
	}
	return backtest.RunStatus{State: "idle"}
}

// newAPIWithRunner creates an apiHandler with a backtest runner attached.
func newAPIWithRunner(t *testing.T, st *store.Store, rm *risk.RiskManager, runner server.BacktestRunnerIface, recordingEnabled *atomic.Bool) http.Handler {
	t.Helper()
	noop := server.ConfigSnapshot{}
	return server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		nil,
		"http://localhost:3000", 3,
		runner,
		recordingEnabled,
		nil,
	)
}

// --- POST /api/backtest/start ---

func TestBacktestStart_202(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool
	runner := &fakeRunner{}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	body := `{
		"from":"2026-01-01T00:00:00Z",
		"to":"2026-01-02T00:00:00Z",
		"speed":0,
		"strategies":["spatial"],
		"seed":42
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/backtest/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v["run_id"] == "" || v["run_id"] == nil {
		t.Error("expected non-empty run_id in response")
	}
}

func TestBacktestStart_409_AlreadyRunning(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool

	runner := &fakeRunner{
		startAsyncFn: func(ctx context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory, _ chan<- backtest.RunResult) (string, error) {
			return "", backtest.ErrAlreadyRunning
		},
	}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	body := `{"from":"2026-01-01T00:00:00Z","to":"2026-01-02T00:00:00Z","speed":0,"strategies":["spatial"],"seed":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/backtest/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v["error"] != "backtest already running" {
		t.Errorf("expected error=backtest already running, got %v", v["error"])
	}
}

func TestBacktestStart_400_InvalidSpec(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool
	runner := &fakeRunner{}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	// from after to — invalid
	body := `{"from":"2026-01-02T00:00:00Z","to":"2026-01-01T00:00:00Z","speed":0,"strategies":["spatial"],"seed":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/backtest/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- GET /api/backtest/status ---

func TestBacktestStatus_Idle(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool
	runner := &fakeRunner{
		statusFn: func() backtest.RunStatus {
			return backtest.RunStatus{State: "idle"}
		},
	}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v["state"] != "idle" {
		t.Errorf("expected state=idle, got %v", v["state"])
	}
}

// --- GET /api/backtest/runs ---

func TestBacktestRuns_List(t *testing.T) {
	stWithDB := newStoreWithDB(t)
	_, rmLocal := newTestComponents(t)
	var recording atomic.Bool
	runner := &fakeRunner{}

	now := time.Now().UTC()
	for i, id := range []string{"run-a", "run-b"} {
		rec := store.BacktestRunRecord{
			ID:             id,
			StartedAt:      now.Add(time.Duration(i) * time.Minute),
			EndedAt:        now.Add(time.Duration(i)*time.Minute + 5*time.Second),
			FromTS:         now.Add(-1 * time.Hour),
			ToTS:           now,
			StrategiesJSON: `["spatial"]`,
			MetricsJSON:    `{}`,
			Status:         "done",
		}
		if err := stWithDB.SaveBacktestRun(rec); err != nil {
			t.Fatalf("SaveBacktestRun %s: %v", id, err)
		}
	}

	handler := newAPIWithRunner(t, stWithDB, rmLocal, runner, &recording)

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/runs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	runs, ok := v["runs"].([]interface{})
	if !ok {
		t.Fatalf("expected runs array in response, got %T: %v", v["runs"], v["runs"])
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

// --- GET /api/backtest/results/:run_id ---

func TestBacktestResults_404(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool
	runner := &fakeRunner{}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/results/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- POST /api/backtest/recording ---

func TestBacktestRecording_Toggle(t *testing.T) {
	st, rm := newTestComponents(t)
	var recording atomic.Bool
	recording.Store(false)
	runner := &fakeRunner{}

	handler := newAPIWithRunner(t, st, rm, runner, &recording)

	// Enable recording.
	req := httptest.NewRequest(http.MethodPost, "/api/backtest/recording",
		strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v["recording"] != true {
		t.Errorf("expected recording=true, got %v", v["recording"])
	}
	if !recording.Load() {
		t.Error("expected atomic.Bool to be set to true")
	}

	// Disable recording.
	req2 := httptest.NewRequest(http.MethodPost, "/api/backtest/recording",
		strings.NewReader(`{"enabled":false}`))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d", resp2.StatusCode)
	}

	var v2 map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&v2); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v2["recording"] != false {
		t.Errorf("expected recording=false, got %v", v2["recording"])
	}
	if recording.Load() {
		t.Error("expected atomic.Bool to be reset to false")
	}
}

// newStoreWithDB creates a Store backed by a real temp SQLite file.
func newStoreWithDB(t *testing.T) *store.Store {
	t.Helper()
	return store.NewStore(t.TempDir())
}

// TestBacktestStart_FactoryBuilderIsCalled verifies that when a factoryBuilder
// is configured, handleBacktestStart invokes it with the spec and passes the
// resulting factories to StartAsync. Guards against the nil-factories regression
// that would silently produce empty replays.
func TestBacktestStart_FactoryBuilderIsCalled(t *testing.T) {
	st, rm := newTestComponents(t)

	var capturedSpec backtest.BacktestSpec
	var capturedFactories []backtest.StrategyFactory
	runner := &fakeRunner{
		startAsyncFn: func(_ context.Context, spec backtest.BacktestSpec, factories []backtest.StrategyFactory, _ chan<- backtest.RunResult) (string, error) {
			capturedSpec = spec
			capturedFactories = factories
			return "run-abc", nil
		},
	}

	builderCalls := 0
	builder := func(spec backtest.BacktestSpec) []backtest.StrategyFactory {
		builderCalls++
		// Return a non-nil sentinel factory to prove the builder result is forwarded.
		return []backtest.StrategyFactory{
			func(_ int64) engine.StrategyIface { return nil },
		}
	}

	noop := server.ConfigSnapshot{}
	handler := server.NewAPIHandler(
		st, rm,
		func() map[string]model.SpreadStats { return nil },
		func() server.ConfigSnapshot { return noop },
		func(server.ConfigPatch) server.ConfigSnapshot { return noop },
		nil,
		"http://localhost:3000", 3,
		runner,
		nil,
		builder,
	)

	body := `{"from":"2026-05-30T12:00:00Z","to":"2026-05-30T13:00:00Z","speed":1.0,"strategies":["spatial","triangular"],"seed":42}`
	req := httptest.NewRequest(http.MethodPost, "/api/backtest/start", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (body=%s)", w.Code, w.Body.String())
	}
	if builderCalls != 1 {
		t.Errorf("factoryBuilder called %d times, want 1", builderCalls)
	}
	if capturedSpec.Seed != 42 {
		t.Errorf("spec.Seed forwarded: got %d, want 42", capturedSpec.Seed)
	}
	if len(capturedFactories) != 1 {
		t.Errorf("factories forwarded: got %d, want 1", len(capturedFactories))
	}
}
