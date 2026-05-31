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

type Gate struct {
	wsURL   string
	ch      chan types.PriceUpdate
	logger  *slog.Logger
	tracker *metrics.LatencyTracker
}

func NewGate(wsURL string, tracker *metrics.LatencyTracker) *Gate {
	return &Gate{
		wsURL:   wsURL,
		ch:      make(chan types.PriceUpdate, 512),
		logger:  slog.Default().With("exchange", "gate"),
		tracker: tracker,
	}
}

func (g *Gate) Name() string                      { return "gate" }
func (g *Gate) Updates() <-chan types.PriceUpdate { return g.ch }

func (g *Gate) Connect(ctx context.Context) error {
	go g.runWithReconnect(ctx)
	return nil
}

func (g *Gate) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := g.run(ctx); err != nil {
			g.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (g *Gate) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, g.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"time":    time.Now().Unix(),
		"channel": "spot.book_ticker",
		"event":   "subscribe",
		"payload": []string{"BTC_USDT"},
	}); err != nil {
		return err
	}

	// Gate.io requires a ping every 30s.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteJSON(map[string]any{ //nolint:errcheck
					"time":    time.Now().Unix(),
					"channel": "spot.ping",
					"event":   "",
				})
			}
		}
	}()

	g.logger.Info("connected")

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
		pu, ok := g.parseMessage(msg)
		if !ok {
			continue
		}
		if g.tracker != nil {
			g.tracker.Record(time.Since(t0))
		}
		select {
		case g.ch <- pu:
		default:
		}
	}
}

// parseMessage decodes a Gate.io v4 spot.book_ticker update frame.
func (g *Gate) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var envelope struct {
		Channel string `json:"channel"`
		Event   string `json:"event"`
		Result  struct {
			Bid     string `json:"b"`
			BidSize string `json:"B"`
			Ask     string `json:"a"`
			AskSize string `json:"A"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return types.PriceUpdate{}, false
	}
	if envelope.Channel != "spot.book_ticker" || envelope.Event != "update" {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(envelope.Result.Bid)
	ask, e2 := decimal.NewFromString(envelope.Result.Ask)
	bidSize, e3 := decimal.NewFromString(envelope.Result.BidSize)
	askSize, e4 := decimal.NewFromString(envelope.Result.AskSize)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	if bid.IsZero() || ask.IsZero() {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "gate",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}
