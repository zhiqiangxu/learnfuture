package trading

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/clearing"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
)

var (
	ErrNoPriceAvailable       = errors.New("no price available")
	ErrInvalidSide            = errors.New("invalid side, must be 1 or -1")
	ErrInvalidLeverage        = errors.New("invalid leverage")
	ErrMarginTooSmall         = errors.New("margin too small")
	ErrInvalidPrice           = errors.New("invalid limit price")
	ErrPositionNotFound       = errors.New("position not found")
	ErrPriceDeviationTooLarge = errors.New("limit price deviates more than 10% from current price")
	ErrTooManyPositions       = errors.New("maximum number of positions reached")
	ErrPositionClosing        = errors.New("position is already being closed")
	ErrInvalidCloseQuantity   = errors.New("close quantity exceeds position quantity")
	ErrNoLiquidity            = errors.New("no liquidity in order book")
)

// Engine is the matching engine. Strict three-layer separation:
//
//   Engine (Matching)    → orderbook matching, memory state management
//   Clearance (Clearing) → pure calculation: margin/fee/PnL/liq price
//   Settler (Settlement) → DB persistence
// MakerFillCallback is called when a maker's resting order is filled via ProcessTrades.
type MakerFillCallback func(userID int64, result *PositionUpdateResult)

type Engine struct {
	mu                sync.Mutex
	db                *sql.DB
	priceCache        *cache.PriceCache
	positionCache     *cache.PositionCache
	orderCache        *cache.OrderCache
	book              *orderbook.Book
	clearance         *clearing.Clearance
	settler           *clearing.Settler
	accountModel      *model.AccountModel
	orderModel        *model.OrderModel
	positionModel     *model.PositionModel
	tradeModel        *model.TradeModel
	maxLeverage       int
	minMargin         decimal.Decimal
	maxPositions      int
	maxPriceDeviation decimal.Decimal
	memAccounts       *cache.AccountCache
	nextPosID         atomic.Int64
	onMakerFill       MakerFillCallback
}

type EngineConfig struct {
	MaxLeverage       int
	MinMargin         decimal.Decimal
	MaxPositions      int
	MaxPriceDeviation decimal.Decimal
}

type PlaceOrderResult struct {
	Order    *model.Order
	Position *model.Position
	Trade    *model.Trade
	Trades   []*orderbook.Trade
	Status   string
	Merged   bool
	AvgPrice decimal.Decimal
	TotalQty decimal.Decimal
	Slippage decimal.Decimal
	Fee      decimal.Decimal
}

type CloseResult struct {
	RawPnl       decimal.Decimal // price diff pnl before fee
	RealizedPnl  decimal.Decimal // rawPnl - fee
	Fee          decimal.Decimal
	FundingPnl   decimal.Decimal
	NetPnl       decimal.Decimal
	ClosePrice   decimal.Decimal
	ClosedQty    decimal.Decimal
	RemainingQty decimal.Decimal
	IsPartial    bool
}

func NewEngine(
	db *sql.DB,
	priceCache *cache.PriceCache,
	positionCache *cache.PositionCache,
	orderCache *cache.OrderCache,
	book *orderbook.Book,
	accountModel *model.AccountModel,
	orderModel *model.OrderModel,
	positionModel *model.PositionModel,
	tradeModel *model.TradeModel,
	clearance *clearing.Clearance,
	settler *clearing.Settler,
	cfg EngineConfig,
) *Engine {
	maxPos := cfg.MaxPositions
	if maxPos == 0 {
		maxPos = 20
	}
	maxDev := cfg.MaxPriceDeviation
	if maxDev.IsZero() {
		maxDev = decimal.NewFromFloat(0.1)
	}
	if book == nil {
		book = orderbook.NewBook()
	}
	eng := &Engine{
		db: db, priceCache: priceCache, positionCache: positionCache,
		orderCache: orderCache, book: book, clearance: clearance,
		settler: settler,
		accountModel: accountModel, orderModel: orderModel,
		positionModel: positionModel, tradeModel: tradeModel,
		maxLeverage: cfg.MaxLeverage, minMargin: cfg.MinMargin,
		maxPositions: maxPos, maxPriceDeviation: maxDev,
		memAccounts: cache.NewAccountCache(),
	}
	eng.nextPosID.Store(100000)
	return eng
}

func (e *Engine) GetMemAccounts() *cache.AccountCache  { return e.memAccounts }
func (e *Engine) SetOnMakerFill(cb MakerFillCallback)  { e.onMakerFill = cb }
func (e *Engine) SetNextPosID(id int64)                { e.nextPosID.Store(id) }
func (e *Engine) GetSettler() *clearing.Settler         { return e.settler }
// WithLock executes fn while holding the engine mutex. Use for operations
// that need to be atomic with position/account state (e.g., funding settlement).
func (e *Engine) WithLock(fn func())                    { e.mu.Lock(); defer e.mu.Unlock(); fn() }
func (e *Engine) GetBook() *orderbook.Book              { return e.book }
func (e *Engine) GetClearance() *clearing.Clearance      { return e.clearance }
func (e *Engine) GetMaintRate() decimal.Decimal          { return e.clearance.GetMaintRate() }
func (e *Engine) GetMaxLeverage() int                    { return e.maxLeverage }

// ============================================================
// PlaceMarketOrder: validate → match → processTrades (unified)
// ============================================================
func (e *Engine) PlaceMarketOrder(userID int64, side, leverage, marginMode int, margin decimal.Decimal, tp, sl *decimal.Decimal) (*PlaceOrderResult, error) {
	if marginMode == 0 {
		marginMode = model.MarginModeIsolated
	}
	if err := e.validateParams(side, leverage, margin); err != nil {
		return nil, err
	}
	if err := e.checkPositionLimit(userID); err != nil {
		return nil, err
	}

	refPrice := e.priceCache.GetPrice()
	if refPrice.IsZero() {
		return nil, ErrNoPriceAvailable
	}

	// Pre-check balance (actual deduction happens in updatePosition)
	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	tradingFee, _ := e.clearance.CalcTakerFee(positionValue)
	totalCost := margin.Add(tradingFee)
	if e.memAccounts.GetBalance(userID).LessThan(totalCost) {
		return nil, model.ErrInsufficientBalance
	}

	// --- CREATE ORDER ---
	estimatedQty := position.CalcQuantity(margin, leverage, refPrice)
	orderID := e.book.NextOrderID()

	// --- MATCHING ---
	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: orderID, UserID: userID, Side: side, Quantity: estimatedQty,
	})
	if len(obTrades) == 0 {
		return nil, ErrNoLiquidity
	}

	// --- PERSIST ORDER + PROCESS TRADES ---
	avgPrice, totalQty := clearing.CalcVWAP(obTrades)
	e.submitOrder(orderID, userID, side, model.OrderTypeMarket, leverage, nil, totalQty, margin, avgPrice, tp, sl, model.OrderStatusFilled)
	takerResults := e.ProcessTrades(obTrades, userID, side, leverage, orderID)

	// --- BUILD RESPONSE ---
	var pos *model.Position
	var resultFee decimal.Decimal
	if len(takerResults) > 0 {
		// Accumulate fee from all results; use the last open/increase result for position state
		r := takerResults[len(takerResults)-1]
		for _, tr := range takerResults {
			resultFee = resultFee.Add(tr.Fee)
		}
		pos = &model.Position{
			ID: r.PositionID, UserID: userID, Symbol: "BTCUSDT", Side: side,
			MarginMode: marginMode, Leverage: leverage,
			EntryPrice: r.EntryPrice, Quantity: r.Quantity,
			Margin: r.Margin, LiqPrice: r.LiqPrice, ForceTpPrice: r.ForceTpPrice,
			TakeProfit: tp, StopLoss: sl, Status: model.PositionStatusActive,
		}
		// Set TP/SL on the cached position
		if tp != nil || sl != nil {
			e.positionCache.Update(r.PositionID, func(cp *cache.CachedPosition) {
				if tp != nil { cp.TakeProfit = tp.String() }
				if sl != nil { cp.StopLoss = sl.String() }
			})
		}
	}

	status := "filled"

	merged := len(takerResults) > 0 && takerResults[len(takerResults)-1].Action == "increase"

	return &PlaceOrderResult{
		Position: pos, Trades: obTrades,
		Status: status, Merged: merged,
		AvgPrice: avgPrice, TotalQty: totalQty, Fee: resultFee,
	}, nil
}

// ============================================================
// PlaceLimitOrder
// ============================================================
func (e *Engine) PlaceLimitOrder(userID int64, side, leverage int, margin, limitPrice decimal.Decimal, tp, sl *decimal.Decimal) (*PlaceOrderResult, error) {
	if err := e.validateParams(side, leverage, margin); err != nil {
		return nil, err
	}
	if limitPrice.IsZero() {
		return nil, ErrInvalidPrice
	}
	if err := e.checkPositionLimit(userID); err != nil {
		return nil, err
	}

	currentPrice := e.priceCache.GetPrice()
	if currentPrice.IsZero() {
		return nil, ErrNoPriceAvailable
	}
	deviation := limitPrice.Sub(currentPrice).Abs().Div(currentPrice)
	if deviation.GreaterThan(e.maxPriceDeviation) {
		return nil, ErrPriceDeviationTooLarge
	}

	quantity := position.CalcQuantity(margin, leverage, limitPrice)
	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	makerFee, _ := e.clearance.CalcMakerFee(positionValue)
	totalCost := margin.Add(makerFee)

	if !e.memAccounts.Freeze(userID, totalCost) {
		return nil, model.ErrInsufficientBalance
	}

	// --- MATCHING ---
	obOrder := &orderbook.Order{
		ID: e.book.NextOrderID(), UserID: userID, Side: side,
		Price: limitPrice, Quantity: quantity,
	}
	obTrades, remaining := e.book.PlaceLimit(obOrder)

	if len(obTrades) > 0 && remaining == nil {
		// Crossed spread → immediate fill as taker
		e.memAccounts.Unfreeze(userID, totalCost)
		avgPrice, totalQty := clearing.CalcVWAP(obTrades)
		e.submitOrder(obOrder.ID, userID, side, model.OrderTypeLimit, leverage, &limitPrice, totalQty, margin, avgPrice, tp, sl, model.OrderStatusFilled)
		takerResults := e.ProcessTrades(obTrades, userID, side, leverage, obOrder.ID)

		var resultFee decimal.Decimal
		for _, tr := range takerResults {
			resultFee = resultFee.Add(tr.Fee)
		}
		return &PlaceOrderResult{
			Trades: obTrades, Status: "filled",
			AvgPrice: avgPrice, TotalQty: totalQty, Fee: resultFee,
		}, nil
	}

	// Resting in book
	e.submitOrder(obOrder.ID, userID, side, model.OrderTypeLimit, leverage, &limitPrice, quantity, margin, decimal.Zero, tp, sl, model.OrderStatusPending)
	e.orderCache.Add(&cache.CachedOrder{
		ID: obOrder.ID, UserID: userID, Side: side,
		Price: limitPrice, Quantity: quantity, MarginCost: margin,
		Leverage: leverage,
		TakeProfit: decimalPtrToString(tp), StopLoss: decimalPtrToString(sl),
	})
	return &PlaceOrderResult{Status: "pending"}, nil
}

// ============================================================
// CancelOrder
// ============================================================
func (e *Engine) CancelOrder(orderID, userID int64) error {
	cachedOrder, ok := e.orderCache.Get(orderID)
	if !ok || cachedOrder.UserID != userID {
		return model.ErrOrderNotPending
	}
	e.book.Cancel(orderID)
	positionValue := cachedOrder.MarginCost.Mul(decimal.NewFromInt(int64(cachedOrder.Leverage)))
	fee, _ := e.clearance.CalcMakerFee(positionValue)
	totalFrozen := cachedOrder.MarginCost.Add(fee)
	e.memAccounts.Unfreeze(userID, totalFrozen)
	e.orderCache.Remove(orderID)
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventCancelOrder, OrderID: orderID,
		UserID: userID, BalanceDelta: totalFrozen, FrozenDelta: totalFrozen.Neg(),
	})
	return nil
}

// ============================================================
// ClosePosition (user-initiated) — pure memory, no DB read
// ============================================================
func (e *Engine) ClosePosition(positionID, userID int64, closeReason int, closeQty decimal.Decimal) (*CloseResult, error) {
	cp, ok := e.positionCache.Get(positionID)
	if !ok || cp.UserID != userID {
		return nil, ErrPositionNotFound
	}
	// CAS to prevent concurrent close — only one caller can proceed
	if !e.positionCache.TrySetState(positionID, cache.PosStateClosing) {
		return nil, ErrPositionClosing
	}

	pos := cachedToModel(cp)
	if !closeQty.IsZero() && closeQty.GreaterThan(pos.Quantity) {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrInvalidCloseQuantity
	}

	actualQty := pos.Quantity
	if !closeQty.IsZero() { actualQty = closeQty }

	orderID := e.book.NextOrderID()
	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: orderID, UserID: userID, Side: -pos.Side, Quantity: actualQty,
	})
	if len(obTrades) == 0 {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })

	avgPrice, totalQty := clearing.CalcVWAP(obTrades)
	e.submitOrder(orderID, userID, -pos.Side, model.OrderTypeMarket, pos.Leverage, nil, totalQty, decimal.Zero, avgPrice, nil, nil, model.OrderStatusFilled)
	takerResults := e.ProcessTrades(obTrades, userID, -pos.Side, pos.Leverage, orderID)
	return e.buildCloseResult(takerResults, pos)
}

// ============================================================
// ClosePositionInternal (risk engine auto-close)
// ============================================================
func (e *Engine) ClosePositionInternal(positionID int64, closePrice decimal.Decimal, closeReason int) (*CloseResult, error) {
	if closeReason == model.CloseReasonLiquidation || closeReason == model.CloseReasonADL {
		return nil, fmt.Errorf("ClosePositionInternal called with reason=%d, should use TakeOver/UpdatePosition", closeReason)
	}

	var targetState int32
	switch closeReason {
	case model.CloseReasonForceTp:
		targetState = cache.PosStateForceTPing
	default:
		targetState = cache.PosStateClosing
	}
	if !e.positionCache.TrySetState(positionID, targetState) {
		return nil, ErrPositionClosing
	}

	cp, ok := e.positionCache.Get(positionID)
	if !ok {
		return nil, ErrPositionNotFound
	}
	pos := cachedToModel(cp)

	orderID := e.book.NextOrderID()
	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: orderID, UserID: pos.UserID, Side: -pos.Side, Quantity: pos.Quantity,
	})
	if len(obTrades) == 0 {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })

	avgPrice, totalQty := clearing.CalcVWAP(obTrades)
	e.submitOrder(orderID, pos.UserID, -pos.Side, model.OrderTypeMarket, pos.Leverage, nil, totalQty, decimal.Zero, avgPrice, nil, nil, model.OrderStatusFilled)
	takerResults := e.ProcessTrades(obTrades, pos.UserID, -pos.Side, pos.Leverage, orderID)
	return e.buildCloseResult(takerResults, pos)
}

// ============================================================
// Helpers
// ============================================================
func (e *Engine) validateParams(side, leverage int, margin decimal.Decimal) error {
	if side != 1 && side != -1 { return ErrInvalidSide }
	if leverage < 1 || leverage > e.maxLeverage { return ErrInvalidLeverage }
	if margin.LessThan(e.minMargin) { return ErrMarginTooSmall }
	return nil
}

func (e *Engine) checkPositionLimit(userID int64) error {
	if len(e.positionCache.GetByUser(userID)) >= e.maxPositions { return ErrTooManyPositions }
	return nil
}

// cachedToModel converts a CachedPosition to model.Position (pure memory, no DB).
func cachedToModel(cp *cache.CachedPosition) *model.Position {
	entryPrice, _ := decimal.NewFromString(cp.EntryPrice)
	quantity, _ := decimal.NewFromString(cp.Quantity)
	margin, _ := decimal.NewFromString(cp.Margin)
	liqPrice, _ := decimal.NewFromString(cp.LiqPrice)
	ftpPrice, _ := decimal.NewFromString(cp.ForceTpPrice)
	fundingPnl, _ := decimal.NewFromString(cp.FundingPnl)

	pos := &model.Position{
		ID: cp.ID, UserID: cp.UserID, Symbol: "BTCUSDT",
		Side: cp.Side, MarginMode: cp.MarginMode, Leverage: cp.Leverage,
		EntryPrice: entryPrice, Quantity: quantity, Margin: margin,
		LiqPrice: liqPrice, ForceTpPrice: ftpPrice,
		FundingPnl: fundingPnl, Status: model.PositionStatusActive,
	}
	if cp.TakeProfit != "" {
		d, _ := decimal.NewFromString(cp.TakeProfit)
		pos.TakeProfit = &d
	}
	if cp.StopLoss != "" {
		d, _ := decimal.NewFromString(cp.StopLoss)
		pos.StopLoss = &d
	}
	return pos
}

// buildCloseResult constructs a CloseResult from updatePosition results.
func (e *Engine) buildCloseResult(results []*PositionUpdateResult, pos *model.Position) (*CloseResult, error) {
	if len(results) == 0 {
		return nil, errors.New("no position update results")
	}
	r := results[0]
	return &CloseResult{
		RawPnl: r.RawPnl, RealizedPnl: r.RealizedPnl, Fee: r.Fee,
		FundingPnl: r.FundingPnl, NetPnl: r.NetPnl,
		ClosePrice: r.EntryPrice, // price at which the close happened
		ClosedQty: r.ClosedQty, RemainingQty: r.RemainingQty, IsPartial: r.IsPartial,
	}, nil
}

// submitOrder creates a model.Order and submits EventCreateOrder to settler.
// Called once per user action (open/close/liquidation/ADL).
func (e *Engine) submitOrder(id, userID int64, side, orderType, leverage int, price *decimal.Decimal, qty, marginCost, filledPrice decimal.Decimal, tp, sl *decimal.Decimal, status int) {
	order := &model.Order{
		ID: id, UserID: userID, Symbol: "BTCUSDT", Side: side,
		OrderType: orderType, Leverage: leverage, Price: price,
		Quantity: qty, MarginCost: marginCost, Status: status,
		TakeProfit: tp, StopLoss: sl,
	}
	if !filledPrice.IsZero() {
		order.FilledPrice = &filledPrice
	}
	evt := &clearing.SettleEvent{Type: clearing.EventCreateOrder, Order: order, UserID: userID}
	// For resting limit orders: freeze balance
	if status == model.OrderStatusPending {
		totalCost := marginCost.Add(marginCost.Mul(decimal.NewFromInt(int64(leverage))).Mul(e.clearance.GetMaintRate()))
		// Balance freeze is already done in memAccounts.Freeze, settler just records it
		positionValue := marginCost.Mul(decimal.NewFromInt(int64(leverage)))
		fee, _ := e.clearance.CalcMakerFee(positionValue)
		frozen := marginCost.Add(fee)
		evt.BalanceDelta = frozen.Neg()
		evt.FrozenDelta = frozen
		_ = totalCost
	}
	e.settler.Submit(evt)
}

func decimalPtrToString(d *decimal.Decimal) string {
	if d == nil { return "" }
	return d.String()
}

// ProcessTrades handles BOTH sides of every trade using the unified updatePosition.
// takerOrderID is the pre-assigned order ID for the taker side (already submitted via EventCreateOrder).
// Maker side uses the orderbook order ID from the trade (already in DB if resting limit order).
func (e *Engine) ProcessTrades(trades []*orderbook.Trade, takerUserID int64, takerSide int, takerLeverage int, takerOrderID int64) []*PositionUpdateResult {
	var takerResults []*PositionUpdateResult
	for _, t := range trades {
		// Resolve order IDs: taker uses takerOrderID, maker uses their own orderbook order ID
		buyerOrderID, sellerOrderID := t.BuyOrderID, t.SellOrderID
		if t.BuyUserID == takerUserID {
			buyerOrderID = takerOrderID
		}
		if t.SellUserID == takerUserID {
			sellerOrderID = takerOrderID
		}

		// Process buyer
		buyerLeverage, buyerIsMaker := e.resolveLeverage(t.BuyUserID, t.BuyOrderID, takerUserID, takerLeverage)
		r := e.UpdatePosition(t.BuyUserID, 1, t.Quantity, t.Price, buyerLeverage, buyerIsMaker, model.CloseReasonManual, buyerOrderID)
		if r != nil {
			if t.BuyUserID == takerUserID {
				takerResults = append(takerResults, r)
			} else if e.onMakerFill != nil && t.BuyUserID > 0 {
				e.onMakerFill(t.BuyUserID, r)
			}
		}

		// Process seller
		sellerLeverage, sellerIsMaker := e.resolveLeverage(t.SellUserID, t.SellOrderID, takerUserID, takerLeverage)
		r = e.UpdatePosition(t.SellUserID, -1, t.Quantity, t.Price, sellerLeverage, sellerIsMaker, model.CloseReasonManual, sellerOrderID)
		if r != nil {
			if t.SellUserID == takerUserID {
				takerResults = append(takerResults, r)
			} else if e.onMakerFill != nil && t.SellUserID > 0 {
				e.onMakerFill(t.SellUserID, r)
			}
		}
	}
	return takerResults
}

// resolveLeverage determines leverage and maker status for a trade participant.
// Taker uses the leverage from the order request.
// Maker (real user) uses the leverage from their cached order.
// Market maker (UserID=0) / liquidation engine (UserID=-1) defaults to 1x.
func (e *Engine) resolveLeverage(userID, orderID int64, takerUserID int64, takerLeverage int) (leverage int, isMaker bool) {
	if userID == takerUserID {
		return takerLeverage, false
	}
	// Try to get leverage from the maker's cached order
	if cachedOrder, ok := e.orderCache.Get(orderID); ok {
		return cachedOrder.Leverage, true
	}
	// System users (market maker, liquidation engine) — default 1x
	return 1, true
}
