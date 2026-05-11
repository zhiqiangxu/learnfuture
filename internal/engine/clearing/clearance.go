package clearing

import (
	"github.com/shopspring/decimal"

	"learn_future/internal/engine/fee"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/engine/position"
)

// Clearance handles the clearing (算账) layer between matching and settlement.
//
// Real exchange architecture:
//   撮合 (Matching)  → 产出成交记录 (trades)
//   清算 (Clearance) → 计算保证金/手续费/盈亏/强平价/仓位变更
//   结算 (Settlement)→ 写DB持久化
//
// Clearance is pure calculation, no IO, no side effects on DB.

type Clearance struct {
	feeCalc    *fee.Calculator
	feeRate    decimal.Decimal
	maintRate  decimal.Decimal
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

// --- Position Update Clearing (used by Engine.UpdatePosition) ---

// OpenResult holds clearing results for opening a new position.
type OpenResult struct {
	Margin       decimal.Decimal
	Fee          decimal.Decimal
	TotalCost    decimal.Decimal // margin + fee
	LiqPrice     decimal.Decimal
	ForceTpPrice decimal.Decimal
}

// CalcOpen calculates margin, fee, liq price, and force TP price for a new position.
func (c *Clearance) CalcOpen(price, qty decimal.Decimal, leverage int, side int, isMaker bool) *OpenResult {
	positionValue := price.Mul(qty)
	margin := positionValue.Div(decimal.NewFromInt(int64(leverage)))
	fee, _ := c.calcFee(positionValue, isMaker)

	return &OpenResult{
		Margin:       margin,
		Fee:          fee,
		TotalCost:    margin.Add(fee),
		LiqPrice:     position.CalcLiquidationPrice(price, leverage, side, c.maintRate),
		ForceTpPrice: position.CalcForceTpPrice(price, leverage, side, c.forceTpROI),
	}
}

// IncreaseResult holds clearing results for adding to an existing position.
type IncreaseResult struct {
	AddMargin    decimal.Decimal
	NewEntry     decimal.Decimal
	NewQty       decimal.Decimal
	NewMargin    decimal.Decimal
	LiqPrice     decimal.Decimal
	ForceTpPrice decimal.Decimal
}

// CalcIncrease calculates the new weighted-average entry, margin, and risk prices
// when adding qty at price to an existing position.
func (c *Clearance) CalcIncrease(oldEntry, oldQty, oldMargin decimal.Decimal, leverage, side int, addQty, addPrice decimal.Decimal) *IncreaseResult {
	newQty := oldQty.Add(addQty)
	newEntry := oldEntry.Mul(oldQty).Add(addPrice.Mul(addQty)).Div(newQty)
	addMargin := addPrice.Mul(addQty).Div(decimal.NewFromInt(int64(leverage)))
	newMargin := oldMargin.Add(addMargin)

	return &IncreaseResult{
		AddMargin:    addMargin,
		NewEntry:     newEntry,
		NewQty:       newQty,
		NewMargin:    newMargin,
		LiqPrice:     position.CalcLiquidationPrice(newEntry, leverage, side, c.maintRate),
		ForceTpPrice: position.CalcForceTpPrice(newEntry, leverage, side, c.forceTpROI),
	}
}

// ReduceResult holds clearing results for reducing/closing a position.
type ReduceResult struct {
	CloseQty     decimal.Decimal
	CloseMargin  decimal.Decimal
	CloseFunding decimal.Decimal
	Fee          decimal.Decimal
	RawPnl       decimal.Decimal
	RealizedPnl  decimal.Decimal // rawPnl - fee
	NetPnl       decimal.Decimal // realizedPnl + funding
	IsFullClose  bool
	// Remaining position values (only meaningful if !IsFullClose)
	RemainQty      decimal.Decimal
	RemainMargin   decimal.Decimal
	RemainFunding  decimal.Decimal
	RemainLiqPrice decimal.Decimal
	RemainFtpPrice decimal.Decimal
}

// CalcReduce calculates PnL, fees, and remaining position values for reducing/closing.
func (c *Clearance) CalcReduce(entryPrice, closePrice, existingQty, existingMargin, fundingPnl decimal.Decimal, leverage, side int, tradeQty decimal.Decimal) *ReduceResult {
	closeQty := decimal.Min(tradeQty, existingQty)
	ratio := closeQty.Div(existingQty)
	closeMargin := existingMargin.Mul(ratio)
	closeFunding := fundingPnl.Mul(ratio)

	rawPnl := position.CalcUnrealizedPnL(entryPrice, closePrice, closeQty, side)
	posValue := closeQty.Mul(closePrice)
	closeFee, _ := c.calcFee(posValue, false) // close = taker
	realizedPnl := rawPnl.Sub(closeFee)
	netPnl := realizedPnl.Add(closeFunding)

	isFullClose := closeQty.Equal(existingQty)

	r := &ReduceResult{
		CloseQty:     closeQty,
		CloseMargin:  closeMargin,
		CloseFunding: closeFunding,
		Fee:          closeFee,
		RawPnl:       rawPnl,
		RealizedPnl:  realizedPnl,
		NetPnl:       netPnl,
		IsFullClose:  isFullClose,
	}

	if !isFullClose {
		r.RemainQty = existingQty.Sub(closeQty)
		r.RemainMargin = existingMargin.Sub(closeMargin)
		r.RemainFunding = fundingPnl.Sub(closeFunding)
		r.RemainLiqPrice = position.CalcLiquidationPrice(entryPrice, leverage, side, c.maintRate)
		r.RemainFtpPrice = position.CalcForceTpPrice(entryPrice, leverage, side, c.forceTpROI)
	}

	return r
}

// --- Helpers ---

func (c *Clearance) calcFee(positionValue decimal.Decimal, isMaker bool) (decimal.Decimal, decimal.Decimal) {
	return c.feeCalc.CalcFee(positionValue, decimal.Zero, isMaker)
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

func (c *Clearance) GetMaintRate() decimal.Decimal { return c.maintRate }

func (c *Clearance) CalcMakerFee(positionValue decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	return c.calcFee(positionValue, true)
}

func (c *Clearance) CalcTakerFee(positionValue decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	return c.calcFee(positionValue, false)
}
