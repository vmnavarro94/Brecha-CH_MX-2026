package strategy

import (
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Strategy is the interface every detection algorithm must implement.
// Name returns a stable identifier used for logging and metrics attribution.
// Detect examines one incoming price update against the current multi-exchange snapshot
// and returns all arbitrage opportunities found. An empty slice means none were detected.
// The now parameter is injected by the caller so implementations stay deterministic.
type Strategy interface {
	Name() string
	Detect(update types.PriceUpdate, snapshot map[string]types.PriceUpdate, now time.Time) []types.Opportunity
}
