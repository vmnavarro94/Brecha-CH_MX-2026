package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
)

// startRequest is the JSON body for POST /api/backtest/start.
type startRequest struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Speed      float64   `json:"speed"`
	Strategies []string  `json:"strategies"`
	Seed       int64     `json:"seed"`
}

// handleBacktestStart handles POST /api/backtest/start.
// Returns 202 with {run_id}, 400 for invalid spec, 409 if already running,
// 503 if runner is not configured.
func (h *apiHandler) handleBacktestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h.backtestRunner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "backtest runner not configured"})
		return
	}

	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.From.IsZero() || req.To.IsZero() || !req.From.Before(req.To) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid spec: from must be before to"})
		return
	}

	spec := backtest.BacktestSpec{
		From:       req.From,
		To:         req.To,
		Speed:      req.Speed,
		Strategies: req.Strategies,
		Seed:       req.Seed,
	}

	// resultCh receives the finished RunResult so we can persist it.
	resultCh := make(chan backtest.RunResult, 1)
	strategiesJSON, _ := json.Marshal(req.Strategies)

	// Build factories from the spec. If no builder was configured (e.g. in
	// unit tests), pass nil — the runner accepts that as an empty replay.
	var factories []backtest.StrategyFactory
	if h.factoryBuilder != nil {
		factories = h.factoryBuilder(spec)
	}

	// StartAsync acquires the lock and returns (runID, nil) immediately, or
	// ("", ErrAlreadyRunning) if another run is in progress.
	runID, err := h.backtestRunner.StartAsync(context.Background(), spec, factories, resultCh)
	if errors.Is(err, backtest.ErrAlreadyRunning) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "backtest already running"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Fire-and-forget: persist the run result when it completes.
	go func() {
		result, ok := <-resultCh
		if !ok {
			return
		}
		metricsJSON, _ := store.MarshalBacktestMetrics(result.Metrics)
		rec := store.BacktestRunRecord{
			ID:             result.RunID,
			StartedAt:      result.StartedAt,
			EndedAt:        result.EndedAt,
			FromTS:         req.From,
			ToTS:           req.To,
			StrategiesJSON: string(strategiesJSON),
			MetricsJSON:    metricsJSON,
			Status:         "done",
		}
		_ = h.store.SaveBacktestRun(rec)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

// handleBacktestStatus handles GET /api/backtest/status.
func (h *apiHandler) handleBacktestStatus(w http.ResponseWriter, r *http.Request) {
	if h.backtestRunner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "backtest runner not configured"})
		return
	}
	s := h.backtestRunner.Status()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state":      s.State,
		"progress":   s.Progress,
		"current_ts": s.CurrentTs,
		"run_id":     s.RunID,
	})
}

// handleBacktestRuns handles GET /api/backtest/runs.
func (h *apiHandler) handleBacktestRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.ListBacktestRuns()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []store.BacktestRunRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// handleBacktestResults handles GET /api/backtest/results/:run_id.
func (h *apiHandler) handleBacktestResults(w http.ResponseWriter, r *http.Request) {
	// Extract run_id from path: /api/backtest/results/{run_id}
	runID := strings.TrimPrefix(r.URL.Path, "/api/backtest/results/")
	if runID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run_id required"})
		return
	}

	rec, err := h.store.GetBacktestRun(runID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Decode metrics from JSON.
	var metrics map[string]backtest.StrategyMetrics
	if rec.MetricsJSON != "" {
		if err := json.Unmarshal([]byte(rec.MetricsJSON), &metrics); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid metrics JSON"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run_id":     rec.ID,
		"started_at": rec.StartedAt,
		"ended_at":   rec.EndedAt,
		"from":       rec.FromTS,
		"to":         rec.ToTS,
		"status":     rec.Status,
		"metrics":    metrics,
	})
}

// recordingRequest is the JSON body for POST /api/backtest/recording.
type recordingRequest struct {
	Enabled bool `json:"enabled"`
}

// handleBacktestRecording handles POST /api/backtest/recording.
func (h *apiHandler) handleBacktestRecording(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h.recordingEnabled == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "recording not configured"})
		return
	}

	var req recordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	h.recordingEnabled.Store(req.Enabled)
	writeJSON(w, http.StatusOK, map[string]bool{"recording": req.Enabled})
}
