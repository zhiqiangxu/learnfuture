package insurance

import (
	"sync"

	"github.com/shopspring/decimal"
)

// Fund represents the insurance fund that absorbs liquidation losses.
//
// In a real exchange:
// - When a position is liquidated, if the liquidation results in remaining margin
//   after covering the loss, the surplus goes to the insurance fund.
// - When a position is liquidated and the loss exceeds the margin (bankruptcy),
//   the insurance fund covers the deficit.
// - If the insurance fund is depleted, ADL (auto-deleveraging) kicks in.
//
// In our simulation:
// - The system has infinite funds, so the insurance fund always has enough.
// - But we still track it for educational purposes to show how the mechanism works.
type Fund struct {
	mu      sync.RWMutex
	balance decimal.Decimal
}

func NewFund(initialBalance decimal.Decimal) *Fund {
	return &Fund{
		balance: initialBalance,
	}
}

// GetBalance returns the current insurance fund balance.
func (f *Fund) GetBalance() decimal.Decimal {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.balance
}

// Contribute adds to the insurance fund (from liquidation surplus).
// When a position is liquidated and the remaining margin > 0 after covering loss,
// the surplus goes to the insurance fund.
func (f *Fund) Contribute(amount decimal.Decimal) {
	if amount.IsNegative() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balance = f.balance.Add(amount)
}

// Cover withdraws from the insurance fund to cover a bankruptcy deficit.
// Returns the amount actually covered (may be less if fund is insufficient).
// If the fund can't fully cover, ADL should be triggered.
func (f *Fund) Cover(deficit decimal.Decimal) (covered decimal.Decimal, needADL bool) {
	if deficit.IsNegative() || deficit.IsZero() {
		return decimal.Zero, false
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.balance.GreaterThanOrEqual(deficit) {
		f.balance = f.balance.Sub(deficit)
		return deficit, false
	}

	// Partial coverage
	covered = f.balance
	f.balance = decimal.Zero
	return covered, true // ADL needed for the uncovered portion
}

