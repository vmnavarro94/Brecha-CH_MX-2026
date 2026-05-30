package depth

import (
	"math/rand"

	"github.com/shopspring/decimal"
)

// Level represents a single price level in a synthetic order book.
type Level struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

// Config holds parameters for synthetic book generation.
type Config struct {
	N         int        // number of levels to generate
	StepPct   float64    // fractional price step per level (e.g. 0.0001 = 0.01%)
	MinQtyBTC float64    // minimum quantity per level in BTC
	MaxQtyBTC float64    // maximum quantity per level in BTC
	Rand      *rand.Rand // seeded source for deterministic tests
}

// AskLevels generates N ask levels starting at bbo, each step StepPct worse (higher) than
// the previous, with randomised quantities in [MinQtyBTC, MaxQtyBTC].
func AskLevels(bbo decimal.Decimal, cfg Config) []Level {
	levels := make([]Level, cfg.N)
	step := bbo.Mul(decimal.NewFromFloat(cfg.StepPct))
	for i := 0; i < cfg.N; i++ {
		price := bbo.Add(step.Mul(decimal.NewFromInt(int64(i))))
		qty := cfg.MinQtyBTC + cfg.Rand.Float64()*(cfg.MaxQtyBTC-cfg.MinQtyBTC)
		levels[i] = Level{
			Price: price,
			Qty:   decimal.NewFromFloat(qty),
		}
	}
	return levels
}

// BidLevels generates N bid levels starting at bbo, each step StepPct worse (lower) than
// the previous, with randomised quantities in [MinQtyBTC, MaxQtyBTC].
func BidLevels(bbo decimal.Decimal, cfg Config) []Level {
	levels := make([]Level, cfg.N)
	step := bbo.Mul(decimal.NewFromFloat(cfg.StepPct))
	for i := 0; i < cfg.N; i++ {
		price := bbo.Sub(step.Mul(decimal.NewFromInt(int64(i))))
		qty := cfg.MinQtyBTC + cfg.Rand.Float64()*(cfg.MaxQtyBTC-cfg.MinQtyBTC)
		levels[i] = Level{
			Price: price,
			Qty:   decimal.NewFromFloat(qty),
		}
	}
	return levels
}

// Walk iterates levels greedily, accumulating filled quantity up to target, and returns the
// volume-weighted average price of consumed liquidity plus a partial-fill flag.
func Walk(levels []Level, target decimal.Decimal) (filled, vwap decimal.Decimal, partial bool) {
	costSum := decimal.Zero
	filled = decimal.Zero
	for _, lvl := range levels {
		remaining := target.Sub(filled)
		if remaining.Sign() <= 0 {
			break
		}
		take := decimal.Min(lvl.Qty, remaining)
		costSum = costSum.Add(take.Mul(lvl.Price))
		filled = filled.Add(take)
	}
	partial = filled.LessThan(target)
	if filled.IsZero() {
		vwap = decimal.Zero
	} else {
		vwap = costSum.Div(filled)
	}
	return
}
