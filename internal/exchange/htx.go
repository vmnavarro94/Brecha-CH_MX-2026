package exchange

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type HTX struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewHTX(wsURL string) *HTX {
	return &HTX{
		wsURL:  wsURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "htx"),
	}
}

func (h *HTX) Name() string                      { return "htx" }
func (h *HTX) Updates() <-chan types.PriceUpdate { return h.ch }

func (h *HTX) Connect(ctx context.Context) error {
	go h.runWithReconnect(ctx)
	return nil
}

func (h *HTX) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := h.run(ctx); err != nil {
			h.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (h *HTX) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{
		"sub": "market.btcusdt.bbo",
		"id":  "1",
	}); err != nil {
		return err
	}

	h.logger.Info("connected")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, compressed, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		// HTX messages are GZip compressed.
		gz, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			continue
		}
		msg, err := io.ReadAll(gz)
		gz.Close()
		if err != nil {
			continue
		}

		// HTX sends ping frames: {"ping": 1234567890}. Must respond with pong.
		var ping struct {
			Ping int64 `json:"ping"`
		}
		if json.Unmarshal(msg, &ping); ping.Ping != 0 {
			conn.WriteJSON(map[string]int64{"pong": ping.Ping}) //nolint:errcheck
			continue
		}

		pu, ok := h.parseMessage(msg)
		if !ok {
			continue
		}
		select {
		case h.ch <- pu:
		default:
		}
	}
}

// parseMessage decodes a HTX market.btcusdt.bbo frame (after gzip + ping handling).
func (h *HTX) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var envelope struct {
		Ch   string `json:"ch"`
		Tick struct {
			Bid     float64 `json:"bid"`
			BidSize float64 `json:"bidSize"`
			Ask     float64 `json:"ask"`
			AskSize float64 `json:"askSize"`
		} `json:"tick"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil || envelope.Ch != "market.btcusdt.bbo" {
		return types.PriceUpdate{}, false
	}
	if envelope.Tick.Bid == 0 || envelope.Tick.Ask == 0 {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "htx",
		Bid:        decimal.NewFromFloat(envelope.Tick.Bid),
		Ask:        decimal.NewFromFloat(envelope.Tick.Ask),
		BidSize:    decimal.NewFromFloat(envelope.Tick.BidSize),
		AskSize:    decimal.NewFromFloat(envelope.Tick.AskSize),
		ReceivedAt: time.Now(),
	}, true
}
