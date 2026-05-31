package engine

import (
	"container/heap"
	"sync/atomic"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/metrics"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// StrategyIface is the subset of strategy.Strategy used by the engine.
// Using a local interface keeps the engine package from importing internal/strategy,
// avoiding a potential import cycle and allowing test doubles in engine_test.go.
type StrategyIface interface {
	Name() string
	Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
}

// Config holds engine-level configuration. Detection and fee parameters live on
// the individual strategies, not here.
type Config struct {
	OpportunityTTL time.Duration
}

// Engine is a thin coordinator: it fans out ProcessUpdate to each registered strategy,
// collects returned opportunities into a max-heap, and serves them via DequeueTop.
// It also tracks processing latency and call count.
type Engine struct {
	strategies []StrategyIface
	snapshotFn func() map[string]types.PriceUpdate
	clock      types.Clock
	cfg        Config
	pq         oppHeap
	latency    *metrics.LatencyTracker
	processed  atomic.Uint64
}

// NewEngine creates a new Engine. snapshotFn returns the current BBO for all exchanges.
// strategies is the slice of detection algorithms to fan out to; may be nil or empty.
func NewEngine(
	snapshotFn func() map[string]types.PriceUpdate,
	clock types.Clock,
	cfg Config,
	strategies []StrategyIface,
) *Engine {
	h := make(oppHeap, 0, 64)
	heap.Init(&h)
	return &Engine{
		strategies: strategies,
		snapshotFn: snapshotFn,
		clock:      clock,
		cfg:        cfg,
		pq:         h,
		latency:    &metrics.LatencyTracker{},
	}
}

// SetClock replaces the engine's clock — used in tests to advance time after detection.
func (e *Engine) SetClock(clk types.Clock) {
	e.clock = clk
}

// ProcessUpdate fans out the incoming price update to all registered strategies and
// enqueues every returned opportunity into the max-heap.
// Complexity: O(S * N) where S = number of strategies, N = exchanges in snapshot.
func (e *Engine) ProcessUpdate(u types.PriceUpdate) {
	start := time.Now()

	snapshot := e.snapshotFn()
	now := e.clock.Now()

	for _, s := range e.strategies {
		for _, opp := range s.Detect(u, snapshot, now) {
			heap.Push(&e.pq, &scoredOpportunity{opp: opp})
		}
	}

	e.latency.Record(time.Since(start))
	e.processed.Add(1)
}

// ProcessedCount returns the total number of ProcessUpdate calls since startup.
// Safe for concurrent reads.
func (e *Engine) ProcessedCount() uint64 {
	return e.processed.Load()
}

// DequeueTop returns the highest-score non-expired opportunity, or false if none exists.
// Expired opportunities (older than OpportunityTTL) are silently discarded.
func (e *Engine) DequeueTop() (*types.Opportunity, bool) {
	ttl := e.cfg.OpportunityTTL
	now := e.clock.Now()
	for e.pq.Len() > 0 {
		item := heap.Pop(&e.pq).(*scoredOpportunity)
		age := now.Sub(item.opp.DetectedAt)
		if age > ttl {
			// Expired — discard and try next.
			continue
		}
		opp := item.opp
		return &opp, true
	}
	return nil, false
}

// LatencyStats returns p50 and p99 in microseconds and the sample count.
// Returns (0, 0, n) when fewer than 10 samples have been recorded (cold-start guard).
func (e *Engine) LatencyStats() (p50us, p99us float64, samples int) {
	p50, p99, n := e.latency.Stats()
	if n < 10 {
		return 0, 0, n
	}
	return float64(p50.Nanoseconds()) / 1000.0, float64(p99.Nanoseconds()) / 1000.0, n
}
