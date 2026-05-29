package exchange

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type Bitget struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewBitget(wsURL string) *Bitget {
	return &Bitget{
		wsURL:  wsURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "bitget"),
	}
}

func (b *Bitget) Name() string                      { return "bitget" }
func (b *Bitget) Updates() <-chan types.PriceUpdate { return b.ch }

func (b *Bitget) Connect(ctx context.Context) error {
	go b.runWithReconnect(ctx)
	return nil
}

func (b *Bitget) runWithReconnect(ctx context.Context) {
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

func (b *Bitget) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, b.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"op":   "subscribe",
		"args": []map[string]string{{"instType": "SPOT", "channel": "books1", "instId": "BTCUSDT"}},
	}); err != nil {
		return err
	}

	// Bitget requires a ping every 25s; responds with {"op":"pong"}.
	go func() {
		ticker := time.NewTicker(25 * time.Second)
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

		// Bitget books1 format:
		// {"action":"snapshot","arg":{...},"data":[{"asks":[["price","qty"]],"bids":[["price","qty"]],"ts":"..."}]}
		var envelope struct {
			Action string `json:"action"`
			Arg    struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				Asks [][]string `json:"asks"`
				Bids [][]string `json:"bids"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}
		if envelope.Arg.Channel != "books1" {
			continue
		}
		if len(envelope.Data) == 0 {
			continue
		}
		d := envelope.Data[0]
		if len(d.Bids) == 0 || len(d.Asks) == 0 {
			continue
		}
		if len(d.Bids[0]) < 2 || len(d.Asks[0]) < 2 {
			continue
		}

		bid, e1 := decimal.NewFromString(d.Bids[0][0])
		bidSize, e2 := decimal.NewFromString(d.Bids[0][1])
		ask, e3 := decimal.NewFromString(d.Asks[0][0])
		askSize, e4 := decimal.NewFromString(d.Asks[0][1])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		if bid.IsZero() || ask.IsZero() {
			continue
		}

		select {
		case b.ch <- types.PriceUpdate{
			Exchange:   "bitget",
			Bid:        bid,
			Ask:        ask,
			BidSize:    bidSize,
			AskSize:    askSize,
			ReceivedAt: time.Now(),
		}:
		default:
		}
	}
}
