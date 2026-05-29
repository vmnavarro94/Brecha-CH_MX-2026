package risk

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// fixedClock is a Clock that always returns the same time.
type fixedClock struct {
	t time.Time
}

func (c *fixedClock) Now() time.Time { return c.t }
func (c *fixedClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newOpp(netPctF float64, maxVol, buyPrice float64) types.Opportunity {
	return types.Opportunity{
		NetProfitPct: decimal.NewFromFloat(netPctF),
		MaxVolume:    decimal.NewFromFloat(maxVol),
		BuyPrice:     decimal.NewFromFloat(buyPrice),
	}
}

func defaultCfg() Config {
	return Config{
		MinNetProfitPct:  0.0015,
		MaxPositionUSDT:  1000.0,
		LossThreshold:    -0.005,
		ConsecutiveLossN: 5,
		PauseDuration:    5 * time.Minute,
	}
}

// TestNetProfitBelowThreshold verifies that an opportunity below MinNetProfitPct is rejected.
func TestNetProfitBelowThreshold(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	opp := newOpp(0.001, 0.01, 50000.0) // net_pct=0.001 < 0.0015, position=500 < 1000
	if rm.Evaluate(opp) {
		t.Error("Evaluate should return false when NetProfitPct < MinNetProfitPct")
	}
}

// TestNetProfitAtThreshold verifies that an opportunity at exactly MinNetProfitPct is approved.
func TestNetProfitAtThreshold(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	opp := newOpp(0.0015, 0.01, 50000.0) // net_pct=0.0015 == threshold, position=0.01*50000=500 < 1000
	if !rm.Evaluate(opp) {
		t.Error("Evaluate should return true when NetProfitPct == MinNetProfitPct")
	}
}

// TestMaxPositionExceeded verifies that an opportunity exceeding MaxPositionUSDT is rejected.
// Position size = MaxVolume * BuyPrice
func TestMaxPositionExceeded(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	// maxVol=0.1, buyPrice=20000 → position=2000 > MaxPositionUSDT=1000
	opp := newOpp(0.002, 0.1, 20000.0)
	if rm.Evaluate(opp) {
		t.Error("Evaluate should return false when position exceeds MaxPositionUSDT")
	}
}

// TestStateActiveFirstLossStartsWatching verifies that the first loss while Active
// moves state to Watching and increments the loss counter.
func TestStateActiveFirstLossStartsWatching(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	if rm.State() != StateActive {
		t.Fatalf("initial state should be Active, got %v", rm.State())
	}

	rm.RecordTradeResult(-0.006) // below LossThreshold=-0.005

	if rm.State() != StateWatching {
		t.Errorf("after first loss, state should be Watching, got %v", rm.State())
	}
}

// TestWatchingFiveLossesTriggersPaused verifies that 5 consecutive losses in Watching
// transitions to Paused.
func TestWatchingFiveLossesTriggersPaused(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	for i := 0; i < 5; i++ {
		rm.RecordTradeResult(-0.01)
	}

	if rm.State() != StatePaused {
		t.Errorf("after 5 consecutive losses, state should be Paused, got %v", rm.State())
	}
}

// TestWatchingProfitableTradeResetsToActive verifies that a profitable trade while Watching
// resets the counter and returns to Active.
func TestWatchingProfitableTradeResetsToActive(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	// 2 losses to enter Watching.
	rm.RecordTradeResult(-0.01)
	rm.RecordTradeResult(-0.01)

	if rm.State() != StateWatching {
		t.Fatalf("expected Watching after 2 losses, got %v", rm.State())
	}

	// Profitable trade resets.
	rm.RecordTradeResult(0.002) // positive net profit

	if rm.State() != StateActive {
		t.Errorf("profitable trade should reset state to Active, got %v", rm.State())
	}
}

// TestPausedBlocksAllEvaluations verifies Evaluate always returns false while Paused.
func TestPausedBlocksAllEvaluations(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	// Trigger Paused via 5 losses.
	for i := 0; i < 5; i++ {
		rm.RecordTradeResult(-0.01)
	}

	// Great opportunity — but state is Paused.
	opp := newOpp(0.01, 0.01, 50000.0) // position=500 < 1000, pct=0.01 > threshold
	if rm.Evaluate(opp) {
		t.Error("Evaluate should return false while Paused regardless of opportunity quality")
	}
}

// TestPausedExpiryReturnsToActive verifies that after PauseDuration elapses,
// state returns to Active and evaluations succeed.
func TestPausedExpiryReturnsToActive(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	// Trigger Paused.
	for i := 0; i < 5; i++ {
		rm.RecordTradeResult(-0.01)
	}

	if rm.State() != StatePaused {
		t.Fatalf("expected Paused, got %v", rm.State())
	}

	// Advance clock past PauseDuration.
	clk.Advance(6 * time.Minute)

	// Now evaluate a good opportunity — should transition back to Active and return true.
	opp := newOpp(0.002, 0.01, 50000.0) // position=500, pct=0.002 > 0.0015
	if !rm.Evaluate(opp) {
		t.Error("after pause expires, Evaluate should return true for valid opportunity")
	}

	if rm.State() != StateActive {
		t.Errorf("after pause expires, state should be Active, got %v", rm.State())
	}
}

// TestRecordTradeResultTransitions verifies the full state machine sequence.
func TestRecordTradeResultTransitions(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	rm := NewRiskManager(defaultCfg(), clk)

	// Active → (3 losses) → Watching
	for i := 0; i < 3; i++ {
		rm.RecordTradeResult(-0.01)
	}
	if rm.State() != StateWatching {
		t.Fatalf("expected Watching after 3 losses, got %v", rm.State())
	}

	// Win → back to Active
	rm.RecordTradeResult(0.005)
	if rm.State() != StateActive {
		t.Fatalf("expected Active after win, got %v", rm.State())
	}

	// Active → 5 losses → Paused
	for i := 0; i < 5; i++ {
		rm.RecordTradeResult(-0.01)
	}
	if rm.State() != StatePaused {
		t.Fatalf("expected Paused after 5 more losses, got %v", rm.State())
	}
}
