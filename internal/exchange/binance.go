package exchange

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/metrics"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type Binance struct {
	wsURL   string
	ch      chan types.PriceUpdate
	bookCh  chan BookUpdate
	logger  *slog.Logger
	tracker *metrics.LatencyTracker
}

func NewBinance(wsURL string, tracker *metrics.LatencyTracker) *Binance {
	// Combined stream: bookTicker (BBO) + depth20@100ms (L2 snapshot every 100ms).
	combined := wsURL + "/stream?streams=btcusdt@bookTicker/btcusdt@depth20@100ms"
	return &Binance{
		wsURL:   combined,
		ch:      make(chan types.PriceUpdate, 512),
		bookCh:  make(chan BookUpdate, 256),
		logger:  slog.Default().With("exchange", "binance"),
		tracker: tracker,
	}
}

func (b *Binance) Name() string                       { return "binance" }
func (b *Binance) Updates() <-chan types.PriceUpdate  { return b.ch }
func (b *Binance) BookUpdates() <-chan BookUpdate      { return b.bookCh }

func (b *Binance) Connect(ctx context.Context) error {
	go b.runWithReconnect(ctx)
	return nil
}

func (b *Binance) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := b.run(ctx); err != nil {
			b.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (b *Binance) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, b.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	b.logger.Info("connected")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		t0 := time.Now()

		// Peek at the "stream" field to dispatch to the right parser.
		var envelope struct {
			Stream string `json:"stream"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}

		switch {
		case envelope.Stream == "btcusdt@bookTicker":
			pu, ok := b.parseMessage(msg)
			if !ok {
				continue
			}
			if b.tracker != nil {
				b.tracker.Record(time.Since(t0))
			}
			select {
			case b.ch <- pu:
			default:
			}
		case envelope.Stream == "btcusdt@depth20@100ms":
			bu, ok := b.parseDepth20(msg)
			if !ok {
				continue
			}
			select {
			case b.bookCh <- bu:
			default:
			}
		}
	}
}

// parseMessage decodes a combined-stream bookTicker wrapped frame into a PriceUpdate.
// Frame format: {"stream":"btcusdt@bookTicker","data":{"b":"...","B":"...","a":"...","A":"..."}}
func (b *Binance) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var envelope struct {
		Data struct {
			Bid     string `json:"b"`
			BidSize string `json:"B"`
			Ask     string `json:"a"`
			AskSize string `json:"A"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(envelope.Data.Bid)
	ask, e2 := decimal.NewFromString(envelope.Data.Ask)
	bidSize, e3 := decimal.NewFromString(envelope.Data.BidSize)
	askSize, e4 := decimal.NewFromString(envelope.Data.AskSize)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	if bid.IsZero() || ask.IsZero() {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "binance",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}

// parseDepth20 decodes a combined-stream depth20@100ms wrapped frame into a BookUpdate.
// Frame format: {"stream":"btcusdt@depth20@100ms","data":{"lastUpdateId":N,"bids":[["price","qty"]...],"asks":[...]}}
func (b *Binance) parseDepth20(msg []byte) (BookUpdate, bool) {
	var envelope struct {
		Data struct {
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return BookUpdate{}, false
	}

	bids, ok := parseLevels(envelope.Data.Bids)
	if !ok {
		return BookUpdate{}, false
	}
	asks, ok := parseLevels(envelope.Data.Asks)
	if !ok {
		return BookUpdate{}, false
	}

	return BookUpdate{
		Exchange:   "binance",
		Bids:       bids,
		Asks:       asks,
		ReceivedAt: time.Now(),
	}, true
}

// parseLevels converts a [][]string price/qty matrix into []types.OrderBookLevel.
func parseLevels(raw [][]string) ([]types.OrderBookLevel, bool) {
	levels := make([]types.OrderBookLevel, 0, len(raw))
	for _, entry := range raw {
		if len(entry) < 2 {
			return nil, false
		}
		price, err := decimal.NewFromString(entry[0])
		if err != nil {
			return nil, false
		}
		qty, err := decimal.NewFromString(entry[1])
		if err != nil {
			return nil, false
		}
		levels = append(levels, types.OrderBookLevel{Price: price, Qty: qty})
	}
	return levels, true
}
