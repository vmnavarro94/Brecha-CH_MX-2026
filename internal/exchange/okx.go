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
	logger  *slog.Logger
	tracker *metrics.LatencyTracker
}

func NewOKX(wsURL string, tracker *metrics.LatencyTracker) *OKX {
	return &OKX{
		wsURL:   wsURL,
		ch:      make(chan types.PriceUpdate, 512),
		logger:  slog.Default().With("exchange", "okx"),
		tracker: tracker,
	}
}

func (o *OKX) Name() string                      { return "okx" }
func (o *OKX) Updates() <-chan types.PriceUpdate { return o.ch }

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

	sub := map[string]any{
		"op":   "subscribe",
		"args": []map[string]string{{"channel": "tickers", "instId": "BTC-USDT"}},
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
