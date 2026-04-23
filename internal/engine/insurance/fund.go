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

// CalcLiquidationSurplus calculates the surplus/deficit from a liquidation.
//
// When a position is liquidated:
// - bankruptcyPrice = the price at which margin = 0
// - liquidationPrice = the price at which margin_ratio = maintenance_margin_rate
// - If we can liquidate at a better price than bankruptcy: surplus → insurance fund
// - If market moves past bankruptcy price: deficit → insurance fund covers
//
// surplus = (liqPrice - bankruptcyPrice) * quantity * side
// positive = surplus (goes to fund), negative = deficit (fund covers)
func CalcLiquidationSurplus(entryPrice, liqPrice decimal.Decimal, quantity decimal.Decimal, side int, leverage int) decimal.Decimal {
	// bankruptcyPrice: the price where margin = 0
	// Long: bankruptcyPrice = entryPrice * (1 - 1/leverage)
	// Short: bankruptcyPrice = entryPrice * (1 + 1/leverage)
	lev := decimal.NewFromInt(int64(leverage))
	one := decimal.NewFromInt(1)
	invLev := one.Div(lev)

	var bankruptcyPrice decimal.Decimal
	if side == 1 {
		bankruptcyPrice = entryPrice.Mul(one.Sub(invLev))
	} else {
		bankruptcyPrice = entryPrice.Mul(one.Add(invLev))
	}

	// surplus = (liqPrice - bankruptcyPrice) * qty * side
	surplus := liqPrice.Sub(bankruptcyPrice).Mul(quantity).Mul(decimal.NewFromInt(int64(side)))
	return surplus
}

// LiquidationResult describes the result of processing a liquidation through the fund.
type LiquidationResult struct {
	Surplus     decimal.Decimal // positive = fund gained, negative = deficit
	FundCovered decimal.Decimal // amount the fund covered (if deficit)
	NeedADL     bool            // true if ADL should be triggered
	ADLDeficit  decimal.Decimal // remaining deficit ADL needs to cover
}

// ProcessLiquidation handles the full liquidation settlement with the insurance fund.
func (f *Fund) ProcessLiquidation(entryPrice, liqPrice decimal.Decimal, quantity decimal.Decimal, side int, leverage int) *LiquidationResult {
	surplus := CalcLiquidationSurplus(entryPrice, liqPrice, quantity, side, leverage)

	result := &LiquidationResult{
		Surplus: surplus,
	}

	if surplus.IsPositive() {
		// Liquidation at a price better than bankruptcy → surplus to fund
		f.Contribute(surplus)
	} else if surplus.IsNegative() {
		// Bankruptcy scenario → fund covers the deficit
		deficit := surplus.Neg()
		covered, needADL := f.Cover(deficit)
		result.FundCovered = covered
		result.NeedADL = needADL
		if needADL {
			result.ADLDeficit = deficit.Sub(covered)
		}
	}

	return result
}
