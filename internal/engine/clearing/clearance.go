package clearing

import (
	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/fee"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
)

// Clearance handles the clearing (算账) layer between matching and settlement.
//
// Real exchange architecture:
//   撮合 (Matching)  → 产出成交记录 (trades)
//   清算 (Clearance) → 计算保证金/手续费/盈亏/强平价/仓位变更
//   结算 (Settlement)→ 写DB持久化
//
// Clearance is pure calculation, no IO, no side effects on DB.
// It reads from memory caches and produces ClearingResult for settlement.

type Clearance struct {
	feeCalc   *fee.Calculator
	feeRate   decimal.Decimal
	maintRate decimal.Decimal
	forceTpROI decimal.Decimal
}

func NewClearance(feeCalc *fee.Calculator, feeRate, maintRate, forceTpROI decimal.Decimal) *Clearance {
	return &Clearance{
		feeCalc:    feeCalc,
		feeRate:    feeRate,
		maintRate:  maintRate,
		forceTpROI: forceTpROI,
	}
}

// --- Open Position Clearing ---

type OpenClearingInput struct {
	UserID     int64
	Side       int
	Leverage   int
	MarginMode int
	Margin     decimal.Decimal
	Trades     []*orderbook.Trade // orderbook fill results
	TakeProfit *decimal.Decimal
	StopLoss   *decimal.Decimal
	Existing   *cache.CachedPosition // nil if new position
}

type OpenClearingResult struct {
	AvgPrice     decimal.Decimal
	TotalQty     decimal.Decimal
	Fee          decimal.Decimal
	FeeRate      decimal.Decimal
	IsMaker      bool
	TotalCost    decimal.Decimal // margin + fee
	LiqPrice     decimal.Decimal
	ForceTpPrice decimal.Decimal
	Slippage     decimal.Decimal

	// If merging into existing position
	Merged       bool
	NewEntryPrice decimal.Decimal
	NewQuantity   decimal.Decimal
	NewMargin     decimal.Decimal
	NewLiqPrice   decimal.Decimal
	NewFtpPrice   decimal.Decimal

	// Partial fill
	Unfilled     decimal.Decimal
	RefundAmount decimal.Decimal
	AdjustedMargin decimal.Decimal
	AdjustedFee    decimal.Decimal
}

// ClearOpen calculates all values for opening/adding to a position.
func (c *Clearance) ClearOpen(input *OpenClearingInput) *OpenClearingResult {
	positionValue := input.Margin.Mul(decimal.NewFromInt(int64(input.Leverage)))

	// Fee calculation (Maker vs Taker)
	isMaker := false
	tradingFee, feeRate := c.calcFee(positionValue, isMaker)

	totalCost := input.Margin.Add(tradingFee)

	// VWAP from orderbook trades
	avgPrice, totalFilledQty := CalcVWAP(input.Trades)

	// Estimate original quantity for unfilled calc
	if len(input.Trades) > 0 {
		// Use first trade price as reference for estimated qty
	}

	// Partial fill handling
	estimatedQty := position.CalcQuantity(input.Margin, input.Leverage, avgPrice)
	unfilled := estimatedQty.Sub(totalFilledQty)
	if unfilled.IsNegative() {
		unfilled = decimal.Zero
	}

	adjustedMargin := input.Margin
	adjustedFee := tradingFee
	refundAmount := decimal.Zero
	if unfilled.IsPositive() && estimatedQty.IsPositive() {
		refundRatio := unfilled.Div(estimatedQty)
		refundAmount = totalCost.Mul(refundRatio)
		adjustedMargin = input.Margin.Mul(decimal.NewFromInt(1).Sub(refundRatio))
		adjustedFee = tradingFee.Mul(decimal.NewFromInt(1).Sub(refundRatio))
	}

	// Slippage
	var slippage decimal.Decimal
	if len(input.Trades) > 0 {
		bestPrice := input.Trades[0].Price
		if !bestPrice.IsZero() {
			slippage = avgPrice.Sub(bestPrice).Abs().Div(bestPrice).Mul(decimal.NewFromInt(100))
		}
	}

	// Liquidation & force TP prices
	liqPrice := position.CalcLiquidationPrice(avgPrice, input.Leverage, input.Side, c.maintRate)
	ftpPrice := position.CalcForceTpPrice(avgPrice, input.Leverage, input.Side, c.forceTpROI)

	result := &OpenClearingResult{
		AvgPrice:       avgPrice,
		TotalQty:       totalFilledQty,
		Fee:            adjustedFee,
		FeeRate:        feeRate,
		IsMaker:        isMaker,
		TotalCost:      totalCost,
		LiqPrice:       liqPrice,
		ForceTpPrice:   ftpPrice,
		Slippage:       slippage,
		Unfilled:       unfilled,
		RefundAmount:   refundAmount,
		AdjustedMargin: adjustedMargin,
		AdjustedFee:    adjustedFee,
	}

	// Position merge calculation
	if input.Existing != nil && input.Existing.Leverage == input.Leverage {
		oldEntry, _ := decimal.NewFromString(input.Existing.EntryPrice)
		oldQty, _ := decimal.NewFromString(input.Existing.Quantity)
		oldMargin, _ := decimal.NewFromString(input.Existing.Margin)

		result.Merged = true
		result.NewQuantity = oldQty.Add(totalFilledQty)
		result.NewEntryPrice = oldEntry.Mul(oldQty).Add(avgPrice.Mul(totalFilledQty)).Div(result.NewQuantity)
		result.NewMargin = oldMargin.Add(adjustedMargin)
		result.NewLiqPrice = position.CalcLiquidationPrice(result.NewEntryPrice, input.Leverage, input.Side, c.maintRate)
		result.NewFtpPrice = position.CalcForceTpPrice(result.NewEntryPrice, input.Leverage, input.Side, c.forceTpROI)
	}

	return result
}

// --- Close Position Clearing ---

type CloseClearingInput struct {
	Position  *model.Position
	ClosePrice decimal.Decimal
	CloseQty   decimal.Decimal // zero = close all
	Reason     int
}

type CloseClearingResult struct {
	IsPartial    bool
	ClosedQty    decimal.Decimal
	RemainingQty decimal.Decimal
	CloseRatio   decimal.Decimal
	CloseMargin  decimal.Decimal
	CloseValue   decimal.Decimal // qty × closePrice (actual close value for fee calc)
	CloseFundingPnl decimal.Decimal
	CloseFee     decimal.Decimal
	RawPnl       decimal.Decimal // price diff pnl before fee
	RealizedPnl  decimal.Decimal // rawPnl - fee
	NetPnl       decimal.Decimal // realizedPnl + fundingPnl
	ClosePrice   decimal.Decimal

	// For partial close: remaining position values
	RemainMargin decimal.Decimal
	RemainLiqPrice decimal.Decimal
	RemainFtpPrice decimal.Decimal
}

// ClearClose calculates all values for closing a position.
func (c *Clearance) ClearClose(input *CloseClearingInput) *CloseClearingResult {
	pos := input.Position

	closedQty := pos.Quantity
	if !input.CloseQty.IsZero() && input.CloseQty.LessThan(pos.Quantity) {
		closedQty = input.CloseQty
	}

	isPartial := closedQty.LessThan(pos.Quantity)
	remainingQty := pos.Quantity.Sub(closedQty)

	closeRatio := closedQty.Div(pos.Quantity)
	closeMargin := pos.Margin.Mul(closeRatio)
	closeFundingPnl := pos.FundingPnl.Mul(closeRatio)

	// Fee based on actual close value (qty × closePrice), not opening value
	closeValue := closedQty.Mul(input.ClosePrice)
	closeFee, _ := c.calcFee(closeValue, false) // close = Taker

	rawPnl := position.CalcUnrealizedPnL(pos.EntryPrice, input.ClosePrice, closedQty, pos.Side)
	realizedPnl := rawPnl.Sub(closeFee)
	netPnl := realizedPnl.Add(closeFundingPnl)

	result := &CloseClearingResult{
		IsPartial:       isPartial,
		ClosedQty:       closedQty,
		RemainingQty:    remainingQty,
		CloseRatio:      closeRatio,
		CloseMargin:     closeMargin,
		CloseValue:      closeValue,
		CloseFundingPnl: closeFundingPnl,
		CloseFee:        closeFee,
		RawPnl:          rawPnl,
		RealizedPnl:     realizedPnl,
		NetPnl:          netPnl,
		ClosePrice:      input.ClosePrice,
	}

	if isPartial {
		result.RemainMargin = pos.Margin.Sub(closeMargin)
		result.RemainLiqPrice = position.CalcLiquidationPrice(pos.EntryPrice, pos.Leverage, pos.Side, c.maintRate)
		result.RemainFtpPrice = position.CalcForceTpPrice(pos.EntryPrice, pos.Leverage, pos.Side, c.forceTpROI)
	}

	return result
}

// --- Funding Clearing ---

type FundingClearingInput struct {
	Quantity     decimal.Decimal
	CurrentPrice decimal.Decimal
	Rate         decimal.Decimal
	Side         int
}

type FundingClearingResult struct {
	PositionValue decimal.Decimal
	Payment       decimal.Decimal
}

// ClearFunding calculates funding fee payment for a position.
func (c *Clearance) ClearFunding(input *FundingClearingInput) *FundingClearingResult {
	posValue := input.Quantity.Mul(input.CurrentPrice)
	payment := position.CalcFundingPayment(input.Quantity, input.CurrentPrice, input.Rate, input.Side)
	return &FundingClearingResult{
		PositionValue: posValue,
		Payment:       payment,
	}
}

// --- Helpers ---

func (c *Clearance) calcFee(positionValue decimal.Decimal, isMaker bool) (decimal.Decimal, decimal.Decimal) {
	if c.feeCalc != nil {
		return c.feeCalc.CalcFee(positionValue, decimal.Zero, isMaker)
	}
	return positionValue.Mul(c.feeRate), c.feeRate
}

// CalcVWAP calculates volume-weighted average price from orderbook trades.
func CalcVWAP(trades []*orderbook.Trade) (avgPrice, totalQty decimal.Decimal) {
	if len(trades) == 0 {
		return decimal.Zero, decimal.Zero
	}
	totalValue := decimal.Zero
	for _, t := range trades {
		totalValue = totalValue.Add(t.Price.Mul(t.Quantity))
		totalQty = totalQty.Add(t.Quantity)
	}
	if totalQty.IsZero() {
		return decimal.Zero, decimal.Zero
	}
	avgPrice = totalValue.Div(totalQty)
	return avgPrice, totalQty
}

// CalcLiqPrice re-exports for external use.
func (c *Clearance) CalcLiqPrice(entryPrice decimal.Decimal, leverage, side int) decimal.Decimal {
	return position.CalcLiquidationPrice(entryPrice, leverage, side, c.maintRate)
}

func (c *Clearance) CalcForceTpPrice(entryPrice decimal.Decimal, leverage, side int) decimal.Decimal {
	return position.CalcForceTpPrice(entryPrice, leverage, side, c.forceTpROI)
}

func (c *Clearance) GetMaintRate() decimal.Decimal  { return c.maintRate }
func (c *Clearance) GetForceTpROI() decimal.Decimal  { return c.forceTpROI }
func (c *Clearance) GetFeeRate() decimal.Decimal     { return c.feeRate }

func (c *Clearance) CalcMakerFee(positionValue decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	return c.calcFee(positionValue, true)
}

func (c *Clearance) CalcTakerFee(positionValue decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	return c.calcFee(positionValue, false)
}
