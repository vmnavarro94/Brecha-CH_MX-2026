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

type OKX struct {
	wsURL   string
	ch      chan types.PriceUpdate
	bookCh  chan BookUpdate
	logger  *slog.Logger
	tracker *metrics.LatencyTracker
}

func NewOKX(wsURL string, tracker *metrics.LatencyTracker) *OKX {
	return &OKX{
		wsURL:   wsURL,
		ch:      make(chan types.PriceUpdate, 512),
		bookCh:  make(chan BookUpdate, 256),
		logger:  slog.Default().With("exchange", "okx"),
		tracker: tracker,
	}
}

func (o *OKX) Name() string                      { return "okx" }
func (o *OKX) Updates() <-chan types.PriceUpdate { return o.ch }
func (o *OKX) BookUpdates() <-chan BookUpdate     { return o.bookCh }

func (o *OKX) Connect(ctx context.Context) error {
	go o.runWithReconnect(ctx)
	return nil
}

func (o *OKX) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := o.run(ctx); err != nil {
			o.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (o *OKX) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, o.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Subscribe to both tickers (BBO) and books5 (L2 snapshots).
	sub := map[string]any{
		"op": "subscribe",
		"args": []map[string]string{
			{"channel": "tickers", "instId": "BTC-USDT"},
			{"channel": "books5", "instId": "BTC-USDT"},
		},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	// OKX requires a ping every 25s; responds with plain "pong".
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteMessage(websocket.TextMessage, []byte("ping")) //nolint:errcheck
			}
		}
	}()

	o.logger.Info("connected")

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
		if string(msg) == "pong" {
			continue
		}

		t0 := time.Now()

		// Peek at the channel to dispatch.
		var peek struct {
			Event string `json:"event"`
			Arg   struct {
				Channel string `json:"channel"`
			} `json:"arg"`
		}
		if err := json.Unmarshal(msg, &peek); err != nil || peek.Event != "" {
			continue
		}

		switch peek.Arg.Channel {
		case "tickers":
			pu, ok := o.parseMessage(msg)
			if !ok {
				continue
			}
			if o.tracker != nil {
				o.tracker.Record(time.Since(t0))
			}
			select {
			case o.ch <- pu:
			default:
			}
		case "books5":
			bu, ok := o.parseBooks5(msg)
			if !ok {
				continue
			}
			select {
			case o.bookCh <- bu:
			default:
			}
		}
	}
}

// parseMessage decodes an OKX tickers frame into a PriceUpdate.
// Subscribe-ack frames (with "event" field set) and data-less frames are rejected.
func (o *OKX) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var peek struct {
		Event string            `json:"event"`
		Data  []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg, &peek); err != nil || peek.Event != "" {
		return types.PriceUpdate{}, false
	}
	if len(peek.Data) == 0 {
		return types.PriceUpdate{}, false
	}
	var tick struct {
		BidPx string `json:"bidPx"`
		AskPx string `json:"askPx"`
		BidSz string `json:"bidSz"`
		AskSz string `json:"askSz"`
	}
	if err := json.Unmarshal(peek.Data[0], &tick); err != nil {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(tick.BidPx)
	ask, e2 := decimal.NewFromString(tick.AskPx)
	bidSize, e3 := decimal.NewFromString(tick.BidSz)
	askSize, e4 := decimal.NewFromString(tick.AskSz)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	if bid.IsZero() || ask.IsZero() {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "okx",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}

// parseBooks5 decodes an OKX books5 frame into a BookUpdate.
// Frame format: {"arg":{"channel":"books5","instId":"BTC-USDT"},"data":[{"bids":[["p","q","0","n"]...],"asks":[...],...}]}
// Each level is [price, qty, deprecated, orderCount].
func (o *OKX) parseBooks5(msg []byte) (BookUpdate, bool) {
	var frame struct {
		Data []struct {
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil || len(frame.Data) == 0 {
		return BookUpdate{}, false
	}
	entry := frame.Data[0]

	bids, ok := parseLevels(entry.Bids)
	if !ok {
		return BookUpdate{}, false
	}
	asks, ok := parseLevels(entry.Asks)
	if !ok {
		return BookUpdate{}, false
	}

	return BookUpdate{
		Exchange:   "okx",
		Bids:       bids,
		Asks:       asks,
		ReceivedAt: time.Now(),
	}, true
}
