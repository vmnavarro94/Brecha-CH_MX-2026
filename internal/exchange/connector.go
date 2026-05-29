package exchange

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

type Connector interface {
	Connect(ctx context.Context) error
	Updates() <-chan types.PriceUpdate
	Name() string
}

// FeeConfig holds trading costs per exchange.
type FeeConfig struct {
	TakerFee      float64
	SlippageFactor float64
}

var Fees = map[string]FeeConfig{
	"binance": {TakerFee: 0.001, SlippageFactor: 0.0002},
	"kraken":  {TakerFee: 0.0026, SlippageFactor: 0.0003},
	"bybit":   {TakerFee: 0.001, SlippageFactor: 0.0002},
}

func backoff(attempt int) time.Duration {
	base := time.Second
	max := 60 * time.Second
	delay := time.Duration(math.Pow(2, float64(attempt))) * base
	if delay > max {
		delay = max
	}
	jitter := time.Duration(rand.Int63n(int64(delay / 5)))
	return delay + jitter
}
