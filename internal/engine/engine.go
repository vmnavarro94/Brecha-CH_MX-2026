package engine

import (
	"container/heap"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/model"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// FeeConfig holds trading costs for a single exchange.
type FeeConfig struct {
	TakerFee      float64
	SlippageFactor float64
}

// Config holds the engine configuration parameters.
type Config struct {
	Fees               map[string]FeeConfig
	MinNetProfitPct    float64
	OpportunityTTL     time.Duration
	StalenessThreshold time.Duration
}

// scoredOpportunity wraps an Opportunity with its heap index.
type scoredOpportunity struct {
	opp   types.Opportunity
	index int
}

// oppHeap implements container/heap as a max-heap ordered by Score descending.
type oppHeap []*scoredOpportunity

func (h oppHeap) Len() int           { return len(h) }
func (h oppHeap) Less(i, j int) bool {
	// Max-heap: higher score = higher priority.
	return h[i].opp.Score.GreaterThan(h[j].opp.Score)
}
func (h oppHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *oppHeap) Push(x any) {
	item := x.(*scoredOpportunity)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *oppHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// Engine detects cross-exchange arbitrage opportunities and maintains a priority queue.
type Engine struct {
	snapshotFn func() map[string]types.PriceUpdate
	models     map[string]*model.SpreadModel // pair key "buyEx-sellEx" -> model
	clock      types.Clock
	cfg        Config
	pq         oppHeap
}

// NewEngine creates a new Engine. snapshotFn returns the current BBO for all exchanges.
// models is optional (may be nil); if nil or the pair key is absent, fallback scoring is used.
func NewEngine(
	snapshotFn func() map[string]types.PriceUpdate,
	models map[string]*model.SpreadModel,
	clock types.Clock,
	cfg Config,
) *Engine {
	h := make(oppHeap, 0, 64)
	heap.Init(&h)
	return &Engine{
		snapshotFn: snapshotFn,
		models:     models,
		clock:      clock,
		cfg:        cfg,
		pq:         h,
	}
}

// SetClock replaces the engine's clock — used in tests to advance time after detection.
func (e *Engine) SetClock(clk types.Clock) {
	e.clock = clk
}

// ProcessUpdate receives a new price update for one exchange and compares it against
// all other exchanges in the current snapshot to detect arbitrage opportunities.
// Complexity: O(N) where N is the number of exchanges in the snapshot.
func (e *Engine) ProcessUpdate(update types.PriceUpdate) {
	snapshot := e.snapshotFn()
	now := e.clock.Now()

	for sellEx, sellPrice := range snapshot {
		if sellEx == update.Exchange {
			continue
		}
		// Skip stale counterparty.
		age := now.Sub(sellPrice.ReceivedAt)
		if age > e.cfg.StalenessThreshold {
			continue
		}

		buyAsk, _ := update.Ask.Float64()
		sellBid, _ := sellPrice.Bid.Float64()

		gross := sellBid - buyAsk
		if gross <= 0 {
			continue
		}

		buyFee := e.feeFor(update.Exchange)
		sellFee := e.feeFor(sellEx)

		costBuyFee := buyAsk * buyFee.TakerFee
		costSellFee := sellBid * sellFee.TakerFee
		costSlippage := buyAsk * buyFee.SlippageFactor
		netProfit := gross - costBuyFee - costSellFee - costSlippage

		if netProfit <= 0 {
			continue
		}

		netPct := netProfit / buyAsk
		if netPct < e.cfg.MinNetProfitPct {
			continue
		}

		zScore, score := e.computeScore(update.Exchange, sellEx, netPct)

		opp := types.Opportunity{
			ID:           uuid.New().String(),
			BuyExchange:  update.Exchange,
			SellExchange: sellEx,
			BuyPrice:     update.Ask,
			SellPrice:    sellPrice.Bid,
			NetProfit:    decimal.NewFromFloat(netProfit),
			NetProfitPct: decimal.NewFromFloat(netPct),
			ZScore:       decimal.NewFromFloat(zScore),
			Score:        decimal.NewFromFloat(score),
			MaxVolume:    decimal.NewFromFloat(1.0),
			DetectedAt:   now,
			Status:       types.StatusDetected,
		}

		heap.Push(&e.pq, &scoredOpportunity{opp: opp})
	}
}

// DequeueTop returns the highest-score non-expired opportunity, or false if none exists.
// Expired opportunities (older than OpportunityTTL) are silently discarded.
func (e *Engine) DequeueTop() (*types.Opportunity, bool) {
	now := e.clock.Now()
	for e.pq.Len() > 0 {
		item := heap.Pop(&e.pq).(*scoredOpportunity)
		age := now.Sub(item.opp.DetectedAt)
		if age > e.cfg.OpportunityTTL {
			// Expired — discard and try next.
			continue
		}
		opp := item.opp
		return &opp, true
	}
	return nil, false
}

// pairKey returns the canonical key for a buy/sell exchange pair.
func pairKey(buyEx, sellEx string) string {
	return fmt.Sprintf("%s-%s", buyEx, sellEx)
}

// feeFor returns the FeeConfig for the given exchange, with safe defaults if absent.
func (e *Engine) feeFor(exchange string) FeeConfig {
	if fee, ok := e.cfg.Fees[exchange]; ok {
		return fee
	}
	return FeeConfig{TakerFee: 0.001, SlippageFactor: 0.0002}
}

// computeScore returns (zScore, score).
// When the spread model for the pair is ready: score = net_pct*0.6 + sigmoid(z)*0.4
// Fallback (model absent or not ready): score = net_pct, zScore = 0
func (e *Engine) computeScore(buyEx, sellEx string, netPct float64) (float64, float64) {
	if e.models == nil {
		return 0, netPct
	}
	key := pairKey(buyEx, sellEx)
	sm, ok := e.models[key]
	if !ok || !sm.IsReady() {
		return 0, netPct
	}
	z := sm.ZScore(netPct)
	score := netPct*0.6 + sigmoid(z)*0.4
	return z, score
}

// sigmoid maps x to (0, 1). Used to normalize z-score contribution in scoring.
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
