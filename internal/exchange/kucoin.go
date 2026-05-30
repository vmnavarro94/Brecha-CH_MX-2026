package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type KuCoin struct {
	apiURL string
	ch     chan types.PriceUpdate
	logger *slog.Logger
}

func NewKuCoin(apiURL string) *KuCoin {
	return &KuCoin{
		apiURL: apiURL,
		ch:     make(chan types.PriceUpdate, 512),
		logger: slog.Default().With("exchange", "kucoin"),
	}
}

func (k *KuCoin) Name() string                      { return "kucoin" }
func (k *KuCoin) Updates() <-chan types.PriceUpdate { return k.ch }

func (k *KuCoin) Connect(ctx context.Context) error {
	go k.runWithReconnect(ctx)
	return nil
}

func (k *KuCoin) runWithReconnect(ctx context.Context) {
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

// fetchWSEndpoint calls the KuCoin REST API to get the WS endpoint and token.
func (k *KuCoin) fetchWSEndpoint(ctx context.Context) (wsURL string, pingInterval int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.apiURL+"/api/v1/bullet-public", nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}

	var result struct {
		Data struct {
			Token           string `json:"token"`
			InstanceServers []struct {
				Endpoint        string `json:"endpoint"`
				PingInterval    int    `json:"pingInterval"`
			} `json:"instanceServers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, err
	}
	if len(result.Data.InstanceServers) == 0 || result.Data.Token == "" {
		return "", 0, fmt.Errorf("kucoin: no WS endpoint returned")
	}

	srv := result.Data.InstanceServers[0]
	connID := fmt.Sprintf("%d", rand.Int63())
	wsURL = fmt.Sprintf("%s?token=%s&connectId=%s", srv.Endpoint, result.Data.Token, connID)
	return wsURL, srv.PingInterval, nil
}

func (k *KuCoin) run(ctx context.Context) error {
	wsURL, pingMs, err := k.fetchWSEndpoint(ctx)
	if err != nil {
		return fmt.Errorf("kucoin fetch endpoint: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Wait for welcome message before subscribing.
	if _, _, err := conn.ReadMessage(); err != nil {
		return err
	}

	subID := fmt.Sprintf("%d", rand.Int63())
	if err := conn.WriteJSON(map[string]any{
		"id":             subID,
		"type":           "subscribe",
		"topic":          "/market/ticker:BTC-USDT",
		"privateChannel": false,
		"response":       true,
	}); err != nil {
		return err
	}

	// KuCoin requires periodic pings using the interval from the token response.
	if pingMs <= 0 {
		pingMs = 18000
	}
	go func() {
		ticker := time.NewTicker(time.Duration(pingMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteJSON(map[string]any{ //nolint:errcheck
					"id":   fmt.Sprintf("%d", rand.Int63()),
					"type": "ping",
				})
			}
		}
	}()

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

// parseMessage decodes a KuCoin /market/ticker:BTC-USDT frame.
func (k *KuCoin) parseMessage(msg []byte) (types.PriceUpdate, bool) {
	var envelope struct {
		Type  string `json:"type"`
		Topic string `json:"topic"`
		Data  struct {
			BestBid     string `json:"bestBid"`
			BestBidSize string `json:"bestBidSize"`
			BestAsk     string `json:"bestAsk"`
			BestAskSize string `json:"bestAskSize"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return types.PriceUpdate{}, false
	}
	if envelope.Type != "message" || envelope.Topic != "/market/ticker:BTC-USDT" {
		return types.PriceUpdate{}, false
	}
	bid, e1 := decimal.NewFromString(envelope.Data.BestBid)
	ask, e2 := decimal.NewFromString(envelope.Data.BestAsk)
	bidSize, e3 := decimal.NewFromString(envelope.Data.BestBidSize)
	askSize, e4 := decimal.NewFromString(envelope.Data.BestAskSize)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return types.PriceUpdate{}, false
	}
	if bid.IsZero() || ask.IsZero() {
		return types.PriceUpdate{}, false
	}
	return types.PriceUpdate{
		Exchange:   "kucoin",
		Bid:        bid,
		Ask:        ask,
		BidSize:    bidSize,
		AskSize:    askSize,
		ReceivedAt: time.Now(),
	}, true
}
