package backtest_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/backtest"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// makeTrades builds a slice of trades from net-profit values for a single strategy.
func makeTrades(strategy string, profits ...float64) []types.Trade {
	trades := make([]types.Trade, len(profits))
	for i, p := range profits {
		trades[i] = types.Trade{
			ID:        fmt.Sprintf("t%d", i),
			Strategy:  strategy,
			NetProfit: decimal.NewFromFloat(p),
		}
	}
	return trades
}

// TestMetrics_TotalPnL_KnownTrades covers spec M1.
func TestMetrics_TotalPnL_KnownTrades(t *testing.T) {
	from := time.Now()
	to := from.Add(time.Hour)

	trades := makeTrades("spatial", 5, 3, -2)
	metrics := backtest.ComputeMetrics(trades, from, to)

	m, ok := metrics["spatial"]
	if !ok {
		t.Fatal("no metrics for spatial")
	}
	if math.Abs(m.TotalPnL-6.0) > 1e-9 {
		t.Errorf("TotalPnL: want 6.0, got %f", m.TotalPnL)
	}

	// No trades.
	empty := backtest.ComputeMetrics(nil, from, to)
	if len(empty) != 0 {
		t.Errorf("expected empty map for nil trades, got %v", empty)
	}
}

// TestMetrics_MaxDrawdown_KnownSequence covers spec M2.
func TestMetrics_MaxDrawdown_KnownSequence(t *testing.T) {
	from := time.Now()
	to := from.Add(time.Hour)

	// Cumulative equity [10,15,8,12,5] requires trades [10,5,-7,4,-7].
	trades := makeTrades("spatial", 10, 5, -7, 4, -7)
	metrics := backtest.ComputeMetrics(trades, from, to)

	m := metrics["spatial"]
	if math.Abs(m.MaxDrawdown-10.0) > 1e-9 {
		t.Errorf("MaxDrawdown: want 10.0, got %f", m.MaxDrawdown)
	}

	// Monotonically increasing equity.
	mono := makeTrades("spatial", 1, 2, 3, 4)
	metrics2 := backtest.ComputeMetrics(mono, from, to)
	if math.Abs(metrics2["spatial"].MaxDrawdown) > 1e-9 {
		t.Errorf("MonotonicallyIncreasing MaxDrawdown: want 0.0, got %f", metrics2["spatial"].MaxDrawdown)
	}
}

// TestMetrics_Sharpe_KnownReturns covers spec M3.
func TestMetrics_Sharpe_KnownReturns(t *testing.T) {
	// 10 trades, from=T, to=T+10s → replaySec=10.
	from := time.Unix(1000, 0)
	to := time.Unix(1010, 0)

	profits := []float64{1, 2, 1, 3, 2, 1, 2, 3, 1, 2}
	trades := makeTrades("spatial", profits...)
	metrics := backtest.ComputeMetrics(trades, from, to)
	m := metrics["spatial"]

	// Compute expected manually.
	n := float64(len(profits))
	mean := 0.0
	for _, p := range profits {
		mean += p
	}
	mean /= n
	variance := 0.0
	for _, p := range profits {
		d := p - mean
		variance += d * d
	}
	variance /= (n - 1)
	std := math.Sqrt(variance)
	replaySec := to.Sub(from).Seconds()
	tradesPerYear := n * 31536000.0 / replaySec
	expected := mean / std * math.Sqrt(tradesPerYear)

	if math.Abs(m.Sharpe-expected) > 1e-9 {
		t.Errorf("Sharpe: want %f, got %f", expected, m.Sharpe)
	}

	// 0 or 1 trade → Sharpe == 0.
	zeroTrades := backtest.ComputeMetrics(makeTrades("spatial", 5), from, to)
	if zeroTrades["spatial"].Sharpe != 0 {
		t.Errorf("1 trade Sharpe: want 0, got %f", zeroTrades["spatial"].Sharpe)
	}
}

// TestMetrics_HitRate covers spec M4.
func TestMetrics_HitRate(t *testing.T) {
	from := time.Now()
	to := from.Add(time.Hour)

	// 4 positive + 6 negative = 10 trades → hitRate = 0.4.
	var profits []float64
	for i := 0; i < 4; i++ {
		profits = append(profits, 1.0)
	}
	for i := 0; i < 6; i++ {
		profits = append(profits, -1.0)
	}
	trades := makeTrades("spatial", profits...)
	metrics := backtest.ComputeMetrics(trades, from, to)

	if math.Abs(metrics["spatial"].HitRate-0.4) > 1e-9 {
		t.Errorf("HitRate: want 0.4, got %f", metrics["spatial"].HitRate)
	}

	// 0 trades.
	empty := backtest.ComputeMetrics(nil, from, to)
	if _, ok := empty["spatial"]; ok {
		t.Error("expected no entry for 0 trades")
	}
}

// TestMetrics_ProfitFactor covers spec M5 (all three scenarios).
func TestMetrics_ProfitFactor(t *testing.T) {
	from := time.Now()
	to := from.Add(time.Hour)

	// Normal: sum(pos)=15, sum(neg)=-5 → 3.0.
	trades := makeTrades("spatial", 5, 7, 3, -2, -3)
	metrics := backtest.ComputeMetrics(trades, from, to)
	if math.Abs(metrics["spatial"].ProfitFactor-3.0) > 1e-9 {
		t.Errorf("ProfitFactor: want 3.0, got %f", metrics["spatial"].ProfitFactor)
	}

	// All positive → 999.
	allPos := makeTrades("spatial", 1, 2, 3)
	metrics2 := backtest.ComputeMetrics(allPos, from, to)
	if math.Abs(metrics2["spatial"].ProfitFactor-999.0) > 1e-9 {
		t.Errorf("AllPositive ProfitFactor: want 999, got %f", metrics2["spatial"].ProfitFactor)
	}

	// 0 trades.
	empty := backtest.ComputeMetrics(nil, from, to)
	if _, ok := empty["spatial"]; ok {
		t.Error("expected no entry for 0 trades")
	}
}

// TestMetrics_AllStrategiesAggregated covers the aggregation across multiple strategies.
func TestMetrics_AllStrategiesAggregated(t *testing.T) {
	from := time.Now()
	to := from.Add(time.Hour)

	var trades []types.Trade
	trades = append(trades, makeTrades("spatial", 1, 2, 3)...)
	trades = append(trades, makeTrades("triangular", 4, 5, 6)...)

	metrics := backtest.ComputeMetrics(trades, from, to)

	if len(metrics) != 2 {
		t.Errorf("want 2 strategy entries, got %d", len(metrics))
	}
	if math.Abs(metrics["spatial"].TotalPnL-6.0) > 1e-9 {
		t.Errorf("spatial TotalPnL: want 6.0, got %f", metrics["spatial"].TotalPnL)
	}
	if math.Abs(metrics["triangular"].TotalPnL-15.0) > 1e-9 {
		t.Errorf("triangular TotalPnL: want 15.0, got %f", metrics["triangular"].TotalPnL)
	}
}
