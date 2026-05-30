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

type CryptoCom struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewCryptoCom(wsURL string) *CryptoCom {
	return &CryptoCom{
		wsURL:  wsURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "cryptocom"),
	}
}

func (c *CryptoCom) Name() string                      { return "cryptocom" }
func (c *CryptoCom) Updates() <-chan types.PriceUpdate { return c.ch }

func (c *CryptoCom) Connect(ctx context.Context) error {
	go c.runWithReconnect(ctx)
	return nil
}

func (c *CryptoCom) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := c.run(ctx); err != nil {
			c.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (c *CryptoCom) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "subscribe",
		"params": map[string]any{
			"channels": []string{"ticker.BTC_USDT"},
		},
	}); err != nil {
		return err
	}

	c.logger.Info("connected")

	rawCount := 0
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

		rawCount++
		if rawCount <= 8 {
			preview := string(msg)
			if len(preview) > 300 {
				preview = preview[:300]
			}
			c.logger.Info("raw msg", "n", rawCount, "data", preview)
		}

		var raw struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Code   int    `json:"code"`
			Result struct {
				Channel string `json:"channel"`
				Data    []struct {
					Bid     string `json:"b"`
					BidSize string `json:"bs"`
					Ask     string `json:"k"`
					AskSize string `json:"ks"`
				} `json:"data"`
			} `json:"result"`
		}
		if err := json.Unmarshal(msg, &raw); err != nil {
			continue
		}

		// Crypto.com sends a heartbeat that must be acknowledged.
		if raw.Method == "public/heartbeat" {
			conn.WriteJSON(map[string]any{ //nolint:errcheck
				"id":     raw.ID,
				"method": "public/respond-heartbeat",
			})
			continue
		}

		// Accept data regardless of method field; just require correct channel and data.
		if raw.Result.Channel != "ticker" {
			continue
		}
		if len(raw.Result.Data) == 0 {
			continue
		}
		d := raw.Result.Data[0]

		bid, e1 := decimal.NewFromString(d.Bid)
		ask, e2 := decimal.NewFromString(d.Ask)
		bidSize, e3 := decimal.NewFromString(d.BidSize)
		askSize, e4 := decimal.NewFromString(d.AskSize)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		if bid.IsZero() || ask.IsZero() {
			continue
		}

		select {
		case c.ch <- types.PriceUpdate{
			Exchange:   "cryptocom",
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
