package exchange

import (
	"time"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// BookConnector is implemented by connectors that can stream L2 order book snapshots.
// Exactly Binance, Bybit, and OKX satisfy this interface; the remaining seven do not.
type BookConnector interface {
	BookUpdates() <-chan BookUpdate
}

// BookUpdate carries a full L2 snapshot from one exchange at a point in time.
type BookUpdate struct {
	Exchange   string
	Bids       []types.OrderBookLevel
	Asks       []types.OrderBookLevel
	ReceivedAt time.Time
}
