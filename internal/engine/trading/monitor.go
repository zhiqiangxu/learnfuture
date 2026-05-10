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
	liqEngine       *LiquidationEngine
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
		liqEngine:       NewLiquidationEngine(engine, positionCache, engine.GetBook(), insuranceFund),
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

	// Limit orders are now auto-filled by orderbook matching (PlaceLimitOrder + MM onTrades)
	// No need for Monitor to poll checkLimitOrders.

	// 1. Check all active positions
	allPositions := m.positionCache.GetAll()

	// Pre-calculate cross margin data PER USER
	type crossData struct {
		upnlTotal        decimal.Decimal
		maintenanceTotal decimal.Decimal
		positionIDs      []int64
	}
	crossByUser := make(map[int64]*crossData)
	for _, pos := range allPositions {
		if pos.MarginMode == model.MarginModeCross {
			cd, ok := crossByUser[pos.UserID]
			if !ok {
				cd = &crossData{}
				crossByUser[pos.UserID] = cd
			}
			ep, _ := decimal.NewFromString(pos.EntryPrice)
			qty, _ := decimal.NewFromString(pos.Quantity)
			upnl := position.CalcUnrealizedPnL(ep, lastPrice, qty, pos.Side)
			cd.upnlTotal = cd.upnlTotal.Add(upnl)
			mg, _ := decimal.NewFromString(pos.Margin)
			posValue := mg.Mul(decimal.NewFromInt(int64(pos.Leverage)))
			maintMargin := posValue.Mul(m.engine.GetMaintRate())
			cd.maintenanceTotal = cd.maintenanceTotal.Add(maintMargin)
			cd.positionIDs = append(cd.positionIDs, pos.ID)
		}
	}

	// Track which users have already been cross-liquidated to avoid double processing
	crossLiquidated := make(map[int64]bool)

	for _, pos := range allPositions {
		entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
		quantity, _ := decimal.NewFromString(pos.Quantity)
		margin, _ := decimal.NewFromString(pos.Margin)
		liqPrice, _ := decimal.NewFromString(pos.LiqPrice)
		forceTpPrice, _ := decimal.NewFromString(pos.ForceTpPrice)

		// 2a. Liquidation check — uses MARK PRICE (防操纵)
		if pos.MarginMode == model.MarginModeCross {
			if crossLiquidated[pos.UserID] {
				continue // already liquidated all cross positions for this user
			}
			cd := crossByUser[pos.UserID]
			balance := m.engine.GetMemAccounts().GetBalance(pos.UserID)
			accountEquity := balance.Add(cd.upnlTotal)
			if accountEquity.LessThanOrEqual(cd.maintenanceTotal) {
				// Cross margin liquidation: liquidate ALL cross positions for THIS USER
				for _, cpID := range cd.positionIDs {
					cp, ok := m.positionCache.Get(cpID)
					if !ok {
						continue
					}
					cpEntry, _ := decimal.NewFromString(cp.EntryPrice)
					cpQty, _ := decimal.NewFromString(cp.Quantity)
					cpMargin, _ := decimal.NewFromString(cp.Margin)
					m.handleLiquidation(cp, lastPrice, cpEntry, cpQty, cpMargin)
				}
				crossLiquidated[pos.UserID] = true
				continue
			}
		} else {
			// Isolated margin: per-position check
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

	// Liquidation engine: attempt to dispose network positions on the orderbook
	m.liqEngine.OnTick(lastPrice)
}


// handleLiquidation processes a liquidation with insurance fund integration.
func (m *Monitor) handleLiquidation(pos *cache.CachedPosition, lastPrice, entryPrice, quantity, margin decimal.Decimal) {
	// Step 1: Transfer position from user to liquidation engine at bankruptcy price.
	// Does NOT go through orderbook. User loses all margin. ∑longs ≡ ∑shorts maintained.
	bankruptcyPrice := position.CalcBankruptcyPrice(entryPrice, pos.Leverage, pos.Side)
	m.liqEngine.TakeOver(pos, bankruptcyPrice)

	// Step 2: Urgency check — if price already past bankruptcy, insurance fund may be needed now.
	// Actual surplus/deficit is settled later in LiquidationEngine.OnTick when position is closed on market.
	pastBankruptcy := isPastPrice(lastPrice, bankruptcyPrice, pos.Side)
	if pastBankruptcy && m.insuranceFund != nil {
		deficit := bankruptcyPrice.Sub(lastPrice).Abs().Mul(quantity)
		covered, needADL := m.insuranceFund.Cover(deficit)
		log.Printf("[Monitor] liquidation pos=%d price past bankruptcy, deficit=%s covered=%s", pos.ID, deficit, covered)
		if needADL {
			uncovered := deficit.Sub(covered)
			log.Printf("[Monitor] insurance fund depleted, triggering ADL for uncovered=%s", uncovered)
			m.triggerADL(pos.Side, uncovered, bankruptcyPrice, lastPrice)
		}
	}

	msg, _ := ws.NewMessage("liquidated", map[string]interface{}{
		"position_id": pos.ID,
		"liq_price":   lastPrice.String(),
		"loss":        margin.Neg().String(),
	})
	m.hub.SendToUser(pos.UserID, msg)

	if m.onPositionClose != nil {
		rawPnl := position.CalcUnrealizedPnL(entryPrice, bankruptcyPrice, quantity, pos.Side)
		result := &CloseResult{
			RawPnl: rawPnl, RealizedPnl: rawPnl, NetPnl: rawPnl,
			ClosePrice: bankruptcyPrice, ClosedQty: quantity,
		}
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
// ADL does NOT go through the order book — it directly settles positions at bankruptcy price.
// This is because ADL only triggers when the order book has no liquidity.
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
		// Directly reduce counterparty's position via updatePosition — NO order book
		// The counterparty's trade side is opposite to their position (selling to close a long, etc.)
		closeSide := -targetSide // if target is short, close side is buy (1)
		result := m.engine.UpdatePosition(r.UserID, closeSide, r.ReducedQty, bankruptcyPrice, 1, false, model.CloseReasonADL)
		if result == nil {
			log.Printf("[Monitor] ADL: failed to update position for user %d", r.UserID)
			continue
		}

		// Notify the ADL'd user
		msg, _ := ws.NewMessage("adl", map[string]interface{}{
			"position_id":    r.PositionID,
			"reduced_qty":    r.ReducedQty.String(),
			"price":          bankruptcyPrice.String(),
			"market_price":   currentPrice.String(),
			"realized_pnl":   result.RealizedPnl.String(),
		})
		m.hub.SendToUser(r.UserID, msg)

		log.Printf("[Monitor] ADL: reduced position %d by %s BTC at bankruptcy price %s (market %s), pnl=%s",
			r.PositionID, r.ReducedQty, bankruptcyPrice, currentPrice, result.RealizedPnl.StringFixed(2))
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

// isPastPrice checks if price has moved past a threshold in the loss direction.
// Long: price dropped below threshold. Short: price rose above threshold.
func isPastPrice(price, threshold decimal.Decimal, side int) bool {
	if side == 1 {
		return price.LessThan(threshold)
	}
	return price.GreaterThan(threshold)
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
