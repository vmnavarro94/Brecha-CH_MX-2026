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

type Kraken struct {
	wsURL  string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewKraken(wsURL string) *Kraken {
	return &Kraken{
		wsURL:  wsURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "kraken"),
	}
}

func (k *Kraken) Name() string                       { return "kraken" }
func (k *Kraken) Updates() <-chan types.PriceUpdate  { return k.ch }

func (k *Kraken) Connect(ctx context.Context) error {
	go k.runWithReconnect(ctx)
	return nil
}

func (k *Kraken) runWithReconnect(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := k.run(ctx); err != nil {
			k.logger.Error("disconnected", "err", err, "attempt", attempt)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

func (k *Kraken) run(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, k.wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	sub := map[string]any{
		"event": "subscribe",
		"pair":  []string{"XBT/USDT"},
		"subscription": map[string]string{
			"name": "ticker",
		},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	k.logger.Info("connected")

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

		// Kraken sends both event objects and array-format ticker updates.
		// Arrays start with '[', objects with '{'.
		pu, ok := k.parseMessage(msg)
		if !ok {
			continue
		}
		select {
		case k.ch <- pu:
		default:
		}
	}
}

// parseMessage decodes a Kraken v1 ticker array frame into a PriceUpdate.
// Frame format: [channelID, {ticker}, "ticker", "XBT/USDT"]
// ticker.b / ticker.a are [price, wholeLotVolume, lotVolume] arrays.
func (k *Kraken) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	if len(msg) == 0 || msg[0] != '[' {
		return types.PriceUpdate{}, false
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(msg, &raw); err != nil || len(raw) < 4 {
		return types.PriceUpdate{}, false
	}
	var ticker struct {
		Bid []json.RawMessage `json:"b"`
		Ask []json.RawMessage `json:"a"`
	}
	if err := json.Unmarshal(raw[1], &ticker); err != nil {
		return types.PriceUpdate{}, false
	}
	if len(ticker.Bid) < 3 || len(ticker.Ask) < 3 {
		return types.PriceUpdate{}, false
	}
	var bidStr, askStr, bidLots, askLots string
	if err := json.Unmarshal(ticker.Bid[0], &bidStr); err != nil {
		return types.PriceUpdate{}, false
	}
	if err := json.Unmarshal(ticker.Ask[0], &askStr); err != nil {
		return types.PriceUpdate{}, false
	}
	if err := json.Unmarshal(ticker.Bid[2], &bidLots); err != nil {
		return types.PriceUpdate{}, false
	}
	if err := json.Unmarshal(ticker.Ask[2], &askLots); err != nil {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(bidStr)
	ask, e2 := decimal.NewFromString(askStr)
	bidSize, e3 := decimal.NewFromString(bidLots)
	askSize, e4 := decimal.NewFromString(askLots)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "kraken",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}
