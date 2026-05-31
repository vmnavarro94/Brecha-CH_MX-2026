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
// WithdrawalBTC is the flat BTC fee charged to move bought BTC out of the buy
// exchange — amortised per trade as part of the round-trip cost of rebalancing.
// NetworkLatencyBps is the basis-points hit per leg modelling price drift during
// the WS round-trip. Higher for geographically distant or rate-limited venues.
type FeeConfig struct {
	TakerFee          float64
	SlippageFactor    float64
	WithdrawalBTC     float64
	NetworkLatencyBps float64
}

// Fees holds standard retail trading + withdrawal + network costs per exchange.
// Withdrawal values reflect public BTC withdrawal fees (Q1 2026). NetworkLatencyBps
// is calibrated to typical observed round-trip drift per geo (EU/US/Asia).
var Fees = map[string]FeeConfig{
	"binance":   {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00020, NetworkLatencyBps: 1.0},
	"kraken":    {TakerFee: 0.0026, SlippageFactor: 0.0003, WithdrawalBTC: 0.00005, NetworkLatencyBps: 2.0},
	"bybit":     {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00050, NetworkLatencyBps: 2.0},
	"okx":       {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00040, NetworkLatencyBps: 2.0},
	"gate":      {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00050, NetworkLatencyBps: 3.0},
	"mexc":      {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00050, NetworkLatencyBps: 3.0},
	"bitget":    {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00030, NetworkLatencyBps: 2.0},
	"htx":       {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00010, NetworkLatencyBps: 3.0},
	"cryptocom": {TakerFee: 0.0007, SlippageFactor: 0.0002, WithdrawalBTC: 0.00006, NetworkLatencyBps: 2.0},
	"kucoin":    {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00050, NetworkLatencyBps: 3.0},
}

// DemoFees is retained as an alias to Fees for backward-compat with consumers
// that may still reference it. Demo mode flag is now purely a UI semantic
// (controls whether the TweaksPanel allows live fee editing).
// Deprecated: use exchange.Fees directly.
var DemoFees = Fees

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
