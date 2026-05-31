package backtest_test

import (
	"sync"
	"testing"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
)

// TestReplayClock_RaceSafeSetAndNow runs concurrent Set and Now calls under
// the race detector to verify no data race occurs.
func TestReplayClock_RaceSafeSetAndNow(t *testing.T) {
	clk := backtest.NewReplayClock()

	target := time.Now().Add(42 * time.Hour)

	var wg sync.WaitGroup
	const goroutines = 50

	// Writers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		ts := target.Add(time.Duration(i) * time.Millisecond)
		go func(t_ time.Time) {
			defer wg.Done()
			clk.Set(t_)
		}(ts)
	}

	// Readers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = clk.Now()
		}()
	}

	wg.Wait()

	// After all sets, Now() must return what was last Set (some valid ts).
	got := clk.Now()
	if got.IsZero() {
		t.Error("Now() returned zero time after concurrent Sets")
	}
}
