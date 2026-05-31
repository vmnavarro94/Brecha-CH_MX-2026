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

type Bybit struct {
	wsURL   string
	ch      chan types.PriceUpdate
	bookCh  chan BookUpdate
	logger  *slog.Logger
	tracker *metrics.LatencyTracker
}

func NewBybit(wsURL string, tracker *metrics.LatencyTracker) *Bybit {
	return &Bybit{
		wsURL:   wsURL,
		ch:      make(chan types.PriceUpdate, 512),
		bookCh:  make(chan BookUpdate, 256),
		logger:  slog.Default().With("exchange", "bybit"),
		tracker: tracker,
	}
}

func (b *Bybit) Name() string                       { return "bybit" }
func (b *Bybit) Updates() <-chan types.PriceUpdate  { return b.ch }
func (b *Bybit) BookUpdates() <-chan BookUpdate      { return b.bookCh }

func (b *Bybit) Connect(ctx context.Context) error {
	go b.runWithReconnect(ctx)
	return nil
}

func (b *Bybit) runWithReconnect(ctx context.Context) {
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

func (b *Bybit) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, b.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Subscribe to orderbook.50 for L2 snapshots (50 levels per side).
	sub := map[string]any{
		"op":   "subscribe",
		"args": []string{"orderbook.50.BTCUSDT"},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	// Bybit requires a ping every 20s to keep the connection alive.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteJSON(map[string]string{"op": "ping"}) //nolint:errcheck
			}
		}
	}()

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

		// Peek at the type field to dispatch.
		var peek struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(msg, &peek)

		switch peek.Type {
		case "snapshot":
			pu, ok := b.parseMessage(msg)
			if ok {
				if b.tracker != nil {
					b.tracker.Record(time.Since(t0))
				}
				select {
				case b.ch <- pu:
				default:
				}
			}
			// Also emit a BookUpdate for L2 consumers.
			if bu, ok := b.parseSnapshot(msg); ok {
				select {
				case b.bookCh <- bu:
				default:
				}
			}
		case "delta":
			// Delta frames are ignored per design ADR-6.
			continue
		}
	}
}

// parseMessage decodes a Bybit orderbook.50.BTCUSDT snapshot frame into a PriceUpdate (BBO).
// Extracts only the best bid (bids[0]) and best ask (asks[0]).
func (b *Bybit) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var envelope struct {
		Topic string `json:"topic"`
		Type  string `json:"type"`
		Data  struct {
			Bids [][]string `json:"b"`
			Asks [][]string `json:"a"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return types.PriceUpdate{}, false
	}
	if envelope.Topic != "orderbook.50.BTCUSDT" {
		return types.PriceUpdate{}, false
	}
	if len(envelope.Data.Bids) == 0 || len(envelope.Data.Asks) == 0 {
		return types.PriceUpdate{}, false
	}
	bidEntry := envelope.Data.Bids[0]
	askEntry := envelope.Data.Asks[0]
	if len(bidEntry) < 2 || len(askEntry) < 2 {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(bidEntry[0])
	bidSize, e2 := decimal.NewFromString(bidEntry[1])
	ask, e3 := decimal.NewFromString(askEntry[0])
	askSize, e4 := decimal.NewFromString(askEntry[1])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "bybit",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}

// parseSnapshot decodes a Bybit orderbook.50 snapshot frame into a full BookUpdate.
// Returns false for delta frames (only snapshots produce L2 updates).
func (b *Bybit) parseSnapshot(msg []byte) (BookUpdate, bool) {
	var envelope struct {
		Type string `json:"type"`
		Data struct {
			Bids [][]string `json:"b"`
			Asks [][]string `json:"a"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return BookUpdate{}, false
	}
	// Only snapshot frames produce L2 updates; deltas are no-ops per ADR-6.
	if envelope.Type != "snapshot" {
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
		Exchange:   "bybit",
		Bids:       bids,
		Asks:       asks,
		ReceivedAt: time.Now(),
	}, true
}
