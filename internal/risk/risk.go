package risk

import (
	"sync"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// State represents the current circuit-breaker state of the RiskManager.
type State int

const (
	// StateActive means the risk manager is operating normally.
	StateActive State = iota
	// StateWatching means consecutive losses have started accumulating.
	StateWatching
	// StatePaused means the circuit breaker has tripped; all trading is blocked.
	StatePaused
)

func (s State) String() string {
	switch s {
	case StateActive:
		return "active"
	case StateWatching:
		return "watching"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}

// Config holds the risk manager configuration parameters.
type Config struct {
	// MinNetProfitPct is the minimum acceptable net profit percentage per trade.
	MinNetProfitPct float64
	// MaxPositionUSDT is the maximum allowed USDT position size (MaxVolume * BuyPrice).
	MaxPositionUSDT float64
	// LossThreshold is the net profit below which a trade is considered a loss.
	LossThreshold float64
	// ConsecutiveLossN is the number of consecutive losses before transitioning to Paused.
	ConsecutiveLossN int
	// PauseDuration is how long the circuit breaker stays in Paused state.
	PauseDuration time.Duration
}

// RiskManager implements a 3-state circuit breaker for trade risk control.
type RiskManager struct {
	mu         sync.Mutex
	cfg        Config
	clock      types.Clock
	state      State
	lossCount  int
	pauseUntil time.Time
}

// NewRiskManager creates a RiskManager starting in StateActive.
func NewRiskManager(cfg Config, clock types.Clock) *RiskManager {
	return &RiskManager{
		cfg:   cfg,
		clock: clock,
		state: StateActive,
	}
}

// State returns the current circuit-breaker state.
func (r *RiskManager) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkPauseExpiry()
	return r.state
}

// Evaluate returns true when the opportunity passes all risk checks and the circuit
// breaker is not tripped. Returns false otherwise.
func (r *RiskManager) Evaluate(opp types.Opportunity) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkPauseExpiry()

	if r.state == StatePaused {
		return false
	}

	// Check minimum net profit percentage.
	netPct, _ := opp.NetProfitPct.Float64()
	if netPct < r.cfg.MinNetProfitPct {
		return false
	}

	// Check maximum position size.
	maxVol, _ := opp.MaxVolume.Float64()
	buyPrice, _ := opp.BuyPrice.Float64()
	position := maxVol * buyPrice
	if position > r.cfg.MaxPositionUSDT {
		return false
	}

	return true
}

// RecordTradeResult updates the circuit-breaker state machine based on the trade outcome.
// netPct is the realised net profit as a fraction of the buy price (negative = loss).
func (r *RiskManager) RecordTradeResult(netPct float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkPauseExpiry()

	if r.state == StatePaused {
		return
	}

	if netPct < r.cfg.LossThreshold {
		// Loss trade.
		r.lossCount++
		if r.state == StateActive {
			r.state = StateWatching
		}
		if r.lossCount >= r.cfg.ConsecutiveLossN {
			r.state = StatePaused
			r.pauseUntil = r.clock.Now().Add(r.cfg.PauseDuration)
		}
	} else {
		// Profitable or break-even trade — reset.
		r.lossCount = 0
		r.state = StateActive
	}
}

// SetMinNetProfitPct updates the minimum net profit threshold.
func (r *RiskManager) SetMinNetProfitPct(v float64) {
	r.mu.Lock()
	r.cfg.MinNetProfitPct = v
	r.mu.Unlock()
}

// SetConsecutiveLossN updates the number of consecutive losses before circuit-breaker trips.
func (r *RiskManager) SetConsecutiveLossN(n int) {
	r.mu.Lock()
	r.cfg.ConsecutiveLossN = n
	r.mu.Unlock()
}

// SetLossThreshold updates the net profit threshold below which a trade is a loss.
func (r *RiskManager) SetLossThreshold(v float64) {
	r.mu.Lock()
	r.cfg.LossThreshold = v
	r.mu.Unlock()
}

// checkPauseExpiry transitions from Paused to Active when PauseDuration has elapsed.
// Caller must hold r.mu.
func (r *RiskManager) checkPauseExpiry() {
	if r.state == StatePaused && !r.clock.Now().Before(r.pauseUntil) {
		r.state = StateActive
		r.lossCount = 0
	}
}
