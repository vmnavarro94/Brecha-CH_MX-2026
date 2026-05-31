package spatial

import (
	"testing"
)

// TestRecentTradeRing_Push verifies C1: capacity=5, push 5 distinct entries, all present.
func TestRecentTradeRing_Push(t *testing.T) {
	r := newRing(5)
	entries := []recentTradeEntry{
		{"a", "b"}, {"c", "d"}, {"e", "f"}, {"g", "h"}, {"i", "j"},
	}
	for _, e := range entries {
		r.Push(e)
	}
	if r.Len() != 5 {
		t.Errorf("Len: got %d, want 5", r.Len())
	}
}

// TestRecentTradeRing_Evict verifies C1: push 6th entry into cap=5 ring; len==5 and oldest gone.
func TestRecentTradeRing_Evict(t *testing.T) {
	r := newRing(5)
	for i := 0; i < 5; i++ {
		r.Push(recentTradeEntry{"old", "old"})
	}
	r.Push(recentTradeEntry{"new", "new"})

	if r.Len() != 5 {
		t.Errorf("Len after eviction: got %d, want 5", r.Len())
	}
	// The oldest "old" entry at buf[0] has been overwritten by "new".
	// buf[0] is now "new"; the 4 remaining "old" entries are at buf[1..4].
	// CountSameExchange("new", "new") should return 1 (only the new entry).
	// CountSameExchange("old", "old") should return 4 (remaining old entries).
	newCount := r.CountSameExchange("new", "new")
	if newCount != 1 {
		t.Errorf("CountSameExchange(new,new) after eviction: got %d, want 1", newCount)
	}
}

// TestRecentTradeRing_CountSameExchange_OR verifies C2 OR semantics with 4 branches + no-match case.
func TestRecentTradeRing_CountSameExchange_OR(t *testing.T) {
	r := newRing(10)

	// Branch 1: stored.BuyEx == cand.BuyEx
	r.Push(recentTradeEntry{BuyEx: "binance", SellEx: "other1"})
	// Branch 2: stored.BuyEx == cand.SellEx
	r.Push(recentTradeEntry{BuyEx: "kraken", SellEx: "other2"})
	// Branch 3: stored.SellEx == cand.BuyEx
	r.Push(recentTradeEntry{BuyEx: "other3", SellEx: "binance"})
	// Branch 4: stored.SellEx == cand.SellEx
	r.Push(recentTradeEntry{BuyEx: "other4", SellEx: "kraken"})
	// No-match: neither leg matches
	r.Push(recentTradeEntry{BuyEx: "mexc", SellEx: "okx"})

	// candidate: buyEx="binance", sellEx="kraken"
	// Branch1: stored.BuyEx=="binance" matches cand.BuyEx → yes (entry 0)
	// Branch2: stored.BuyEx=="kraken" matches cand.SellEx → yes (entry 1)
	// Branch3: stored.SellEx=="binance" matches cand.BuyEx → yes (entry 2)
	// Branch4: stored.SellEx=="kraken" matches cand.SellEx → yes (entry 3)
	// No-match: "mexc","okx" → no (entry 4)
	count := r.CountSameExchange("binance", "kraken")
	if count != 4 {
		t.Errorf("CountSameExchange (OR, 4 branches): got %d, want 4", count)
	}
}

// TestRecentTradeRing_Empty verifies edge case: CountSameExchange on empty ring returns 0.
func TestRecentTradeRing_Empty(t *testing.T) {
	r := newRing(50)
	got := r.CountSameExchange("binance", "kraken")
	if got != 0 {
		t.Errorf("CountSameExchange on empty ring: got %d, want 0", got)
	}
}
