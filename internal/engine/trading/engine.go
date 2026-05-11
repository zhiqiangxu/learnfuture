package trading

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
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
// WithLock executes fn while holding the engine mutex. Use for operations
// that need to be atomic with position/account state (e.g., funding settlement).
func (e *Engine) WithLock(fn func())                    { e.mu.Lock(); defer e.mu.Unlock(); fn() }
func (e *Engine) GetBook() *orderbook.Book              { return e.book }
func (e *Engine) GetClearance() *clearing.Clearance      { return e.clearance }
func (e *Engine) GetFeeRate() decimal.Decimal            { return e.clearance.GetMaintRate() }
func (e *Engine) GetMaintRate() decimal.Decimal          { return e.clearance.GetMaintRate() }
func (e *Engine) GetForceTpROI() decimal.Decimal         { return e.clearance.GetForceTpROI() }
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

	// --- MATCHING ---
	estimatedQty := position.CalcQuantity(margin, leverage, refPrice)
	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: userID, Side: side, Quantity: estimatedQty,
	})
	if len(obTrades) == 0 {
		return nil, ErrNoLiquidity
	}

	// --- UNIFIED POSITION UPDATE (both sides) ---
	takerResults := e.ProcessTrades(obTrades, userID, side, leverage)

	// --- BUILD RESPONSE ---
	avgPrice, totalQty := clearing.CalcVWAP(obTrades)
	var pos *model.Position
	var resultFee decimal.Decimal
	if len(takerResults) > 0 {
		r := takerResults[0]
		resultFee = r.Fee
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

	merged := len(takerResults) > 0 && takerResults[0].Action == "increase"

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
	if !currentPrice.IsZero() {
		deviation := limitPrice.Sub(currentPrice).Abs().Div(currentPrice)
		if deviation.GreaterThan(e.maxPriceDeviation) {
			return nil, ErrPriceDeviationTooLarge
		}
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
		e.memAccounts.Unfreeze(userID, totalCost) // release frozen, updatePosition will deduct
		takerResults := e.ProcessTrades(obTrades, userID, side, leverage)
		avgPrice, totalQty := clearing.CalcVWAP(obTrades)

		var resultFee decimal.Decimal
		if len(takerResults) > 0 {
			resultFee = takerResults[0].Fee
		}
		return &PlaceOrderResult{
			Trades: obTrades, Status: "filled",
			AvgPrice: avgPrice, TotalQty: totalQty, Fee: resultFee,
		}, nil
	}

	// Resting in book
	order := &model.Order{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		OrderType: model.OrderTypeLimit, Leverage: leverage,
		Price: &limitPrice, Quantity: quantity, MarginCost: margin,
		TakeProfit: tp, StopLoss: sl, Status: model.OrderStatusPending,
	}
	e.orderCache.Add(&cache.CachedOrder{
		ID: obOrder.ID, UserID: userID, Side: side,
		Price: limitPrice, Quantity: quantity, MarginCost: margin,
		Leverage: leverage,
		TakeProfit: decimalPtrToString(tp), StopLoss: decimalPtrToString(sl),
	})
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventOpenPosition, Order: order,
		UserID: userID, BalanceDelta: totalCost.Neg(), FrozenDelta: totalCost,
	})
	return &PlaceOrderResult{Order: order, Status: "pending"}, nil
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

	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: userID, Side: -pos.Side, Quantity: actualQty,
	})
	if len(obTrades) == 0 {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	// Reset to Active so updatePosition's FindByUserSide can find it
	e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })

	// --- UNIFIED: processTrades handles both sides ---
	takerResults := e.ProcessTrades(obTrades, userID, -pos.Side, pos.Leverage)
	return e.buildCloseResult(takerResults, pos)
}

// ============================================================
// ClosePositionInternal (risk engine auto-close) — pure memory, no DB read
// ============================================================
func (e *Engine) ClosePositionInternal(positionID int64, closePrice decimal.Decimal, closeReason int) (*CloseResult, error) {
	// Liquidation and ADL bypass orderbook — they should not go through this function
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

	obTrades, _ := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: pos.UserID, Side: -pos.Side, Quantity: pos.Quantity,
	})
	if len(obTrades) == 0 {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	// Reset to Active so updatePosition's FindByUserSide can find it
	e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })

	// --- UNIFIED: processTrades handles both sides ---
	takerResults := e.ProcessTrades(obTrades, pos.UserID, -pos.Side, pos.Leverage)
	return e.buildCloseResult(takerResults, pos)
}

// ============================================================
// applyClose: clearing → memory → settlement
// ============================================================
func (e *Engine) applyClose(pos *model.Position, closePrice, closedQty decimal.Decimal, closeReason int) (*CloseResult, error) {
	// --- CLEARING ---
	cr := e.clearance.ClearClose(&clearing.CloseClearingInput{
		Position: pos, ClosePrice: closePrice, CloseQty: closedQty, Reason: closeReason,
	})

	// --- MEMORY ---
	if closeReason == model.CloseReasonLiquidation {
		e.memAccounts.AddPnl(pos.UserID, cr.CloseMargin.Neg())
	} else {
		e.memAccounts.ReturnMarginWithPnl(pos.UserID, cr.CloseMargin, cr.NetPnl)
	}
	if cr.IsPartial {
		e.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
			cp.Quantity = cr.RemainingQty.String()
			cp.Margin = cr.RemainMargin.String()
			cp.LiqPrice = cr.RemainLiqPrice.String()
			cp.ForceTpPrice = cr.RemainFtpPrice.String()
			cp.State.Store(cache.PosStateActive)
		})
	} else {
		e.positionCache.Remove(pos.ID)
	}

	// --- SETTLEMENT ---
	closeOrder := &model.Order{
		UserID: pos.UserID, Symbol: "BTCUSDT", Side: -pos.Side,
		OrderType: model.OrderTypeMarket, Leverage: pos.Leverage,
		Quantity: cr.ClosedQty, MarginCost: decimal.Zero, Status: model.OrderStatusFilled,
	}
	closeOrder.FilledPrice = &closePrice
	closeTrade := &model.Trade{
		UserID: pos.UserID, PositionID: &pos.ID, Symbol: "BTCUSDT", Side: -pos.Side,
		Price: closePrice, Quantity: cr.ClosedQty,
		Fee: cr.CloseFee, RealizedPnl: cr.RealizedPnl, IsClose: true, CloseReason: closeReason,
	}
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventClosePosition, Order: closeOrder, Trade: closeTrade,
		PositionID: pos.ID, UserID: pos.UserID, CloseReason: closeReason,
		ClosePrice: closePrice, Margin: cr.CloseMargin, RealizedPnl: cr.RealizedPnl,
		Fee: cr.CloseFee, NetPnl: cr.NetPnl,
	})

	return &CloseResult{
		RawPnl: cr.RawPnl, RealizedPnl: cr.RealizedPnl, Fee: cr.CloseFee, FundingPnl: cr.CloseFundingPnl,
		NetPnl: cr.NetPnl, ClosePrice: closePrice, ClosedQty: cr.ClosedQty,
		RemainingQty: cr.RemainingQty, IsPartial: cr.IsPartial,
	}, nil
}

// ============================================================
// FillLimitOrder (called by Monitor)
// ============================================================
func (e *Engine) FillLimitOrder(cachedOrder *cache.CachedOrder) (*PlaceOrderResult, error) {
	fillPrice := cachedOrder.Price
	margin := cachedOrder.MarginCost
	leverage := cachedOrder.Leverage
	side := cachedOrder.Side
	quantity := cachedOrder.Quantity

	// Unfreeze margin — updatePosition will handle the actual deduction
	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	fee, _ := e.clearance.CalcMakerFee(positionValue)
	totalFrozen := margin.Add(fee)
	e.memAccounts.Unfreeze(cachedOrder.UserID, totalFrozen)

	// Synthetic trade for processTrades (limit order was triggered, not matched via orderbook now)
	syntheticTrade := &orderbook.Trade{Price: fillPrice, Quantity: quantity}
	if side == 1 {
		syntheticTrade.BuyUserID = cachedOrder.UserID
		syntheticTrade.SellUserID = 0
	} else {
		syntheticTrade.SellUserID = cachedOrder.UserID
		syntheticTrade.BuyUserID = 0
	}

	e.orderCache.Remove(cachedOrder.ID)
	takerResults := e.ProcessTrades([]*orderbook.Trade{syntheticTrade}, cachedOrder.UserID, side, leverage)

	var resultFee decimal.Decimal
	if len(takerResults) > 0 {
		resultFee = takerResults[0].Fee
	}
	return &PlaceOrderResult{Status: "filled", AvgPrice: fillPrice, TotalQty: quantity, Fee: resultFee}, nil
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

func decimalPtrToString(d *decimal.Decimal) string {
	if d == nil { return "" }
	return d.String()
}

// processTrades handles BOTH sides of every trade using the unified updatePosition.
// This is the single entry point for all position changes after orderbook matching.
// takerUserID: the user who submitted the order (gets taker fee)
// takerLeverage: leverage for new positions opened by the taker
// ProcessTrades handles BOTH sides of every trade using the unified updatePosition.
func (e *Engine) ProcessTrades(trades []*orderbook.Trade, takerUserID int64, takerSide int, takerLeverage int) []*PositionUpdateResult {
	var takerResults []*PositionUpdateResult
	for _, t := range trades {
		// Process buyer
		buyerLeverage := 1 // default for maker/market-maker
		buyerIsMaker := true
		if t.BuyUserID == takerUserID {
			buyerLeverage = takerLeverage
			buyerIsMaker = false
		}
		r := e.UpdatePosition(t.BuyUserID, 1, t.Quantity, t.Price, buyerLeverage, buyerIsMaker, 0)
		if t.BuyUserID == takerUserID && r != nil {
			takerResults = append(takerResults, r)
		}

		// Process seller
		sellerLeverage := 1
		sellerIsMaker := true
		if t.SellUserID == takerUserID {
			sellerLeverage = takerLeverage
			sellerIsMaker = false
		}
		r = e.UpdatePosition(t.SellUserID, -1, t.Quantity, t.Price, sellerLeverage, sellerIsMaker, 0)
		if t.SellUserID == takerUserID && r != nil {
			takerResults = append(takerResults, r)
		}
	}
	return takerResults
}

func LogError(op string, err error) {
	if err != nil { log.Printf("[TradingEngine] %s error: %v", op, err) }
}
