package adl

import (
	"sort"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/position"
)

// Auto-Deleveraging (ADL) Engine
//
// ADL is triggered when:
// 1. A position is liquidated
// 2. The insurance fund cannot cover the liquidation deficit
// 3. The system must forcibly reduce opposing positions to cover the loss
//
// ADL priority ranking:
// - Opposing positions are ranked by profitability (PnL ratio)
// - Most profitable positions are deleveraged first
// - This is fair because highly profitable positions benefit most from the
//   market movement that caused the liquidation
//
// In our simulation, ADL is triggered by the "force take-profit" mechanism
// (收益率 >= 500%), which represents the LP's risk limit.

// RankedPosition represents a position with its ADL priority score.
type RankedPosition struct {
	Position *cache.CachedPosition
	PnlRatio decimal.Decimal // unrealized PnL / margin
	Score    decimal.Decimal // ADL priority score
}

// RankPositions ranks opposing positions by ADL priority.
// Side: the side being reduced (opposite to the liquidated side).
// currentPrice: the current market price.
// Returns positions sorted by ADL priority (highest first = first to be reduced).
func RankPositions(positions []*cache.CachedPosition, targetSide int, currentPrice decimal.Decimal) []*RankedPosition {
	var ranked []*RankedPosition

	for _, pos := range positions {
		if pos.Side != targetSide {
			continue
		}

		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)

		upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, quantity, pos.Side)
		pnlRatio := position.CalcROI(upnl, margin)

		// ADL score = PnL ratio * leverage factor
		// Higher leverage + higher profit = higher priority
		leverageFactor := decimal.NewFromInt(int64(pos.Leverage))
		score := pnlRatio.Mul(leverageFactor)

		if upnl.IsPositive() { // Only profitable positions can be ADL'd
			ranked = append(ranked, &RankedPosition{
				Position: pos,
				PnlRatio: pnlRatio,
				Score:    score,
			})
		}
	}

	// Sort by score descending (most profitable first)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score.GreaterThan(ranked[j].Score)
	})

	return ranked
}

// CalcADLQuantity determines how much quantity needs to be reduced from
// the target position to cover the given deficit.
// deficit: the uncovered loss from the insurance fund
// currentPrice: current market price
// Returns the quantity to reduce.
func CalcADLQuantity(deficit, currentPrice decimal.Decimal) decimal.Decimal {
	if currentPrice.IsZero() {
		return decimal.Zero
	}
	// quantity = deficit / currentPrice
	return deficit.Div(currentPrice)
}

// ADLResult describes the outcome of an ADL operation on a single position.
type ADLResult struct {
	PositionID     int64
	UserID         int64
	ReducedQty     decimal.Decimal
	ReducedMargin  decimal.Decimal
	RealizedPnl    decimal.Decimal
	RemainingQty   decimal.Decimal
	FullyClosed    bool
}

// ExecuteADL simulates the ADL reduction on ranked positions.
// It reduces positions starting from the highest priority until the deficit is covered.
// Returns the list of ADL results and any remaining uncovered deficit.
func ExecuteADL(ranked []*RankedPosition, deficit, currentPrice decimal.Decimal) (results []*ADLResult, remainingDeficit decimal.Decimal) {
	remainingDeficit = deficit

	for _, rp := range ranked {
		if remainingDeficit.IsZero() || remainingDeficit.IsNegative() {
			break
		}

		pos := rp.Position
		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)

		// How much of this position needs to be reduced
		neededQty := CalcADLQuantity(remainingDeficit, currentPrice)

		reducedQty := neededQty
		fullyClosed := false
		if neededQty.GreaterThanOrEqual(quantity) {
			reducedQty = quantity
			fullyClosed = true
		}

		// Calculate the margin and PnL for the reduced portion
		ratio := reducedQty.Div(quantity)
		reducedMargin := margin.Mul(ratio)

		// PnL for the reduced portion
		upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, reducedQty, pos.Side)

		// The deficit covered by this reduction
		coveredAmount := reducedQty.Mul(currentPrice)
		if coveredAmount.GreaterThan(remainingDeficit) {
			coveredAmount = remainingDeficit
		}
		remainingDeficit = remainingDeficit.Sub(coveredAmount)

		results = append(results, &ADLResult{
			PositionID:   pos.ID,
			UserID:       pos.UserID,
			ReducedQty:   reducedQty,
			ReducedMargin: reducedMargin,
			RealizedPnl:  upnl,
			RemainingQty: quantity.Sub(reducedQty),
			FullyClosed:  fullyClosed,
		})
	}

	return results, remainingDeficit
}

// GetADLIndicator returns the ADL indicator level (1-5) for a position.
// This tells the user how likely their position is to be ADL'd.
// 5 = highest risk (most profitable, will be reduced first)
// 1 = lowest risk
func GetADLIndicator(pnlRatio decimal.Decimal, totalPositions int) int {
	if totalPositions == 0 {
		return 1
	}

	// Simple mapping based on PnL ratio
	hundred := decimal.NewFromInt(100)
	if pnlRatio.GreaterThan(hundred.Mul(decimal.NewFromInt(4))) { // > 400%
		return 5
	}
	if pnlRatio.GreaterThan(hundred.Mul(decimal.NewFromInt(3))) { // > 300%
		return 4
	}
	if pnlRatio.GreaterThan(hundred.Mul(decimal.NewFromInt(2))) { // > 200%
		return 3
	}
	if pnlRatio.GreaterThan(hundred) { // > 100%
		return 2
	}
	return 1
}
