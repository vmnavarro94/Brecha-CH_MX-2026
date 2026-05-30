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

type Binance struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewBinance(wsURL string) *Binance {
	return &Binance{
		wsURL:  wsURL + "/ws/btcusdt@bookTicker",
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "binance"),
	}
}

func (b *Binance) Name() string                       { return "binance" }
func (b *Binance) Updates() <-chan types.PriceUpdate  { return b.ch }

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

		pu, ok := b.parseMessage(msg)
		if !ok {
			continue
		}

		select {
		case b.ch <- pu:
		default:
		}
	}
}

// parseMessage decodes a Binance @bookTicker frame into a PriceUpdate.
// Returns ok=false for non-JSON frames or unparseable numeric fields.
func (b *Binance) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var tick struct {
		Bid     string `json:"b"`
		BidSize string `json:"B"`
		Ask     string `json:"a"`
		AskSize string `json:"A"`
	}
	if err := json.Unmarshal(msg, &tick); err != nil {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(tick.Bid)
	ask, e2 := decimal.NewFromString(tick.Ask)
	bidSize, e3 := decimal.NewFromString(tick.BidSize)
	askSize, e4 := decimal.NewFromString(tick.AskSize)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
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
