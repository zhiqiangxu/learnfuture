package position

import (
	"github.com/shopspring/decimal"
)

var (
	Hundred = decimal.NewFromInt(100)
)

// CalcUnrealizedPnL calculates unrealized PnL.
// formula: (currentPrice - entryPrice) * quantity * side
func CalcUnrealizedPnL(entryPrice, currentPrice, quantity decimal.Decimal, side int) decimal.Decimal {
	return currentPrice.Sub(entryPrice).Mul(quantity).Mul(decimal.NewFromInt(int64(side)))
}

// CalcROI calculates return on investment.
// formula: unrealizedPnl / margin * 100
func CalcROI(unrealizedPnl, margin decimal.Decimal) decimal.Decimal {
	if margin.IsZero() {
		return decimal.Zero
	}
	return unrealizedPnl.Div(margin).Mul(Hundred)
}

// CalcMarginRatio calculates the margin ratio.
// formula: (margin + unrealizedPnl) / (quantity * currentPrice)
func CalcMarginRatio(margin, unrealizedPnl, quantity, currentPrice decimal.Decimal) decimal.Decimal {
	positionValue := quantity.Mul(currentPrice)
	if positionValue.IsZero() {
		return decimal.Zero
	}
	return margin.Add(unrealizedPnl).Div(positionValue)
}

// CalcBankruptcyPrice calculates the price where margin = 0 (total loss).
// Long: entryPrice * (1 - 1/leverage)
// Short: entryPrice * (1 + 1/leverage)
func CalcBankruptcyPrice(entryPrice decimal.Decimal, leverage int, side int) decimal.Decimal {
	lev := decimal.NewFromInt(int64(leverage))
	invLev := decimal.NewFromInt(1).Div(lev)
	if side == 1 { // long
		return entryPrice.Mul(decimal.NewFromInt(1).Sub(invLev))
	}
	return entryPrice.Mul(decimal.NewFromInt(1).Add(invLev))
}

// CalcLiquidationPrice calculates the liquidation price (where margin ratio = maintenance margin rate).
//
// Long:  liqPrice = entryPrice×(1-1/L)/(1-r)
// Short: liqPrice = entryPrice×(1+1/L)/(1+r)
func CalcLiquidationPrice(entryPrice decimal.Decimal, leverage int, side int, maintenanceRate decimal.Decimal) decimal.Decimal {
	return CalcLiquidationPriceWithFunding(entryPrice, leverage, side, maintenanceRate, decimal.Zero, decimal.Zero)
}

// CalcLiquidationPriceWithFunding includes accumulated funding fee in the calculation.
func CalcLiquidationPriceWithFunding(entryPrice decimal.Decimal, leverage int, side int, maintenanceRate, fundingPnl, quantity decimal.Decimal) decimal.Decimal {
	lev := decimal.NewFromInt(int64(leverage))
	one := decimal.NewFromInt(1)
	invLev := one.Div(lev)

	if side == 1 { // long
		base := entryPrice.Mul(one.Sub(invLev)).Div(one.Sub(maintenanceRate))
		if quantity.IsPositive() && !fundingPnl.IsZero() {
			adjustment := fundingPnl.Div(quantity.Mul(one.Sub(maintenanceRate)))
			base = base.Sub(adjustment)
		}
		return base
	}
	// short
	base := entryPrice.Mul(one.Add(invLev)).Div(one.Add(maintenanceRate))
	if quantity.IsPositive() && !fundingPnl.IsZero() {
		adjustment := fundingPnl.Div(quantity.Mul(one.Add(maintenanceRate)))
		base = base.Sub(adjustment)
	}
	return base
}

// CalcForceTpPrice calculates the forced take-profit price.
// Long: entryPrice * (1 + forceTpROI/leverage)
// Short: entryPrice * (1 - forceTpROI/leverage)
func CalcForceTpPrice(entryPrice decimal.Decimal, leverage int, side int, forceTpROI decimal.Decimal) decimal.Decimal {
	lev := decimal.NewFromInt(int64(leverage))
	one := decimal.NewFromInt(1)
	roiOverLev := forceTpROI.Div(lev)

	if side == 1 { // long
		return entryPrice.Mul(one.Add(roiOverLev))
	}
	// short
	return entryPrice.Mul(one.Sub(roiOverLev))
}

// CalcQuantity calculates the BTC quantity for a given margin and leverage.
// formula: margin * leverage / price
func CalcQuantity(margin decimal.Decimal, leverage int, price decimal.Decimal) decimal.Decimal {
	if price.IsZero() {
		return decimal.Zero
	}
	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	return positionValue.Div(price)
}

// CalcFundingPayment calculates funding fee payment.
// formula: positionValue * rate * (-side)
// rate > 0: longs pay shorts; rate < 0: shorts pay longs
func CalcFundingPayment(quantity, currentPrice, rate decimal.Decimal, side int) decimal.Decimal {
	positionValue := quantity.Mul(currentPrice)
	return positionValue.Mul(rate).Mul(decimal.NewFromInt(int64(-side)))
}
