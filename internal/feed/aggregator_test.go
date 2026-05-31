package feed

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/exchange"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// fakeConnector implements exchange.Connector for tests.
type fakeConnector struct {
	name string
	ch   chan types.PriceUpdate
}

func newFakeConnector(name string) *fakeConnector {
	return &fakeConnector{name: name, ch: make(chan types.PriceUpdate, 16)}
}

func (f *fakeConnector) Name() string                      { return f.name }
func (f *fakeConnector) Updates() <-chan types.PriceUpdate { return f.ch }
func (f *fakeConnector) Connect(ctx context.Context) error { return nil }

// fakeBookConnector implements exchange.Connector + exchange.BookConnector for tests.
type fakeBookConnector struct {
	fakeConnector
	bookCh chan exchange.BookUpdate
}

func newFakeBookConnector(name string) *fakeBookConnector {
	return &fakeBookConnector{
		fakeConnector: fakeConnector{name: name, ch: make(chan types.PriceUpdate, 16)},
		bookCh:        make(chan exchange.BookUpdate, 16),
	}
}

func (f *fakeBookConnector) BookUpdates() <-chan exchange.BookUpdate { return f.bookCh }

func mkUpdate(ex string, bid, ask float64) types.PriceUpdate {
	return types.PriceUpdate{
		Exchange:   ex,
		Bid:        decimal.NewFromFloat(bid),
		Ask:        decimal.NewFromFloat(ask),
		ReceivedAt: time.Now(),
	}
}

func TestAggregator_StartConnects(t *testing.T) {
	fc := newFakeConnector("binance")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fc})
	if err := agg.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestAggregator_DrainAndSnapshot(t *testing.T) {
	fc := newFakeConnector("binance")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fc})
	_ = agg.Start(ctx)

	fc.ch <- mkUpdate("binance", 73000, 73001)

	// Read it off the fan-out.
	select {
	case got := <-agg.Updates():
		if got.Exchange != "binance" || got.Bid.String() != "73000" {
			t.Errorf("unexpected update from Updates(): %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Updates() did not produce the price within 100ms")
	}

	// Snapshot reflects the latest.
	snap := agg.Snapshot()
	if len(snap) != 1 || snap["binance"].Ask.String() != "73001" {
		t.Errorf("Snapshot: got %+v", snap)
	}
}

func TestAggregator_LatestReturnsCopy(t *testing.T) {
	fc := newFakeConnector("kraken")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fc})
	_ = agg.Start(ctx)

	fc.ch <- mkUpdate("kraken", 73010, 73011)
	// Drain so the goroutine processes.
	<-agg.Updates()

	got := agg.Latest("kraken")
	if got == nil {
		t.Fatal("Latest returned nil for known exchange")
	}
	if got.Ask.String() != "73011" {
		t.Errorf("Latest.Ask: got %s, want 73011", got.Ask.String())
	}
	// Mutating the returned copy must not affect the aggregator's stored value.
	got.Ask = decimal.NewFromFloat(0.0)
	got2 := agg.Latest("kraken")
	if got2.Ask.String() != "73011" {
		t.Errorf("Latest aliasing: aggregator state mutated by caller")
	}
}

func TestAggregator_LatestUnknown(t *testing.T) {
	agg := NewAggregator([]exchange.Connector{})
	if agg.Latest("nonexistent") != nil {
		t.Error("Latest should return nil for unknown exchange")
	}
}

func TestAggregator_MultipleConnectors(t *testing.T) {
	a := newFakeConnector("binance")
	b := newFakeConnector("kraken")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{a, b})
	_ = agg.Start(ctx)

	a.ch <- mkUpdate("binance", 73000, 73001)
	b.ch <- mkUpdate("kraken", 73020, 73021)

	// Both must end up in Snapshot eventually.
	var wg sync.WaitGroup
	wg.Add(2)
	got := make(map[string]bool)
	var mu sync.Mutex
	go func() {
		for i := 0; i < 2; i++ {
			u := <-agg.Updates()
			mu.Lock()
			got[u.Exchange] = true
			mu.Unlock()
			wg.Done()
		}
	}()
	wg.Wait()

	if !got["binance"] || !got["kraken"] {
		t.Errorf("expected both binance and kraken on fan-out, got %v", got)
	}

	snap := agg.Snapshot()
	if len(snap) != 2 {
		t.Errorf("Snapshot size: got %d, want 2", len(snap))
	}
}

func TestAggregator_FanOutNonBlocking(t *testing.T) {
	// Verify drain does not block when the fan-out channel is full.
	fc := newFakeConnector("binance")
	agg := NewAggregator([]exchange.Connector{fc})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = agg.Start(ctx)

	// Send more updates than the fan-out buffer (512). Drain MUST not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 600; i++ {
			fc.ch <- mkUpdate("binance", float64(i), float64(i+1))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("producer blocked — drain is not non-blocking")
	}
}

// TestAggregator_Book_WithL2Connector verifies that Book() returns non-empty Bids and
// Asks after a BookUpdate is received from a BookConnector. Spec: D2.
func TestAggregator_Book_WithL2Connector(t *testing.T) {
	fbc := newFakeBookConnector("binance")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fbc})
	_ = agg.Start(ctx)

	// Emit a BookUpdate.
	fbc.bookCh <- exchange.BookUpdate{
		Exchange: "binance",
		Bids: []types.OrderBookLevel{
			{Price: decimal.NewFromFloat(73000), Qty: decimal.NewFromFloat(1.0)},
		},
		Asks: []types.OrderBookLevel{
			{Price: decimal.NewFromFloat(73001), Qty: decimal.NewFromFloat(0.5)},
		},
		ReceivedAt: time.Now(),
	}

	// Poll until the aggregator processes it (max 100ms).
	var ob types.OrderBook
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		ob = agg.Book("binance")
		if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if len(ob.Bids) == 0 {
		t.Error("Book(binance).Bids should be non-empty after BookUpdate")
	}
	if len(ob.Asks) == 0 {
		t.Error("Book(binance).Asks should be non-empty after BookUpdate")
	}
	if ob.Bids[0].Price.String() != "73000" {
		t.Errorf("Bids[0].Price: got %s, want 73000", ob.Bids[0].Price.String())
	}
}

// TestAggregator_Book_NoL2Connector verifies that Book() returns an empty OrderBook for
// a connector that does not implement BookConnector. Spec: D2.
func TestAggregator_Book_NoL2Connector(t *testing.T) {
	fc := newFakeConnector("kraken")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fc})
	_ = agg.Start(ctx)

	ob := agg.Book("kraken")
	if len(ob.Bids) != 0 || len(ob.Asks) != 0 {
		t.Errorf("Book(kraken) should be empty for non-BookConnector, got %+v", ob)
	}
}

// TestAggregator_HasL2 verifies HasL2 returns true for BookConnector and false otherwise.
func TestAggregator_HasL2(t *testing.T) {
	fbc := newFakeBookConnector("binance")
	fc := newFakeConnector("kraken")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg := NewAggregator([]exchange.Connector{fbc, fc})
	_ = agg.Start(ctx)

	if !agg.HasL2("binance") {
		t.Error("HasL2(binance) should be true for BookConnector")
	}
	if agg.HasL2("kraken") {
		t.Error("HasL2(kraken) should be false for non-BookConnector")
	}
}
