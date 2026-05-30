package strategy_test

import (
	"context"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/strategy"
)

// stubbedFundingStrategy is a minimal stub implementing strategy.Starter.
// This compile-time assertion confirms that:
//   - strategy.Starter is defined with Start(ctx context.Context) error
//   - A type can satisfy it
//
// Note: the real FundingStrategy will satisfy this interface in Phase 3.
type stubbedFundingStrategy struct{}

func (s *stubbedFundingStrategy) Start(_ context.Context) error { return nil }

// TestStarterInterface_FundingStrategyImplements is a compile-time assertion that
// the Starter interface is defined in internal/strategy with the correct method set.
// It will fail to compile until starter.go defines: type Starter interface { Start(ctx context.Context) error }
var _ strategy.Starter = (*stubbedFundingStrategy)(nil)
