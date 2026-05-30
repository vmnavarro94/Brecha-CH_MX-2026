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

type MEXC struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewMEXC(wsURL string) *MEXC {
	return &MEXC{
		wsURL:  wsURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "mexc"),
	}
}

func (m *MEXC) Name() string                      { return "mexc" }
func (m *MEXC) Updates() <-chan types.PriceUpdate { return m.ch }

func (m *MEXC) Connect(ctx context.Context) error {
	go m.runWithReconnect(ctx)
	return nil
}

func (m *MEXC) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := m.run(ctx); err != nil {
			m.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (m *MEXC) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, m.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"method": "SUBSCRIPTION",
		"params": []string{"spot@public.bookTicker.v3.api@BTCUSDT"},
	}); err != nil {
		return err
	}

	// MEXC requires a periodic PING to stay alive.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteJSON(map[string]string{"method": "PING"}) //nolint:errcheck
			}
		}
	}()

	m.logger.Info("connected")

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
		if rawCount <= 5 {
			preview := string(msg)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			m.logger.Info("raw msg", "n", rawCount, "data", preview)
		}

		// MEXC server sends {"msg":"PING"} every ~20s; must respond with {"msg":"PONG"}.
		var ping struct{ Msg string `json:"msg"` }
		if json.Unmarshal(msg, &ping) == nil && ping.Msg == "PING" {
			conn.WriteJSON(map[string]string{"msg": "PONG"}) //nolint:errcheck
			continue
		}

		// MEXC bookTicker v3 format:
		// {"c":"spot@public.bookTicker.v3.api@BTCUSDT","d":{"b":"...","B":"...","a":"...","A":"..."}}
		var envelope struct {
			Channel string `json:"c"`
			Data    struct {
				Bid     string `json:"b"`
				BidSize string `json:"B"`
				Ask     string `json:"a"`
				AskSize string `json:"A"`
			} `json:"d"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}
		if envelope.Channel != "spot@public.bookTicker.v3.api@BTCUSDT" {
			continue
		}

		bid, e1 := decimal.NewFromString(envelope.Data.Bid)
		ask, e2 := decimal.NewFromString(envelope.Data.Ask)
		bidSize, e3 := decimal.NewFromString(envelope.Data.BidSize)
		askSize, e4 := decimal.NewFromString(envelope.Data.AskSize)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		if bid.IsZero() || ask.IsZero() {
			continue
		}

		select {
		case m.ch <- types.PriceUpdate{
			Exchange:   "mexc",
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
