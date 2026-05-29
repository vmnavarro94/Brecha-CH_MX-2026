package feed

import (
	"context"
	"sync"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/exchange"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Aggregator fans out price updates from all connectors to subscribers.
type Aggregator struct {
	connectors []exchange.Connector
	prices     map[string]*types.PriceUpdate
	mu         sync.RWMutex
	out        chan types.PriceUpdate
}

func NewAggregator(connectors []exchange.Connector) *Aggregator {
	return &Aggregator{
		connectors: connectors,
		prices:     make(map[string]*types.PriceUpdate),
		out:        make(chan types.PriceUpdate, 512),
	}
}

func (a *Aggregator) Start(ctx context.Context) error {
	for _, c := range a.connectors {
		if err := c.Connect(ctx); err != nil {
			return err
		}
		go a.drain(ctx, c)
	}
	return nil
}

// Updates returns the fan-out channel consumed by the engine and spread model.
func (a *Aggregator) Updates() <-chan types.PriceUpdate {
	return a.out
}

// Latest returns the most recent price for a given exchange, or nil if unknown.
func (a *Aggregator) Latest(exchange string) *types.PriceUpdate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p := a.prices[exchange]
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// Snapshot returns the current BBO for all exchanges.
func (a *Aggregator) Snapshot() map[string]types.PriceUpdate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]types.PriceUpdate, len(a.prices))
	for k, v := range a.prices {
		out[k] = *v
	}
	return out
}

func (a *Aggregator) drain(ctx context.Context, c exchange.Connector) {
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-c.Updates():
			if !ok {
				return
			}
			a.mu.Lock()
			a.prices[update.Exchange] = &update
			a.mu.Unlock()

			select {
			case a.out <- update:
			default:
			}
		}
	}
}
