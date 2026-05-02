package trading

import (
	"log"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/adl"
	"learn_future/internal/engine/insurance"
	"learn_future/internal/engine/markprice"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
	"learn_future/internal/ws"
)

type OrderFillCallback func(userID int64, result *PlaceOrderResult)
type PositionCloseCallback func(userID int64, positionID int64, result *CloseResult, reason int)

// Monitor is the risk engine that runs on every price tick.
// It uses:
// - lastPrice (合约最新价) for limit order triggers, TP/SL, PnL display
// - markPrice (标记价格) for liquidation checks (防操纵)
// - insuranceFund for liquidation surplus/deficit handling
// - ADL when insurance fund is depleted
type Monitor struct {
	engine          *Engine
	positionCache   *cache.PositionCache
	orderCache      *cache.OrderCache
	hub             *ws.Hub
	markPriceEngine *markprice.Engine
	insuranceFund   *insurance.Fund
	onOrderFill     OrderFillCallback
	onPositionClose PositionCloseCallback
}

func NewMonitor(
	engine *Engine,
	positionCache *cache.PositionCache,
	orderCache *cache.OrderCache,
	hub *ws.Hub,
	markPriceEngine *markprice.Engine,
	insuranceFund *insurance.Fund,
) *Monitor {
	return &Monitor{
		engine:          engine,
		positionCache:   positionCache,
		orderCache:      orderCache,
		hub:             hub,
		markPriceEngine: markPriceEngine,
		insuranceFund:   insuranceFund,
	}
}

func (m *Monitor) SetOnOrderFill(cb OrderFillCallback) {
	m.onOrderFill = cb
}

func (m *Monitor) SetOnPositionClose(cb PositionCloseCallback) {
	m.onPositionClose = cb
}

// OnPriceUpdate is the core risk engine loop, called on each price tick.
//
// Real exchange behavior:
// - Limit orders trigger on LAST PRICE (合约最新成交价)
// - TP/SL trigger on LAST PRICE
// - Liquidation checks use MARK PRICE (标记价格, 防操纵)
// - Force TP checks use LAST PRICE (收益基于实际成交价)
// - PnL display uses LAST PRICE
func (m *Monitor) OnPriceUpdate(lastPrice decimal.Decimal) {
	// Get mark price for liquidation checks
	markPrice := lastPrice // fallback
	if m.markPriceEngine != nil {
		mp := m.markPriceEngine.GetMarkPrice()
		if !mp.IsZero() {
			markPrice = mp
		}
	}

	// 1. Check limit orders (use lastPrice)
	m.checkLimitOrders(lastPrice)

	// 2. Check all active positions
	allPositions := m.positionCache.GetAll()

	// Pre-calculate cross margin account equity for cross-margin liquidation check
	// Cross margin: all cross positions share account balance
	// Liquidation when: accountEquity < sum(maintenanceMargin of all cross positions)
	crossMaintenanceTotal := decimal.Zero
	crossUpnlTotal := decimal.Zero
	var crossPositionIDs []int64
	for _, pos := range allPositions {
		if pos.MarginMode == 2 { // cross
			ep, _ := decimal.NewFromString(pos.EntryPrice)
			qty, _ := decimal.NewFromString(pos.Quantity)
			upnl := position.CalcUnrealizedPnL(ep, lastPrice, qty, pos.Side)
			crossUpnlTotal = crossUpnlTotal.Add(upnl)
			mg, _ := decimal.NewFromString(pos.Margin)
			// maintenance margin = position value * maintenance rate
			posValue := mg.Mul(decimal.NewFromInt(int64(pos.Leverage)))
			maintMargin := posValue.Mul(m.engine.GetMaintRate())
			crossMaintenanceTotal = crossMaintenanceTotal.Add(maintMargin)
			crossPositionIDs = append(crossPositionIDs, pos.ID)
		}
	}

	for _, pos := range allPositions {
		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)
		liqPrice, _ := decimal.NewFromString(pos.LiqPrice)
		forceTpPrice, _ := decimal.NewFromString(pos.ForceTpPrice)

		// 2a. Liquidation check — uses MARK PRICE (防操纵)
		if pos.MarginMode == 2 {
			// Cross margin: check account-level equity vs total maintenance margin
			// equity = balance + totalCrossUpnl
			balance := m.engine.GetMemAccounts().GetBalance(pos.UserID)
			accountEquity := balance.Add(crossUpnlTotal)
			if accountEquity.LessThanOrEqual(crossMaintenanceTotal) {
				// Cross margin liquidation: liquidate ALL cross positions
				for _, cpID := range crossPositionIDs {
					cp, ok := m.positionCache.Get(cpID)
					if !ok {
						continue
					}
					cpEntry, _ := decimal.NewFromString(cp.EntryPrice)
					cpQty, _ := decimal.NewFromString(cp.Quantity)
					cpMargin, _ := decimal.NewFromString(cp.Margin)
					m.handleLiquidation(cp, lastPrice, cpEntry, cpQty, cpMargin)
				}
				continue
			}
		} else {
			// Isolated margin: per-position check (original behavior)
			if shouldLiquidate(markPrice, liqPrice, pos.Side) {
				m.handleLiquidation(pos, lastPrice, entryPrice, quantity, margin)
				continue
			}
		}

		// 2b. Force take-profit — uses LAST PRICE
		if shouldTrigger(lastPrice, forceTpPrice, pos.Side, true) {
			m.handleForceTP(pos, lastPrice)
			continue
		}

		// 2c. Take-profit — uses LAST PRICE
		// FIX #8 (OCO): when TP triggers, SL is automatically cancelled (and vice versa)
		if pos.TakeProfit != "" {
			tpPrice, _ := decimal.NewFromString(pos.TakeProfit)
			if shouldTrigger(lastPrice, tpPrice, pos.Side, true) {
				// OCO: clear SL before closing
				m.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
					cp.StopLoss = ""
				})
				m.handleClose(pos, lastPrice, model.CloseReasonTakeProfit)
				continue
			}
		}

		// 2d. Stop-loss — uses LAST PRICE
		if pos.StopLoss != "" {
			slPrice, _ := decimal.NewFromString(pos.StopLoss)
			if shouldTrigger(lastPrice, slPrice, pos.Side, false) {
				// OCO: clear TP before closing
				m.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
					cp.TakeProfit = ""
				})
				m.handleClose(pos, lastPrice, model.CloseReasonStopLoss)
				continue
			}
		}

		// 2e. Push PnL update (use lastPrice for display)
		upnl := position.CalcUnrealizedPnL(entryPrice, lastPrice, quantity, pos.Side)
		marginRatio := position.CalcMarginRatio(margin, upnl, quantity, lastPrice)
		roi := position.CalcROI(upnl, margin)
		adlIndicator := adl.GetADLIndicator(roi, len(allPositions))

		msg, _ := ws.NewMessage("position_pnl", map[string]interface{}{
			"id":            pos.ID,
			"upnl":          upnl.StringFixed(2),
			"margin_ratio":  marginRatio.StringFixed(4),
			"roi":           roi.StringFixed(2),
			"adl_indicator": adlIndicator,
		})
		m.hub.SendToUser(pos.UserID, msg)
	}
}

func (m *Monitor) checkLimitOrders(lastPrice decimal.Decimal) {
	triggered := m.orderCache.GetTriggered(lastPrice)
	for _, order := range triggered {
		result, err := m.engine.FillLimitOrder(order)
		if err != nil {
			log.Printf("[Monitor] fill limit order %d error: %v", order.ID, err)
			continue
		}
		msg, _ := ws.NewMessage("order_filled", map[string]interface{}{
			"order_id": order.ID,
			"price":    order.Price.String(),
			"qty":      order.Quantity.String(),
		})
		m.hub.SendToUser(order.UserID, msg)

		if m.onOrderFill != nil {
			m.onOrderFill(order.UserID, result)
		}
	}
}

// handleLiquidation processes a liquidation with insurance fund integration.
func (m *Monitor) handleLiquidation(pos *cache.CachedPosition, lastPrice, entryPrice, quantity, margin decimal.Decimal) {
	result, err := m.engine.ClosePositionInternal(pos.ID, lastPrice, model.CloseReasonLiquidation)
	if err != nil {
		log.Printf("[Monitor] liquidate position %d error: %v", pos.ID, err)
		return
	}

	// Process through insurance fund
	if m.insuranceFund != nil {
		liqResult := m.insuranceFund.ProcessLiquidation(
			entryPrice, lastPrice, quantity, pos.Side, pos.Leverage,
		)

		if liqResult.NeedADL {
			// Calculate bankruptcy price: price where margin goes to zero
			// Long: bankruptcyPrice = entryPrice - margin/quantity
			// Short: bankruptcyPrice = entryPrice + margin/quantity
			bankruptcyPrice := entryPrice.Sub(margin.Div(quantity).Mul(decimal.NewFromInt(int64(pos.Side))))
			log.Printf("[Monitor] insurance fund depleted, triggering ADL for deficit=%s bankruptcyPrice=%s", liqResult.ADLDeficit, bankruptcyPrice)
			m.triggerADL(pos.Side, liqResult.ADLDeficit, bankruptcyPrice, lastPrice)
		}

		log.Printf("[Monitor] liquidation pos=%d surplus=%s fundBalance=%s",
			pos.ID, liqResult.Surplus.StringFixed(4), m.insuranceFund.GetBalance().StringFixed(2))
	}

	msg, _ := ws.NewMessage("liquidated", map[string]interface{}{
		"position_id": pos.ID,
		"liq_price":   lastPrice.String(),
		"loss":        margin.Neg().String(),
	})
	m.hub.SendToUser(pos.UserID, msg)

	if m.onPositionClose != nil {
		m.onPositionClose(pos.UserID, pos.ID, result, model.CloseReasonLiquidation)
	}
}

func (m *Monitor) handleForceTP(pos *cache.CachedPosition, lastPrice decimal.Decimal) {
	result, err := m.engine.ClosePositionInternal(pos.ID, lastPrice, model.CloseReasonForceTp)
	if err != nil {
		log.Printf("[Monitor] force TP position %d error: %v", pos.ID, err)
		return
	}
	msg, _ := ws.NewMessage("force_tp", map[string]interface{}{
		"position_id": pos.ID,
		"price":       lastPrice.String(),
		"profit":      result.RealizedPnl.String(),
	})
	m.hub.SendToUser(pos.UserID, msg)

	if m.onPositionClose != nil {
		m.onPositionClose(pos.UserID, pos.ID, result, model.CloseReasonForceTp)
	}
}

func (m *Monitor) handleClose(pos *cache.CachedPosition, lastPrice decimal.Decimal, reason int) {
	result, err := m.engine.ClosePositionInternal(pos.ID, lastPrice, reason)
	if err != nil {
		return
	}
	if m.onPositionClose != nil {
		m.onPositionClose(pos.UserID, pos.ID, result, reason)
	}
}

// triggerADL finds opposing profitable positions and reduces them to cover the deficit.
// bankruptcyPrice is the price at which the liquidated position's margin goes to zero.
func (m *Monitor) triggerADL(liquidatedSide int, deficit, bankruptcyPrice, currentPrice decimal.Decimal) {
	// ADL targets the opposite side (if a long was liquidated, reduce shorts)
	targetSide := -liquidatedSide

	allPositions := m.positionCache.GetAll()
	ranked := adl.RankPositions(allPositions, targetSide, currentPrice)

	if len(ranked) == 0 {
		log.Printf("[Monitor] ADL: no opposing positions to reduce")
		return
	}

	// Use bankruptcy price for settlement (not market price) — ensures zero-sum
	results, remaining := adl.ExecuteADL(ranked, deficit, bankruptcyPrice)
	for _, r := range results {
		// Close at bankruptcy price (counterparty gets less profit than market price)
		closePrice := bankruptcyPrice
		_, err := m.engine.ClosePositionInternal(r.PositionID, closePrice, model.CloseReasonADL)
		if err != nil {
			log.Printf("[Monitor] ADL close position %d error: %v", r.PositionID, err)
			continue
		}
		// Notify the ADL'd user
		msg, _ := ws.NewMessage("adl", map[string]interface{}{
			"position_id":    r.PositionID,
			"reduced_qty":    r.ReducedQty.String(),
			"price":          closePrice.String(),
			"market_price":   currentPrice.String(),
			"realized_pnl":   r.RealizedPnl.String(),
		})
		m.hub.SendToUser(r.UserID, msg)

		log.Printf("[Monitor] ADL: reduced position %d by %s BTC at bankruptcy price %s (market %s)",
			r.PositionID, r.ReducedQty, closePrice, currentPrice)
	}

	if remaining.IsPositive() {
		log.Printf("[Monitor] ADL: still %s deficit uncovered (socialized loss)", remaining)
	}
}

// shouldLiquidate checks if price has crossed the liquidation price.
// Uses mark price to prevent manipulation.
func shouldLiquidate(markPrice, liqPrice decimal.Decimal, side int) bool {
	if side == 1 {
		return markPrice.LessThanOrEqual(liqPrice)
	}
	return markPrice.GreaterThanOrEqual(liqPrice)
}

// shouldTrigger checks if price triggers a TP/SL/ForceTP.
// isProfitDirection: true for TP/ForceTP (price moving in profit direction),
// false for SL (price moving against).
func shouldTrigger(price, triggerPrice decimal.Decimal, side int, isProfitDirection bool) bool {
	if isProfitDirection {
		// TP/ForceTP: long triggers when price >= trigger, short when price <= trigger
		if side == 1 {
			return price.GreaterThanOrEqual(triggerPrice)
		}
		return price.LessThanOrEqual(triggerPrice)
	}
	// SL: long triggers when price <= trigger, short when price >= trigger
	if side == 1 {
		return price.LessThanOrEqual(triggerPrice)
	}
	return price.GreaterThanOrEqual(triggerPrice)
}
