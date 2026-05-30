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
type FeeConfig struct {
	TakerFee       float64
	SlippageFactor float64
	WithdrawalBTC  float64
}

// Fees holds standard retail taker + withdrawal fees per exchange.
// Withdrawal values reflect public BTC withdrawal fees on each platform (Q1 2026).
var Fees = map[string]FeeConfig{
	"binance":   {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00020},
	"kraken":    {TakerFee: 0.0026, SlippageFactor: 0.0003, WithdrawalBTC: 0.00005},
	"bybit":     {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00050},
	"okx":       {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00040},
	"gate":      {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00050},
	"mexc":      {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00050},
	"bitget":    {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00030},
	"htx":       {TakerFee: 0.002, SlippageFactor: 0.0003, WithdrawalBTC: 0.00010},
	"cryptocom": {TakerFee: 0.0007, SlippageFactor: 0.0002, WithdrawalBTC: 0.00006},
	"kucoin":    {TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.00050},
}

// DemoFees uses near-zero fees so any positive cross-exchange spread triggers a trade.
// Withdrawal cost is set very low so the partial fill / order book logic is still
// exercised end-to-end without the fee model swallowing every opportunity.
var DemoFees = map[string]FeeConfig{
	"binance":   {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"kraken":    {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"bybit":     {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"okx":       {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"gate":      {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"mexc":      {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"bitget":    {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"htx":       {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"cryptocom": {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
	"kucoin":    {TakerFee: 0.00001, SlippageFactor: 0.000001, WithdrawalBTC: 0.0000001},
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
