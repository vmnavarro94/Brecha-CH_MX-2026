package backtest

import (
	"math"
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// StrategyMetrics holds computed performance metrics for a single strategy.
type StrategyMetrics struct {
	TotalPnL     float64
	MaxDrawdown  float64
	Sharpe       float64
	HitRate      float64
	ProfitFactor float64
	TradeCount   int
}

// ComputeMetrics groups trades by Strategy and computes all metrics for each
// strategy. from/to define the replay window and are used for Sharpe annualization.
// Returns an empty map when trades is nil or empty.
func ComputeMetrics(trades []types.Trade, from, to time.Time) map[string]StrategyMetrics {
	if len(trades) == 0 {
		return make(map[string]StrategyMetrics)
	}

	byStrategy := make(map[string][]types.Trade)
	for _, t := range trades {
		byStrategy[t.Strategy] = append(byStrategy[t.Strategy], t)
	}

	result := make(map[string]StrategyMetrics, len(byStrategy))
	for strat, st := range byStrategy {
		result[strat] = StrategyMetrics{
			TotalPnL:     sumNetProfit(st),
			MaxDrawdown:  maxDrawdown(st),
			Sharpe:       sharpeRatio(st, from, to),
			HitRate:      hitRate(st),
			ProfitFactor: profitFactor(st),
			TradeCount:   len(st),
		}
	}
	return result
}

// sumNetProfit returns the arithmetic sum of NetProfit across all trades.
func sumNetProfit(trades []types.Trade) float64 {
	total := 0.0
	for _, t := range trades {
		v, _ := t.NetProfit.Float64()
		total += v
	}
	return total
}

// maxDrawdown returns the maximum peak-to-trough decline on the cumulative
// P&L equity curve. Returns 0 for monotonically non-decreasing sequences.
func maxDrawdown(trades []types.Trade) float64 {
	if len(trades) == 0 {
		return 0
	}
	var peak, eq float64
	var dd float64
	for _, t := range trades {
		v, _ := t.NetProfit.Float64()
		eq += v
		if eq > peak {
			peak = eq
		}
		if drop := peak - eq; drop > dd {
			dd = drop
		}
	}
	return dd
}

// sharpeRatio returns mean(returns)/std(returns)*sqrt(annualizationFactor).
// Returns 0 when len < 2, std == 0, or the replay window is zero.
func sharpeRatio(trades []types.Trade, from, to time.Time) float64 {
	n := float64(len(trades))
	if n < 2 {
		return 0
	}
	replaySec := to.Sub(from).Seconds()
	if replaySec <= 0 {
		return 0
	}

	returns := make([]float64, len(trades))
	for i, t := range trades {
		returns[i], _ = t.NetProfit.Float64()
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= n

	variance := 0.0
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	variance /= (n - 1)

	std := math.Sqrt(variance)
	if std == 0 {
		return 0
	}

	tradesPerYear := n * secondsPerYear / replaySec
	return mean / std * math.Sqrt(tradesPerYear)
}

// hitRate returns winning_trades / total_trades, or 0 when no trades.
func hitRate(trades []types.Trade) float64 {
	if len(trades) == 0 {
		return 0
	}
	wins := 0
	for _, t := range trades {
		if t.NetProfit.IsPositive() {
			wins++
		}
	}
	return float64(wins) / float64(len(trades))
}

// profitFactor returns sum(positive NetProfit) / abs(sum(negative NetProfit)).
// Returns 999 when there are no losing trades with non-zero losses.
// Returns 0 when there are no trades.
func profitFactor(trades []types.Trade) float64 {
	if len(trades) == 0 {
		return 0
	}
	var pos, neg float64
	for _, t := range trades {
		v, _ := t.NetProfit.Float64()
		if v > 0 {
			pos += v
		} else if v < 0 {
			neg += v
		}
	}
	if neg == 0 {
		return 999
	}
	return pos / math.Abs(neg)
}
