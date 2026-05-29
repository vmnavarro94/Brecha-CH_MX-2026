package store

import (
	"sync"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// Store is an in-memory store for Opportunity and Trade records.
// All methods are safe for concurrent use.
type Store struct {
	mu            sync.RWMutex
	opportunities map[string]types.Opportunity
	trades        []types.Trade
}

// NewStore creates and returns a new empty Store.
func NewStore() *Store {
	return &Store{
		opportunities: make(map[string]types.Opportunity),
	}
}

// Save persists an Opportunity, overwriting any existing record with the same ID.
func (s *Store) Save(opp types.Opportunity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opportunities[opp.ID] = opp
}

// SaveTrade appends a Trade to the store.
func (s *Store) SaveTrade(trade types.Trade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, trade)
}

// GetByID returns the Opportunity with the given ID and true, or nil and false if not found.
func (s *Store) GetByID(id string) (*types.Opportunity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	opp, ok := s.opportunities[id]
	if !ok {
		return nil, false
	}
	copy := opp
	return &copy, true
}

// QueryByStatus returns all Opportunities with the given status.
func (s *Store) QueryByStatus(status types.OpportunityStatus) []types.Opportunity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.Opportunity, 0)
	for _, opp := range s.opportunities {
		if opp.Status == status {
			result = append(result, opp)
		}
	}
	return result
}

// AllTrades returns a copy of all stored trades.
func (s *Store) AllTrades() []types.Trade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.Trade, len(s.trades))
	copy(result, s.trades)
	return result
}
