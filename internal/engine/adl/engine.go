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
// Only includes positions that are profitable at BOTH market price and bankruptcy price,
// ensuring ADL never forces a counterparty into loss.
func RankPositions(positions []*cache.CachedPosition, targetSide int, currentPrice, bankruptcyPrice decimal.Decimal) []*RankedPosition {
	var ranked []*RankedPosition

	for _, pos := range positions {
		if pos.Side != targetSide {
			continue
		}
		// Skip non-active positions
		if pos.State.Load() != cache.PosStateActive {
			continue
		}

		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)

		// Must be profitable at market price
		upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, quantity, pos.Side)
		if !upnl.IsPositive() {
			continue
		}

		// Must also be profitable at bankruptcy price (settlement won't cause loss)
		upnlAtBankruptcy := position.CalcUnrealizedPnL(entryPrice, bankruptcyPrice, quantity, pos.Side)
		if !upnlAtBankruptcy.IsPositive() {
			continue
		}

		pnlRatio := position.CalcROI(upnl, margin)
		leverageFactor := decimal.NewFromInt(int64(pos.Leverage))
		score := pnlRatio.Mul(leverageFactor)

		ranked = append(ranked, &RankedPosition{
			Position: pos,
			PnlRatio: pnlRatio,
			Score:    score,
		})
	}

	// Sort by score descending (most profitable first)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score.GreaterThan(ranked[j].Score)
	})

	return ranked
}

// ADLResult describes the outcome of an ADL operation on a single position.
type ADLResult struct {
	PositionID      int64
	UserID          int64
	ReducedQty      decimal.Decimal
	ReducedMargin   decimal.Decimal
	RealizedPnl     decimal.Decimal
	SettlementPrice decimal.Decimal // capped price used for settlement
	RemainingQty   decimal.Decimal
	FullyClosed    bool
}

// ExecuteADL reduces opposing positions to cover the deficit from a liquidation.
// Settlement price is capped so counterparties only lose PROFITS, never incur losses.
// currentPrice is needed to calculate profit given up (market price vs settlement price).
func ExecuteADL(ranked []*RankedPosition, deficit, bankruptcyPrice, currentPrice decimal.Decimal) (results []*ADLResult, remainingDeficit decimal.Decimal) {
	remainingDeficit = deficit

	for _, rp := range ranked {
		if remainingDeficit.IsZero() || remainingDeficit.IsNegative() {
			break
		}

		pos := rp.Position
		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)

		// Cap settlement price: never worse than counterparty's entry price.
		//   Long counterparty: settlement price must be >= entry
		//   Short counterparty: settlement price must be <= entry
		settlementPrice := bankruptcyPrice
		if pos.Side == 1 && bankruptcyPrice.LessThan(entryPrice) {
			settlementPrice = entryPrice
		} else if pos.Side == -1 && bankruptcyPrice.GreaterThan(entryPrice) {
			settlementPrice = entryPrice
		}

		// Profit per unit the counterparty gives up = (market PnL - settlement PnL) per unit
		marketPnlPerUnit := currentPrice.Sub(entryPrice).Mul(decimal.NewFromInt(int64(pos.Side)))
		settlePnlPerUnit := settlementPrice.Sub(entryPrice).Mul(decimal.NewFromInt(int64(pos.Side)))
		profitGivenUpPerUnit := marketPnlPerUnit.Sub(settlePnlPerUnit)

		if !profitGivenUpPerUnit.IsPositive() {
			// Settlement is same or better than market — no profit to take
			continue
		}

		// How much quantity to reduce to cover the deficit
		neededQty := remainingDeficit.Div(profitGivenUpPerUnit)

		// Clamp reducedQty: don't reduce more than needed or more than position size
		reducedQty := neededQty
		fullyClosed := false
		if neededQty.GreaterThanOrEqual(quantity) {
			reducedQty = quantity
			fullyClosed = true
		}

		// Ensure coveredAmount doesn't exceed deficit (adjust reducedQty if needed)
		coveredAmount := reducedQty.Mul(profitGivenUpPerUnit)
		if coveredAmount.GreaterThan(remainingDeficit) {
			reducedQty = remainingDeficit.Div(profitGivenUpPerUnit)
			coveredAmount = remainingDeficit
			fullyClosed = false
		}

		ratio := reducedQty.Div(quantity)
		reducedMargin := margin.Mul(ratio)
		realizedPnl := position.CalcUnrealizedPnL(entryPrice, settlementPrice, reducedQty, pos.Side)

		remainingDeficit = remainingDeficit.Sub(coveredAmount)

		results = append(results, &ADLResult{
			PositionID:      pos.ID,
			UserID:          pos.UserID,
			ReducedQty:      reducedQty,
			ReducedMargin:   reducedMargin,
			RealizedPnl:     realizedPnl,
			SettlementPrice: settlementPrice,
			RemainingQty:    quantity.Sub(reducedQty),
			FullyClosed:     fullyClosed,
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
