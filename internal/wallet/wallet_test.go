package wallet

import (
	"sync"
	"testing"
)

// TestCreditIncreasesBalance verifies that Credit increases the balance by the given amount.
func TestCreditIncreasesBalance(t *testing.T) {
	w := NewMultiWallet([]string{"binance"}, map[string]float64{"USDT": 100.0})
	w.Credit("binance", "USDT", 50.0)
	got := w.Balance("binance", "USDT")
	if got != 150.0 {
		t.Errorf("Balance after Credit: got %v, want 150.0", got)
	}
}

// TestDebitDecreasesBalance verifies that a successful Debit reduces the balance.
func TestDebitDecreasesBalance(t *testing.T) {
	w := NewMultiWallet([]string{"binance"}, map[string]float64{"USDT": 100.0})
	if err := w.Debit("binance", "USDT", 40.0); err != nil {
		t.Fatalf("Debit returned unexpected error: %v", err)
	}
	got := w.Balance("binance", "USDT")
	if got != 60.0 {
		t.Errorf("Balance after Debit: got %v, want 60.0", got)
	}
}

// TestDebitInsufficientFunds verifies that Debit returns an error and leaves balance unchanged
// when the requested amount exceeds the current balance.
func TestDebitInsufficientFunds(t *testing.T) {
	w := NewMultiWallet([]string{"binance"}, map[string]float64{"USDT": 10.0})
	err := w.Debit("binance", "USDT", 15.0)
	if err == nil {
		t.Error("Debit should return error on insufficient funds")
	}
	got := w.Balance("binance", "USDT")
	if got != 10.0 {
		t.Errorf("Balance should remain unchanged after failed Debit: got %v, want 10.0", got)
	}
}

// TestBalanceReturnsCurrentAmount verifies that Balance returns the correct value after
// multiple operations.
func TestBalanceReturnsCurrentAmount(t *testing.T) {
	w := NewMultiWallet([]string{"kraken"}, map[string]float64{"BTC": 1.0})
	w.Credit("kraken", "BTC", 0.5)
	_ = w.Debit("kraken", "BTC", 0.2)
	got := w.Balance("kraken", "BTC")
	if got != 1.3 {
		t.Errorf("Balance: got %v, want 1.3", got)
	}
}

// TestConcurrentCredit spawns 100 goroutines each Credit 1.0 and verifies the final balance
// equals 100.0. Run with -race to detect data races.
func TestConcurrentCredit(t *testing.T) {
	w := NewMultiWallet([]string{"binance"}, map[string]float64{"USDT": 0.0})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Credit("binance", "USDT", 1.0)
		}()
	}
	wg.Wait()

	got := w.Balance("binance", "USDT")
	if got != 100.0 {
		t.Errorf("Concurrent Credit balance: got %v, want 100.0", got)
	}
}
