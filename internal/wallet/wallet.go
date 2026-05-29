package wallet

import (
	"errors"
	"sync"
)

// ErrInsufficientFunds is returned when a Debit operation would exceed the current balance.
var ErrInsufficientFunds = errors.New("insufficient funds")

// MultiWallet manages balances for multiple exchanges and currencies in memory.
// All operations are safe for concurrent use.
type MultiWallet struct {
	mu       sync.Mutex
	balances map[string]map[string]float64 // exchange -> currency -> amount
}

// NewMultiWallet creates a MultiWallet pre-seeded with the given exchanges and initial balances.
// initial is a map of currency -> starting amount applied to every exchange.
func NewMultiWallet(exchanges []string, initial map[string]float64) *MultiWallet {
	w := &MultiWallet{
		balances: make(map[string]map[string]float64, len(exchanges)),
	}
	for _, ex := range exchanges {
		w.balances[ex] = make(map[string]float64, len(initial))
		for currency, amount := range initial {
			w.balances[ex][currency] = amount
		}
	}
	return w
}

// Credit adds amount to the balance for the given exchange and currency.
func (w *MultiWallet) Credit(exchange, currency string, amount float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ensureExists(exchange, currency)
	w.balances[exchange][currency] += amount
}

// Debit subtracts amount from the balance for the given exchange and currency.
// Returns ErrInsufficientFunds and leaves balance unchanged if amount > current balance.
func (w *MultiWallet) Debit(exchange, currency string, amount float64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ensureExists(exchange, currency)
	if w.balances[exchange][currency] < amount {
		return ErrInsufficientFunds
	}
	w.balances[exchange][currency] -= amount
	return nil
}

// Balance returns the current balance for the given exchange and currency.
// Returns 0 if the combination does not exist.
func (w *MultiWallet) Balance(exchange, currency string) float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if currencies, ok := w.balances[exchange]; ok {
		return currencies[currency]
	}
	return 0
}

// ensureExists initialises the exchange and currency maps if they don't exist.
// MUST be called with the mutex held.
func (w *MultiWallet) ensureExists(exchange, currency string) {
	if _, ok := w.balances[exchange]; !ok {
		w.balances[exchange] = make(map[string]float64)
	}
	if _, ok := w.balances[exchange][currency]; !ok {
		w.balances[exchange][currency] = 0
	}
}
