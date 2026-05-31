package backtest_test

import (
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// TestReplaySnapshot_UpdateAndRead verifies write-then-read and concurrent reads.
func TestReplaySnapshot_UpdateAndRead(t *testing.T) {
	snap := backtest.NewReplaySnapshot()

	p := types.PriceUpdate{
		Exchange:   "binance",
		Bid:        decimal.NewFromFloat(50000),
		Ask:        decimal.NewFromFloat(50001),
		ReceivedAt: time.Now(),
	}
	snap.Update(p)

	// Single read.
	m := snap.Snapshot()
	got, ok := m["binance"]
	if !ok {
		t.Fatal("binance not found in snapshot")
	}
	if !got.Bid.Equal(p.Bid) {
		t.Errorf("Bid: want %s, got %s", p.Bid, got.Bid)
	}

	// Concurrent reads under -race.
	var wg sync.WaitGroup
	const readers = 50
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m2 := snap.Snapshot()
			_ = m2["binance"]
		}()
	}
	wg.Wait()

	// Update from multiple goroutines concurrently.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		ex := "exchange_" + string(rune('a'+i%26))
		go func(exchange string) {
			defer wg.Done()
			snap.Update(types.PriceUpdate{
				Exchange:   exchange,
				Bid:        decimal.NewFromFloat(1.0),
				Ask:        decimal.NewFromFloat(2.0),
				ReceivedAt: time.Now(),
			})
		}(ex)
	}
	wg.Wait()
}
