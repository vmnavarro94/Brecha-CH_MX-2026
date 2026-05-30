package strategy_test

import (
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// mockStrategy is a compile-time assertion that the Strategy interface exists
// with the correct method set.
type mockStrategy struct{}

func (m mockStrategy) Name() string { return "mock" }
func (m mockStrategy) Detect(
	update types.PriceUpdate,
	snapshot map[string]types.PriceUpdate,
	now time.Time,
) []types.Opportunity {
	return nil
}

// Compile-time interface check — this file must fail to compile until
// internal/strategy/strategy.go defines the Strategy interface.
var _ strategy.Strategy = mockStrategy{}
