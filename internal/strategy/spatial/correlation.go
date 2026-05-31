package spatial

import "sync"

// recentTradeEntry stores the buy/sell exchange names for a past trade.
type recentTradeEntry struct {
	BuyEx  string
	SellEx string
}

// recentTradeRing is a fixed-capacity ring buffer of recent trade entries.
// It evicts the oldest entry when full. Thread-safe via Mutex.
type recentTradeRing struct {
	mu  sync.Mutex
	buf []recentTradeEntry
	cap int
	idx int // next write position
	len int // current fill count
}

// newRing creates a new recentTradeRing with the given capacity.
func newRing(capacity int) *recentTradeRing {
	return &recentTradeRing{
		buf: make([]recentTradeEntry, capacity),
		cap: capacity,
	}
}

// Push adds an entry to the ring, evicting the oldest when full.
func (r *recentTradeRing) Push(entry recentTradeEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.idx] = entry
	r.idx = (r.idx + 1) % r.cap
	if r.len < r.cap {
		r.len++
	}
}

// CountSameExchange counts entries where either leg matches either leg of the candidate (OR semantics).
// cand.BuyEx matches stored.BuyEx OR stored.SellEx, and
// cand.SellEx matches stored.BuyEx OR stored.SellEx.
func (r *recentTradeRing) CountSameExchange(buyEx, sellEx string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for i := 0; i < r.len; i++ {
		e := r.buf[i]
		if e.BuyEx == buyEx || e.BuyEx == sellEx || e.SellEx == buyEx || e.SellEx == sellEx {
			count++
		}
	}
	return count
}

// Len returns the current number of entries in the ring.
func (r *recentTradeRing) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.len
}
