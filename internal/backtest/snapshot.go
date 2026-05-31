package backtest

import (
	"sync"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// ReplaySnapshot holds the latest price for each exchange during a replay run.
// It is used as the snapshot function passed to engine.NewEngine.
type ReplaySnapshot struct {
	mu     sync.RWMutex
	prices map[string]types.PriceUpdate
}

// NewReplaySnapshot returns an empty ReplaySnapshot.
func NewReplaySnapshot() *ReplaySnapshot {
	return &ReplaySnapshot{
		prices: make(map[string]types.PriceUpdate),
	}
}

// Update stores or overwrites the price for p.Exchange.
func (s *ReplaySnapshot) Update(p types.PriceUpdate) {
	s.mu.Lock()
	s.prices[p.Exchange] = p
	s.mu.Unlock()
}

// Snapshot returns a shallow copy of the current price map.
// Satisfies the snapshotFn signature required by engine.NewEngine.
func (s *ReplaySnapshot) Snapshot() map[string]types.PriceUpdate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]types.PriceUpdate, len(s.prices))
	for k, v := range s.prices {
		out[k] = v
	}
	return out
}
