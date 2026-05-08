package trading

import (
	"log"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/clearing"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
)

// PositionUpdateResult describes what happened when a position was updated.
type PositionUpdateResult struct {
	Action       string          // "open", "increase", "reduce", "close"
	PositionID   int64
	Side         int
	EntryPrice   decimal.Decimal
	Quantity     decimal.Decimal
	Margin       decimal.Decimal
	Fee          decimal.Decimal
	LiqPrice     decimal.Decimal
	ForceTpPrice decimal.Decimal
	// Close-specific fields
	RawPnl      decimal.Decimal
	RealizedPnl decimal.Decimal
	FundingPnl  decimal.Decimal
	NetPnl      decimal.Decimal
	ClosedQty   decimal.Decimal
	RemainingQty decimal.Decimal
	IsPartial   bool
}

// UpdatePosition is the unified position update function (exported for Monitor/ADL).
// It handles all cases: new open, increase, reduce, close, and flip.
// Both the taker and maker side of every trade go through this same function.
func (e *Engine) UpdatePosition(userID int64, tradeSide int, qty, price decimal.Decimal, leverage int, isMaker bool, closeReason int) *PositionUpdateResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find existing position in the opposite direction (would be reduced/closed)
	opposite := e.positionCache.FindByUserSide(userID, -tradeSide)
	// Find existing position in the same direction (would be increased)
	sameDir := e.positionCache.FindByUserSide(userID, tradeSide)

	if opposite != nil {
		return e.handleReduce(userID, opposite, tradeSide, qty, price, leverage, closeReason)
	} else if sameDir != nil {
		return e.handleIncrease(userID, sameDir, qty, price)
	} else {
		return e.handleOpen(userID, tradeSide, qty, price, leverage, isMaker)
	}
}

// handleOpen creates a new position.
func (e *Engine) handleOpen(userID int64, side int, qty, price decimal.Decimal, leverage int, isMaker bool) *PositionUpdateResult {
	if leverage <= 0 {
		leverage = 1
	}

	positionValue := price.Mul(qty)
	margin := positionValue.Div(decimal.NewFromInt(int64(leverage)))

	var fee decimal.Decimal
	if isMaker {
		fee, _ = e.clearance.CalcMakerFee(positionValue)
	} else {
		fee, _ = e.clearance.CalcTakerFee(positionValue)
	}
	totalCost := margin.Add(fee)

	if !e.memAccounts.Deduct(userID, totalCost) {
		log.Printf("[UpdatePosition] user %d insufficient balance for open, need=%s", userID, totalCost)
		return nil
	}

	liqPrice := e.clearance.CalcLiqPrice(price, leverage, side)
	ftpPrice := e.clearance.CalcForceTpPrice(price, leverage, side)

	e.nextPosID++
	e.positionCache.Add(&cache.CachedPosition{
		ID: e.nextPosID, UserID: userID, Side: side,
		MarginMode: model.MarginModeIsolated, Leverage: leverage,
		EntryPrice: price.String(), Quantity: qty.String(),
		Margin: margin.String(), LiqPrice: liqPrice.String(),
		ForceTpPrice: ftpPrice.String(),
	})

	// Async DB persistence
	pos := &model.Position{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		MarginMode: model.MarginModeIsolated, Leverage: leverage,
		EntryPrice: price, Quantity: qty, Margin: margin,
		LiqPrice: liqPrice, ForceTpPrice: ftpPrice,
		Status: model.PositionStatusActive,
	}
	order := &model.Order{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		OrderType: model.OrderTypeMarket, Leverage: leverage,
		Quantity: qty, MarginCost: margin, Status: model.OrderStatusFilled,
	}
	order.FilledPrice = &price
	trade := &model.Trade{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		Price: price, Quantity: qty, Fee: fee, IsClose: false,
	}
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventOpenPosition, Order: order, Position: pos, Trade: trade,
	})
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventBalanceUpdate, UserID: userID,
		BalanceDelta: totalCost.Neg(), FrozenDelta: decimal.Zero,
	})

	log.Printf("[UpdatePosition] user %d opened %s %s @ %s, margin=%s",
		userID, sideStr(side), qty, price.StringFixed(2), margin.StringFixed(2))

	return &PositionUpdateResult{
		Action: "open", PositionID: e.nextPosID, Side: side,
		EntryPrice: price, Quantity: qty, Margin: margin, Fee: fee,
		LiqPrice: liqPrice, ForceTpPrice: ftpPrice,
	}
}

// handleIncrease adds to an existing same-direction position (weighted average entry price).
func (e *Engine) handleIncrease(userID int64, existing *cache.CachedPosition, qty, price decimal.Decimal) *PositionUpdateResult {
	oldQty, _ := decimal.NewFromString(existing.Quantity)
	oldEntry, _ := decimal.NewFromString(existing.EntryPrice)
	oldMargin, _ := decimal.NewFromString(existing.Margin)

	newQty := oldQty.Add(qty)
	newEntry := oldEntry.Mul(oldQty).Add(price.Mul(qty)).Div(newQty)
	addMargin := price.Mul(qty).Div(decimal.NewFromInt(int64(existing.Leverage)))
	newMargin := oldMargin.Add(addMargin)

	if !e.memAccounts.Deduct(userID, addMargin) {
		log.Printf("[UpdatePosition] user %d insufficient balance for increase, need=%s", userID, addMargin)
		return nil
	}

	newLiqPrice := e.clearance.CalcLiqPrice(newEntry, existing.Leverage, existing.Side)
	newFtpPrice := e.clearance.CalcForceTpPrice(newEntry, existing.Leverage, existing.Side)

	e.positionCache.Update(existing.ID, func(cp *cache.CachedPosition) {
		cp.EntryPrice = newEntry.String()
		cp.Quantity = newQty.String()
		cp.Margin = newMargin.String()
		cp.LiqPrice = newLiqPrice.String()
		cp.ForceTpPrice = newFtpPrice.String()
	})

	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventBalanceUpdate, UserID: userID,
		BalanceDelta: addMargin.Neg(), FrozenDelta: decimal.Zero,
	})

	log.Printf("[UpdatePosition] user %d increased position %d, qty=%s→%s, entry=%s→%s",
		userID, existing.ID, oldQty, newQty, oldEntry.StringFixed(2), newEntry.StringFixed(2))

	return &PositionUpdateResult{
		Action: "increase", PositionID: existing.ID, Side: existing.Side,
		EntryPrice: newEntry, Quantity: newQty, Margin: newMargin,
		LiqPrice: newLiqPrice, ForceTpPrice: newFtpPrice,
	}
}

// handleReduce reduces or closes an opposite-direction position, and opens remainder if qty exceeds.
func (e *Engine) handleReduce(userID int64, existing *cache.CachedPosition, tradeSide int, qty, price decimal.Decimal, leverage int, closeReason int) *PositionUpdateResult {
	existingQty, _ := decimal.NewFromString(existing.Quantity)
	existingEntry, _ := decimal.NewFromString(existing.EntryPrice)
	existingMargin, _ := decimal.NewFromString(existing.Margin)
	fundingPnl, _ := decimal.NewFromString(existing.FundingPnl)

	closeQty := decimal.Min(qty, existingQty)
	ratio := closeQty.Div(existingQty)
	closeMargin := existingMargin.Mul(ratio)
	closeFunding := fundingPnl.Mul(ratio)

	// Calculate PnL
	rawPnl := position.CalcUnrealizedPnL(existingEntry, price, closeQty, existing.Side)
	posValue := closeQty.Mul(price)
	closeFee, _ := e.clearance.CalcTakerFee(posValue)
	realizedPnl := rawPnl.Sub(closeFee)
	netPnl := realizedPnl.Add(closeFunding)

	// Return margin + PnL to account
	if closeReason == model.CloseReasonLiquidation {
		e.memAccounts.AddPnl(userID, closeMargin.Neg())
	} else {
		e.memAccounts.ReturnMarginWithPnl(userID, closeMargin, netPnl)
	}

	if closeQty.Equal(existingQty) {
		// Full close
		e.positionCache.Remove(existing.ID)
		log.Printf("[UpdatePosition] user %d closed position %d, pnl=%s", userID, existing.ID, netPnl.StringFixed(2))
	} else {
		// Partial close
		remainQty := existingQty.Sub(closeQty)
		remainMargin := existingMargin.Sub(closeMargin)
		remainFunding := fundingPnl.Sub(closeFunding)
		remainLiqPrice := e.clearance.CalcLiqPrice(existingEntry, existing.Leverage, existing.Side)
		remainFtpPrice := e.clearance.CalcForceTpPrice(existingEntry, existing.Leverage, existing.Side)

		e.positionCache.Update(existing.ID, func(cp *cache.CachedPosition) {
			cp.Quantity = remainQty.String()
			cp.Margin = remainMargin.String()
			cp.FundingPnl = remainFunding.String()
			cp.LiqPrice = remainLiqPrice.String()
			cp.ForceTpPrice = remainFtpPrice.String()
			cp.State.Store(cache.PosStateActive)
		})
		log.Printf("[UpdatePosition] user %d reduced position %d by %s, remaining=%s", userID, existing.ID, closeQty, remainQty)
	}

	// Async DB persistence
	if closeReason == 0 {
		closeReason = model.CloseReasonManual
	}
	closeTrade := &model.Trade{
		UserID: userID, Symbol: "BTCUSDT", Side: tradeSide,
		Price: price, Quantity: closeQty,
		Fee: closeFee, RealizedPnl: realizedPnl, IsClose: true, CloseReason: closeReason,
	}
	posID := existing.ID
	closeTrade.PositionID = &posID
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventClosePosition, Trade: closeTrade,
		PositionID: existing.ID, UserID: userID, CloseReason: closeReason,
		ClosePrice: price, Margin: closeMargin, RealizedPnl: realizedPnl,
		Fee: closeFee, NetPnl: netPnl,
	})

	// If qty exceeds existing position, open remainder in new direction
	remainingTradeQty := qty.Sub(existingQty)
	if remainingTradeQty.IsPositive() {
		e.handleOpen(userID, tradeSide, remainingTradeQty, price, leverage, false)
	}

	return &PositionUpdateResult{
		Action: func() string { if closeQty.Equal(existingQty) { return "close" }; return "reduce" }(),
		PositionID: existing.ID, Side: existing.Side,
		EntryPrice: existingEntry, Fee: closeFee,
		RawPnl: rawPnl, RealizedPnl: realizedPnl, FundingPnl: closeFunding, NetPnl: netPnl,
		ClosedQty: closeQty, RemainingQty: existingQty.Sub(closeQty),
		IsPartial: !closeQty.Equal(existingQty),
	}
}

func sideStr(side int) string {
	if side == 1 {
		return "LONG"
	}
	return "SHORT"
}
