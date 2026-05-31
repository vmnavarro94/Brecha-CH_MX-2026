package backtest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/executor"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// ErrAlreadyRunning is returned by Run when a backtest is already in progress.
var ErrAlreadyRunning = errors.New("backtest already running")

// BacktestSpec describes the parameters of a single backtest run.
type BacktestSpec struct {
	From       time.Time
	To         time.Time
	Speed      float64 // 0 = maximum, >0 = playback multiplier
	Strategies []string
	Seed       int64
}

// RunResult is the output of a completed backtest run.
type RunResult struct {
	RunID     string
	Metrics   map[string]StrategyMetrics
	StartedAt time.Time
	EndedAt   time.Time
}

// RunStatus is the real-time state of the runner; safe to read via atomic load.
type RunStatus struct {
	State     string  // "idle" | "running" | "done"
	Progress  float64 // 0..1
	CurrentTs int64   // Unix nano of frame being processed
	RunID     string
}

// Runner executes a single backtest replay at a time. Concurrent calls to Run
// are rejected with ErrAlreadyRunning.
type Runner struct {
	mu       sync.Mutex
	status   atomic.Pointer[RunStatus]
	recorder *recorder.Recorder
	store    *store.Store
}

// NewRunner creates a Runner backed by the given recorder and store.
func NewRunner(rec *recorder.Recorder, st *store.Store) *Runner {
	r := &Runner{recorder: rec, store: st}
	idle := &RunStatus{State: "idle"}
	r.status.Store(idle)
	return r
}

// Status returns the current run status. Lock-free; safe to call concurrently.
func (r *Runner) Status() RunStatus {
	return *r.status.Load()
}

// Run executes a full replay against the recorded frames. Returns ErrAlreadyRunning
// if a run is already in progress. Each call produces a fresh set of strategy
// instances via the provided factories.
func (r *Runner) Run(ctx context.Context, spec BacktestSpec, factories []StrategyFactory) (RunResult, error) {
	if !r.mu.TryLock() {
		return RunResult{}, ErrAlreadyRunning
	}
	defer r.mu.Unlock()

	runID := uuid.New().String()
	r.status.Store(&RunStatus{State: "running", RunID: runID})

	startedAt := time.Now()

	// Load frames and funding rates for the replay window.
	frames, err := r.recorder.QueryFrames(spec.From, spec.To)
	if err != nil {
		return RunResult{}, err
	}
	fundingRates, err := r.recorder.QueryFundingRates(spec.From, spec.To)
	if err != nil {
		return RunResult{}, err
	}

	total := len(frames)

	// Build replay primitives.
	clk := NewReplayClock()
	snap := NewReplaySnapshot()

	// Instantiate fresh strategies.
	strategies := make([]engine.StrategyIface, 0, len(factories))
	for _, f := range factories {
		strategies = append(strategies, f(spec.Seed))
	}

	// In-memory store for this run — no persistence.
	tempStore := store.NewStore("")

	// Build engine with replay clock and snapshot.
	eng := engine.NewEngine(
		snap.Snapshot,
		clk,
		engine.Config{OpportunityTTL: 500 * time.Millisecond},
		strategies,
	)

	// Build executors against the temp store and replay clock.
	// Funding and triangular executors are always wired; they only fire if the
	// corresponding factory was registered.
	fundingExec := executor.NewFundingExecutor(tempStore, clk)
	triangularExec := executor.NewTriangularExecutor(tempStore, clk, 0.001, 100)

	// Spatial executor requires a wallet; for replay we skip wallet-gated
	// execution and only record trades via the simpler per-strategy executors.
	// All strategies that are NOT funding/triangular fall through to a no-op
	// (spatial opportunities are recorded only when explicit wallet support is
	// added; for now the run still returns correct metrics from the temp store).
	_ = fundingRates // funding replay is handled via FundingReplayFactory

	// Replay loop.
	var prevFrameTime time.Time

	for i, frame := range frames {
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		default:
		}

		// Advance the clock and snapshot.
		clk.Set(frame.ReceivedAt)
		snap.Update(frame)

		// Speed control.
		if spec.Speed > 0 && !prevFrameTime.IsZero() {
			gap := frame.ReceivedAt.Sub(prevFrameTime)
			delay := time.Duration(float64(gap) / spec.Speed)
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return RunResult{}, ctx.Err()
				case <-timer.C:
				}
			}
		}
		prevFrameTime = frame.ReceivedAt

		eng.ProcessUpdate(frame)

		// Drain all queued opportunities and dispatch by strategy.
		for {
			opp, ok := eng.DequeueTop()
			if !ok {
				break
			}
			switch opp.Strategy {
			case "funding":
				_ = fundingExec.Execute(opp)
			case "triangular":
				_ = triangularExec.Execute(opp)
			default:
				// Spatial and other strategies: record a simple trade directly.
				recordSpatialTrade(tempStore, opp, clk)
			}
		}

		// Update progress atomically.
		progress := 1.0
		if total > 0 {
			progress = float64(i+1) / float64(total)
		}
		r.status.Store(&RunStatus{
			State:     "running",
			Progress:  progress,
			CurrentTs: frame.ReceivedAt.UnixNano(),
			RunID:     runID,
		})
	}

	endedAt := time.Now()

	// Compute metrics across all strategies from the temp store.
	metrics := ComputeMetrics(tempStore.AllTrades(), spec.From, spec.To)

	r.status.Store(&RunStatus{
		State:    "done",
		Progress: 1.0,
		RunID:    runID,
	})

	return RunResult{
		RunID:     runID,
		Metrics:   metrics,
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}, nil
}

// recordSpatialTrade creates a minimal trade record for a spatial opportunity.
// This avoids importing wallet/depth in the backtest package while still
// accumulating trade data for metrics.
func recordSpatialTrade(st *store.Store, opp *types.Opportunity, clk types.Clock) {
	trade := types.Trade{
		ID:          uuid.New().String(),
		OpportunityID: opp.ID,
		BuyExchange:  opp.BuyExchange,
		SellExchange: opp.SellExchange,
		BuyPrice:     opp.BuyPrice,
		SellPrice:    opp.SellPrice,
		Volume:       opp.MaxVolume,
		GrossProfit:  opp.NetProfit,
		Fees:         decimal.Zero,
		NetProfit:    opp.NetProfit,
		ExecutedAt:   clk.Now(),
		Strategy:     opp.Strategy,
	}
	st.SaveTrade(trade)
	opp.Status = types.StatusExecuted
	st.Save(*opp)
}
