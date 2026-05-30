package spatial

// FeeConfig holds trading costs for a single exchange.
// WithdrawalBTC is a flat BTC cost amortised per arbitrage trade — accounts for the
// round-trip rebalancing cost of moving the bought BTC out of the buy exchange.
// NetworkLatencyBps is a per-leg basis-points cost modelling price drift during the
// network round-trip; e.g. 3 bps = 0.03% of leg notional.
type FeeConfig struct {
	TakerFee          float64
	SlippageFactor    float64
	WithdrawalBTC     float64
	NetworkLatencyBps float64
}

// feeFor returns the FeeConfig for the given exchange, falling back to sensible defaults.
func feeFor(fees map[string]FeeConfig, exchange string) FeeConfig {
	if fee, ok := fees[exchange]; ok {
		return fee
	}
	return FeeConfig{TakerFee: 0.001, SlippageFactor: 0.0002, WithdrawalBTC: 0.0002, NetworkLatencyBps: 2.0}
}
