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

		// Heartbeats must be acknowledged before parsing; parseMessage rejects them.
		var hb struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(msg, &hb) == nil && hb.Method == "public/heartbeat" {
			conn.WriteJSON(map[string]any{ //nolint:errcheck
				"id":     hb.ID,
				"method": "public/respond-heartbeat",
			})
			continue
		}

		pu, ok := c.parseMessage(msg)
		if !ok {
			continue
		}
		select {
		case c.ch <- pu:
		default:
		}
	}
}

// parseMessage decodes a Crypto.com ticker frame. Heartbeat frames are rejected.
func (c *CryptoCom) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var raw struct {
		Method string `json:"method"`
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
		return types.PriceUpdate{}, false
	}
	if raw.Method == "public/heartbeat" {
		return types.PriceUpdate{}, false
	}
	if raw.Result.Channel != "ticker" || len(raw.Result.Data) == 0 {
		return types.PriceUpdate{}, false
	}
	d := raw.Result.Data[0]
	bid, e1 := decimal.NewFromString(d.Bid)
	ask, e2 := decimal.NewFromString(d.Ask)
	bidSize, e3 := decimal.NewFromString(d.BidSize)
	askSize, e4 := decimal.NewFromString(d.AskSize)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	if bid.IsZero() || ask.IsZero() {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "cryptocom",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}
