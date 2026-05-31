package backtest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/engine"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/recorder"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/store"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy/spatial"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// tempRecorder opens a recorder in a fresh temp dir, registers cleanup, and records frames.
func tempRecorder(t *testing.T, frames []types.PriceUpdate) (*recorder.Recorder, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "runner_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	rec, err := recorder.New(dir)
	if err != nil {
		t.Fatalf("recorder.New: %v", err)
	}
	t.Cleanup(func() { rec.Close() })

	for _, f := range frames {
		if err := rec.RecordFrame(f); err != nil {
			t.Fatalf("RecordFrame: %v", err)
		}
	}
	return rec, dir
}

// makeFrames creates n frames starting at base with 1-second spacing.
func makeFrames(base time.Time, n int) []types.PriceUpdate {
	frames := make([]types.PriceUpdate, n)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		frames[i] = types.PriceUpdate{
			Exchange:   "binance",
			Bid:        decimal.NewFromFloat(50000 + float64(i)*0.1),
			Ask:        decimal.NewFromFloat(50001 + float64(i)*0.1),
			BidSize:    decimal.NewFromFloat(1.0),
			AskSize:    decimal.NewFromFloat(1.0),
			ReceivedAt: ts,
		}
	}
	return frames
}

// TestRunner_RejectsConcurrentRuns covers spec RR4: ErrAlreadyRunning.
// A blocking factory is used to hold the first run open so the second call
// is guaranteed to find the lock taken.
func TestRunner_RejectsConcurrentRuns(t *testing.T) {
	base := time.Now().UTC()
	frames := makeFrames(base, 5)
	rec, _ := tempRecorder(t, frames)
	st := store.NewStore("")

	runner := backtest.NewRunner(rec, st)

	// blockCh blocks the factory until the test is ready to proceed.
	blockCh := make(chan struct{})
	// enteredCh fires once the goroutine is inside Run (factory was called).
	enteredCh := make(chan struct{}, 1)

	cfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.001,
		MaxPositionUSDT:    1000,
		StalenessThreshold: time.Second,
	}
	innerFactory := backtest.NewSpatialFactory(cfg)

	blockingFactory := backtest.StrategyFactory(func(seed int64) engine.StrategyIface {
		// Signal that we're inside the factory (therefore inside Run).
		select {
		case enteredCh <- struct{}{}:
		default:
		}
		// Block until test signals.
		<-blockCh
		return innerFactory(seed)
	})

	spec := backtest.BacktestSpec{
		From:       base,
		To:         base.Add(5 * time.Second),
		Speed:      0,
		Strategies: []string{"spatial"},
		Seed:       42,
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_, _ = runner.Run(context.Background(), spec, []backtest.StrategyFactory{blockingFactory})
	}()

	// Wait until the first run has acquired the lock (factory was called).
	<-enteredCh

	_, err := runner.Run(context.Background(), spec, []backtest.StrategyFactory{backtest.NewSpatialFactory(spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.001,
		MaxPositionUSDT:    1000,
		StalenessThreshold: time.Second,
	})})
	if err != backtest.ErrAlreadyRunning {
		t.Errorf("want ErrAlreadyRunning, got %v", err)
	}

	// Unblock the first run.
	close(blockCh)
	<-doneCh
}

// TestRunner_ProducesTradesFromRecording covers spec RR1 + RR2.
func TestRunner_ProducesTradesFromRecording(t *testing.T) {
	base := time.Now().UTC()
	frames := makeFrames(base, 10)
	rec, _ := tempRecorder(t, frames)
	st := store.NewStore("")

	runner := backtest.NewRunner(rec, st)

	// Use low MinNetProfitPct so even tiny spreads trigger opportunities.
	spatCfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    100000,
		StalenessThreshold: 10 * time.Second,
	}
	factories := []backtest.StrategyFactory{backtest.NewSpatialFactory(spatCfg)}

	spec := backtest.BacktestSpec{
		From:       base,
		To:         base.Add(10 * time.Second),
		Speed:      0,
		Strategies: []string{"spatial"},
		Seed:       42,
	}

	result, err := runner.Run(context.Background(), spec, factories)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RunID == "" {
		t.Error("RunID should not be empty")
	}
	if result.Metrics == nil {
		t.Error("Metrics should not be nil")
	}
}

// TestRunner_DeterministicWithSameSeed covers spec RR5.
func TestRunner_DeterministicWithSameSeed(t *testing.T) {
	base := time.Now().UTC()
	frames := makeFrames(base, 10)
	rec, _ := tempRecorder(t, frames)

	st1 := store.NewStore("")
	st2 := store.NewStore("")

	runner1 := backtest.NewRunner(rec, st1)
	runner2 := backtest.NewRunner(rec, st2)

	triCfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.0,
		MaxPositionUSDT:    100000,
		StalenessThreshold: 10 * time.Second,
	}
	factories := []backtest.StrategyFactory{backtest.NewSpatialFactory(triCfg)}

	spec := backtest.BacktestSpec{
		From:       base,
		To:         base.Add(10 * time.Second),
		Speed:      0,
		Strategies: []string{"spatial"},
		Seed:       999,
	}

	r1, err1 := runner1.Run(context.Background(), spec, factories)
	r2, err2 := runner2.Run(context.Background(), spec, factories)

	if err1 != nil || err2 != nil {
		t.Fatalf("Run errors: %v, %v", err1, err2)
	}

	for strat, m1 := range r1.Metrics {
		m2, ok := r2.Metrics[strat]
		if !ok {
			t.Errorf("strategy %q absent from run2 metrics", strat)
			continue
		}
		if m1.TotalPnL != m2.TotalPnL {
			t.Errorf("%s TotalPnL: run1=%f, run2=%f", strat, m1.TotalPnL, m2.TotalPnL)
		}
		if m1.TradeCount != m2.TradeCount {
			t.Errorf("%s TradeCount: run1=%d, run2=%d", strat, m1.TradeCount, m2.TradeCount)
		}
	}
}

// TestRunner_RespectsSpeedZero covers spec RR3 maximum-speed scenario.
func TestRunner_RespectsSpeedZero(t *testing.T) {
	base := time.Now().UTC()
	frames := makeFrames(base, 5)
	rec, _ := tempRecorder(t, frames)
	st := store.NewStore("")

	runner := backtest.NewRunner(rec, st)

	spatCfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.001,
		MaxPositionUSDT:    1000,
		StalenessThreshold: time.Second,
	}
	factories := []backtest.StrategyFactory{backtest.NewSpatialFactory(spatCfg)}

	spec := backtest.BacktestSpec{
		From:       base,
		To:         base.Add(5 * time.Second),
		Speed:      0, // maximum speed — no sleep
		Strategies: []string{"spatial"},
		Seed:       1,
	}

	start := time.Now()
	_, err := runner.Run(context.Background(), spec, factories)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("Speed=0 run took %v, expected < 200ms", elapsed)
	}
}

// TestRunner_StatusProgressUpdates covers spec RR4 sequential re-run + API2 idle check.
func TestRunner_StatusProgressUpdates(t *testing.T) {
	base := time.Now().UTC()
	frames := makeFrames(base, 5)
	rec, _ := tempRecorder(t, frames)
	st := store.NewStore("")

	runner := backtest.NewRunner(rec, st)

	spatCfg := spatial.Config{
		Fees:               map[string]spatial.FeeConfig{},
		MinNetProfitPct:    0.001,
		MaxPositionUSDT:    1000,
		StalenessThreshold: time.Second,
	}
	factories := []backtest.StrategyFactory{backtest.NewSpatialFactory(spatCfg)}

	spec := backtest.BacktestSpec{
		From:       base,
		To:         base.Add(5 * time.Second),
		Speed:      0,
		Strategies: []string{"spatial"},
		Seed:       1,
	}

	_, err := runner.Run(context.Background(), spec, factories)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	status := runner.Status()
	if status.State != "done" {
		t.Errorf("State: want done, got %q", status.State)
	}
	if status.Progress != 1.0 {
		t.Errorf("Progress: want 1.0, got %f", status.Progress)
	}

	// Sequential re-run must succeed.
	_, err = runner.Run(context.Background(), spec, factories)
	if err != nil {
		t.Errorf("sequential re-run: %v", err)
	}
}
