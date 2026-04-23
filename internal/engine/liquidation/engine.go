package liquidation

// The liquidation and force-TP logic is integrated into trading/monitor.go
// This package provides the pure calculation functions for testing.

import (
	"github.com/shopspring/decimal"

	"learn_future/internal/engine/position"
)

// CheckLiquidation returns true if the position should be liquidated.
func CheckLiquidation(entryPrice, currentPrice, quantity, margin decimal.Decimal, side int, maintenanceRate decimal.Decimal) bool {
	upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, quantity, side)
	marginRatio := position.CalcMarginRatio(margin, upnl, quantity, currentPrice)
	return marginRatio.LessThanOrEqual(maintenanceRate)
}

// CheckForceTakeProfit returns true if the position hits the force TP threshold.
func CheckForceTakeProfit(entryPrice, currentPrice, margin decimal.Decimal, quantity decimal.Decimal, side int, forceTpROI decimal.Decimal) bool {
	upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, quantity, side)
	roi := position.CalcROI(upnl, margin)
	return roi.GreaterThanOrEqual(forceTpROI.Mul(decimal.NewFromInt(100))) // forceTpROI=5 means 500%
}

// ShouldLiquidateAtPrice checks if a specific price triggers liquidation for a given side.
func ShouldLiquidateAtPrice(currentPrice, liqPrice decimal.Decimal, side int) bool {
	if side == 1 { // long
		return currentPrice.LessThanOrEqual(liqPrice)
	}
	// short
	return currentPrice.GreaterThanOrEqual(liqPrice)
}

// ShouldForceTPAtPrice checks if a specific price triggers force TP for a given side.
func ShouldForceTPAtPrice(currentPrice, forceTpPrice decimal.Decimal, side int) bool {
	if side == 1 { // long
		return currentPrice.GreaterThanOrEqual(forceTpPrice)
	}
	// short
	return currentPrice.LessThanOrEqual(forceTpPrice)
}
