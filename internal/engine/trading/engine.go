package trading

import (
	"database/sql"
	"errors"
	"log"
	"sync"

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
	wal               *clearing.WAL
	accountModel      *model.AccountModel
	orderModel        *model.OrderModel
	positionModel     *model.PositionModel
	tradeModel        *model.TradeModel
	maxLeverage       int
	minMargin         decimal.Decimal
	maxPositions      int
	maxPriceDeviation decimal.Decimal
	memAccounts       *cache.AccountCache
	nextPosID         int64
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
	wal *clearing.WAL,
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
	return &Engine{
		db: db, priceCache: priceCache, positionCache: positionCache,
		orderCache: orderCache, book: book, clearance: clearance,
		settler: settler, wal: wal,
		accountModel: accountModel, orderModel: orderModel,
		positionModel: positionModel, tradeModel: tradeModel,
		maxLeverage: cfg.MaxLeverage, minMargin: cfg.MinMargin,
		maxPositions: maxPos, maxPriceDeviation: maxDev,
		memAccounts: cache.NewAccountCache(), nextPosID: 100000,
	}
}

func (e *Engine) GetMemAccounts() *cache.AccountCache  { return e.memAccounts }
func (e *Engine) GetBook() *orderbook.Book              { return e.book }
func (e *Engine) GetClearance() *clearing.Clearance      { return e.clearance }
func (e *Engine) GetFeeRate() decimal.Decimal            { return e.clearance.GetMaintRate() }
func (e *Engine) GetMaintRate() decimal.Decimal          { return e.clearance.GetMaintRate() }
func (e *Engine) GetForceTpROI() decimal.Decimal         { return e.clearance.GetForceTpROI() }
func (e *Engine) GetMaxLeverage() int                    { return e.maxLeverage }

// ============================================================
// PlaceMarketOrder: match → clear → memory → settle
// ============================================================
func (e *Engine) PlaceMarketOrder(userID int64, side, leverage, marginMode int, margin decimal.Decimal, tp, sl *decimal.Decimal) (*PlaceOrderResult, error) {
	if marginMode == 0 {
		marginMode = model.MarginModeIsolated
	}
	if err := e.validateParams(side, leverage, margin); err != nil {
		return nil, err
	}

	refPrice := e.priceCache.GetPrice()
	if refPrice.IsZero() {
		return nil, ErrNoPriceAvailable
	}

	// Pre-clear to get cost estimate
	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	tradingFee, _ := e.clearance.CalcTakerFee(positionValue)
	totalCost := margin.Add(tradingFee)

	// WAL + memory deduct
	var walSeq uint64
	if e.wal != nil {
		seq, _ := e.wal.WriteGroup([]*clearing.WALEntry{
			{Type: clearing.WALDeduct, UserID: userID, Amount: totalCost},
		})
		walSeq = seq
	}
	if !e.memAccounts.Deduct(userID, totalCost) {
		return nil, model.ErrInsufficientBalance
	}

	// --- MATCHING: submit to orderbook ---
	estimatedQty := position.CalcQuantity(margin, leverage, refPrice)
	obTrades := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: userID, Side: side, Quantity: estimatedQty,
	})
	if len(obTrades) == 0 {
		e.memAccounts.ReturnMarginWithPnl(userID, totalCost, decimal.Zero)
		return nil, ErrNoLiquidity
	}

	// --- COUNTERPARTY: process maker side positions (zero-sum) ---
	e.processCounterpartyTrades(obTrades, side)

	// --- CLEARING: pure calculation ---
	e.mu.Lock()
	existing := e.positionCache.FindByUserSide(userID, side)
	cr := e.clearance.ClearOpen(&clearing.OpenClearingInput{
		UserID: userID, Side: side, Leverage: leverage, MarginMode: marginMode,
		Margin: margin, Trades: obTrades, TakeProfit: tp, StopLoss: sl,
		Existing: existing,
	})

	if cr.RefundAmount.IsPositive() {
		e.memAccounts.ReturnMarginWithPnl(userID, cr.RefundAmount, decimal.Zero)
	}

	// --- MEMORY STATE UPDATE ---
	var pos *model.Position
	if cr.Merged {
		existing.EntryPrice = cr.NewEntryPrice.String()
		existing.Quantity = cr.NewQuantity.String()
		existing.Margin = cr.NewMargin.String()
		existing.LiqPrice = cr.NewLiqPrice.String()
		existing.ForceTpPrice = cr.NewFtpPrice.String()
		if tp != nil { existing.TakeProfit = tp.String() }
		if sl != nil { existing.StopLoss = sl.String() }

		pos = &model.Position{
			ID: existing.ID, UserID: userID, Symbol: "BTCUSDT", Side: side,
			MarginMode: marginMode, Leverage: leverage,
			EntryPrice: cr.NewEntryPrice, Quantity: cr.NewQuantity,
			Margin: cr.NewMargin, LiqPrice: cr.NewLiqPrice, ForceTpPrice: cr.NewFtpPrice,
			TakeProfit: tp, StopLoss: sl, Status: model.PositionStatusActive,
		}
	} else {
		if err := e.checkPositionLimit(userID); err != nil {
			e.mu.Unlock()
			e.memAccounts.ReturnMarginWithPnl(userID, totalCost, decimal.Zero)
			return nil, err
		}
		e.nextPosID++
		pos = &model.Position{
			UserID: userID, Symbol: "BTCUSDT", Side: side,
			MarginMode: marginMode, Leverage: leverage,
			EntryPrice: cr.AvgPrice, Quantity: cr.TotalQty,
			Margin: cr.AdjustedMargin, LiqPrice: cr.LiqPrice, ForceTpPrice: cr.ForceTpPrice,
			TakeProfit: tp, StopLoss: sl, Status: model.PositionStatusActive,
		}
		e.positionCache.Add(&cache.CachedPosition{
			ID: e.nextPosID, UserID: userID, Side: side, MarginMode: marginMode,
			Leverage: leverage, EntryPrice: cr.AvgPrice.String(),
			Quantity: cr.TotalQty.String(), Margin: cr.AdjustedMargin.String(),
			LiqPrice: cr.LiqPrice.String(), ForceTpPrice: cr.ForceTpPrice.String(),
			TakeProfit: decimalPtrToString(tp), StopLoss: decimalPtrToString(sl),
		})
	}
	e.mu.Unlock()

	// --- SETTLEMENT: async DB ---
	order := &model.Order{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		OrderType: model.OrderTypeMarket, Leverage: leverage,
		Quantity: cr.TotalQty, MarginCost: cr.AdjustedMargin,
		TakeProfit: tp, StopLoss: sl, Status: model.OrderStatusFilled,
	}
	fp := cr.AvgPrice
	order.FilledPrice = &fp

	trade := &model.Trade{
		UserID: userID, Symbol: "BTCUSDT", Side: side,
		Price: cr.AvgPrice, Quantity: cr.TotalQty, Fee: cr.AdjustedFee, IsClose: false,
	}

	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventOpenPosition, Seq: walSeq,
		Order: order, Position: pos, Trade: trade,
	})
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventBalanceUpdate, Seq: walSeq,
		UserID: userID, BalanceDelta: totalCost.Neg(),
	})

	status := "filled"
	if cr.Unfilled.IsPositive() { status = "partial" }

	return &PlaceOrderResult{
		Order: order, Position: pos, Trade: trade, Trades: obTrades,
		Status: status, Merged: cr.Merged,
		AvgPrice: cr.AvgPrice, TotalQty: cr.TotalQty, Slippage: cr.Slippage,
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

	if e.wal != nil {
		e.wal.WriteGroup([]*clearing.WALEntry{
			{Type: clearing.WALFreeze, UserID: userID, Amount: totalCost},
		})
	}
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
		avgPrice, totalQty := clearing.CalcVWAP(obTrades)
		e.memAccounts.Unfreeze(userID, totalCost)
		takerFee, _ := e.clearance.CalcTakerFee(positionValue)
		actualCost := margin.Add(takerFee)
		e.memAccounts.Deduct(userID, actualCost)

		liqPrice := e.clearance.CalcLiqPrice(avgPrice, leverage, side)
		ftpPrice := e.clearance.CalcForceTpPrice(avgPrice, leverage, side)

		e.mu.Lock()
		e.nextPosID++
		pos := &model.Position{
			UserID: userID, Symbol: "BTCUSDT", Side: side,
			Leverage: leverage, EntryPrice: avgPrice, Quantity: totalQty,
			Margin: margin, LiqPrice: liqPrice, ForceTpPrice: ftpPrice,
			TakeProfit: tp, StopLoss: sl, Status: model.PositionStatusActive,
		}
		e.positionCache.Add(&cache.CachedPosition{
			ID: e.nextPosID, UserID: userID, Side: side, Leverage: leverage,
			EntryPrice: avgPrice.String(), Quantity: totalQty.String(),
			Margin: margin.String(), LiqPrice: liqPrice.String(),
			ForceTpPrice: ftpPrice.String(),
			TakeProfit: decimalPtrToString(tp), StopLoss: decimalPtrToString(sl),
		})
		e.mu.Unlock()

		order := &model.Order{
			UserID: userID, Symbol: "BTCUSDT", Side: side,
			OrderType: model.OrderTypeLimit, Leverage: leverage,
			Price: &limitPrice, Quantity: totalQty, MarginCost: margin,
			TakeProfit: tp, StopLoss: sl, Status: model.OrderStatusFilled,
		}
		order.FilledPrice = &avgPrice
		trade := &model.Trade{
			UserID: userID, Symbol: "BTCUSDT", Side: side,
			Price: avgPrice, Quantity: totalQty, Fee: takerFee, IsClose: false,
		}
		e.settler.Submit(&clearing.SettleEvent{
			Type: clearing.EventOpenPosition, Order: order, Position: pos, Trade: trade,
		})
		return &PlaceOrderResult{
			Order: order, Position: pos, Trade: trade, Trades: obTrades,
			Status: "filled", AvgPrice: avgPrice, TotalQty: totalQty,
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
	e.settler.Submit(&clearing.SettleEvent{Type: clearing.EventOpenPosition, Order: order})
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventBalanceUpdate,
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
	e.settler.Submit(&clearing.SettleEvent{Type: clearing.EventCancelOrder, OrderID: orderID, UserID: userID})
	e.settler.Submit(&clearing.SettleEvent{
		Type: clearing.EventBalanceUpdate,
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

	obTrades := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: userID, Side: -pos.Side, Quantity: actualQty,
	})
	if len(obTrades) == 0 {
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	// --- COUNTERPARTY: process maker side positions (zero-sum) ---
	e.processCounterpartyTrades(obTrades, -pos.Side)

	closePrice, closedQty := clearing.CalcVWAP(obTrades)
	return e.applyClose(pos, closePrice, closedQty, closeReason)
}

// ============================================================
// ClosePositionInternal (risk engine auto-close) — pure memory, no DB read
// ============================================================
func (e *Engine) ClosePositionInternal(positionID int64, closePrice decimal.Decimal, closeReason int) (*CloseResult, error) {
	var targetState int32
	switch closeReason {
	case model.CloseReasonLiquidation:
		targetState = cache.PosStateLiquidating
	case model.CloseReasonForceTp, model.CloseReasonADL:
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

	obTrades := e.book.PlaceMarket(&orderbook.Order{
		ID: e.book.NextOrderID(), UserID: pos.UserID, Side: -pos.Side, Quantity: pos.Quantity,
	})
	if len(obTrades) == 0 {
		// No liquidity — rollback state, cannot close
		e.positionCache.Update(positionID, func(p *cache.CachedPosition) { p.State.Store(cache.PosStateActive) })
		return nil, ErrNoLiquidity
	}

	// --- COUNTERPARTY: process maker side positions (zero-sum) ---
	e.processCounterpartyTrades(obTrades, -pos.Side)

	// Use actual matched price and quantity (may be partial fill)
	actualPrice, actualQty := clearing.CalcVWAP(obTrades)
	return e.applyClose(pos, actualPrice, actualQty, closeReason)
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
	if e.wal != nil {
		if closeReason == model.CloseReasonLiquidation {
			e.wal.WriteGroup([]*clearing.WALEntry{{Type: clearing.WALPnl, UserID: pos.UserID, Amount: cr.CloseMargin.Neg()}})
		} else {
			e.wal.WriteGroup([]*clearing.WALEntry{{Type: clearing.WALReturn, UserID: pos.UserID, Amount: cr.CloseMargin, Amount2: cr.NetPnl}})
		}
	}
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

	positionValue := margin.Mul(decimal.NewFromInt(int64(leverage)))
	fee, _ := e.clearance.CalcMakerFee(positionValue)
	totalFrozen := margin.Add(fee)

	e.memAccounts.Unfreeze(cachedOrder.UserID, totalFrozen)
	if !e.memAccounts.Deduct(cachedOrder.UserID, totalFrozen) {
		return nil, model.ErrInsufficientBalance
	}

	liqPrice := e.clearance.CalcLiqPrice(fillPrice, leverage, side)
	ftpPrice := e.clearance.CalcForceTpPrice(fillPrice, leverage, side)

	var tp, sl *decimal.Decimal
	if cachedOrder.TakeProfit != "" { d, _ := decimal.NewFromString(cachedOrder.TakeProfit); tp = &d }
	if cachedOrder.StopLoss != "" { d, _ := decimal.NewFromString(cachedOrder.StopLoss); sl = &d }

	pos := &model.Position{
		UserID: cachedOrder.UserID, Symbol: "BTCUSDT", Side: side,
		Leverage: leverage, EntryPrice: fillPrice, Quantity: quantity,
		Margin: margin, LiqPrice: liqPrice, ForceTpPrice: ftpPrice,
		TakeProfit: tp, StopLoss: sl, Status: model.PositionStatusActive,
	}
	trade := &model.Trade{
		UserID: cachedOrder.UserID, OrderID: cachedOrder.ID, Symbol: "BTCUSDT", Side: side,
		Price: fillPrice, Quantity: quantity, Fee: fee, IsClose: false,
	}

	e.orderCache.Remove(cachedOrder.ID)
	e.mu.Lock()
	e.nextPosID++
	e.positionCache.Add(&cache.CachedPosition{
		ID: e.nextPosID, UserID: cachedOrder.UserID, Side: side, Leverage: leverage,
		EntryPrice: fillPrice.String(), Quantity: quantity.String(),
		Margin: margin.String(), LiqPrice: liqPrice.String(), ForceTpPrice: ftpPrice.String(),
		TakeProfit: cachedOrder.TakeProfit, StopLoss: cachedOrder.StopLoss,
	})
	e.mu.Unlock()

	e.settler.Submit(&clearing.SettleEvent{Type: clearing.EventOpenPosition, Position: pos, Trade: trade})
	return &PlaceOrderResult{Position: pos, Trade: trade, Status: "filled", AvgPrice: fillPrice, TotalQty: quantity}, nil
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

func decimalPtrToString(d *decimal.Decimal) string {
	if d == nil { return "" }
	return d.String()
}

// ============================================================
// processCounterpartyTrades handles the maker (counterparty) side
// of every trade, ensuring zero-sum position tracking.
//
// For each trade, the counterparty either:
//   - Has no position → create new position (opposite side)
//   - Has same-direction position → increase position
//   - Has opposite-direction position → reduce/close position (settle PnL)
// ============================================================
func (e *Engine) processCounterpartyTrades(trades []*orderbook.Trade, takerSide int) {
	for _, t := range trades {
		// Determine counterparty userID and their trade side
		var cpUserID int64
		var cpSide int
		if takerSide == 1 { // taker is buyer → counterparty is seller
			cpUserID = t.SellUserID
			cpSide = -1
		} else {
			cpUserID = t.BuyUserID
			cpSide = 1
		}

		tradePrice := t.Price
		tradeQty := t.Quantity

		// Find counterparty's existing position
		existing := e.positionCache.FindByUserSide(cpUserID, -cpSide) // check for opposite position first
		sameDir := e.positionCache.FindByUserSide(cpUserID, cpSide)

		if existing != nil {
			// Counterparty has opposite position → this trade closes/reduces it
			existingQty, _ := decimal.NewFromString(existing.Quantity)
			existingEntry, _ := decimal.NewFromString(existing.EntryPrice)
			existingMargin, _ := decimal.NewFromString(existing.Margin)

			if tradeQty.GreaterThanOrEqual(existingQty) {
				// Full close
				pnl := position.CalcUnrealizedPnL(existingEntry, tradePrice, existingQty, existing.Side)
				e.memAccounts.ReturnMarginWithPnl(cpUserID, existingMargin, pnl)
				e.positionCache.Remove(existing.ID)
				log.Printf("[Counterparty] closed position %d for user %d, pnl=%s", existing.ID, cpUserID, pnl.StringFixed(2))

				// Remaining quantity opens new position in trade direction
				remaining := tradeQty.Sub(existingQty)
				if remaining.IsPositive() {
					e.createCounterpartyPosition(cpUserID, cpSide, tradePrice, remaining)
				}
			} else {
				// Partial close
				ratio := tradeQty.Div(existingQty)
				closeMargin := existingMargin.Mul(ratio)
				pnl := position.CalcUnrealizedPnL(existingEntry, tradePrice, tradeQty, existing.Side)
				e.memAccounts.ReturnMarginWithPnl(cpUserID, closeMargin, pnl)

				newQty := existingQty.Sub(tradeQty)
				newMargin := existingMargin.Sub(closeMargin)
				e.positionCache.Update(existing.ID, func(cp *cache.CachedPosition) {
					cp.Quantity = newQty.String()
					cp.Margin = newMargin.String()
					cp.State.Store(cache.PosStateActive)
				})
				log.Printf("[Counterparty] reduced position %d by %s, pnl=%s", existing.ID, tradeQty, pnl.StringFixed(2))
			}
		} else if sameDir != nil {
			// Counterparty has same-direction position → increase (weighted average)
			oldQty, _ := decimal.NewFromString(sameDir.Quantity)
			oldEntry, _ := decimal.NewFromString(sameDir.EntryPrice)
			oldMargin, _ := decimal.NewFromString(sameDir.Margin)

			newQty := oldQty.Add(tradeQty)
			newEntry := oldEntry.Mul(oldQty).Add(tradePrice.Mul(tradeQty)).Div(newQty)
			addMargin := tradePrice.Mul(tradeQty).Div(decimal.NewFromInt(int64(sameDir.Leverage)))
			newMargin := oldMargin.Add(addMargin)

			e.memAccounts.Deduct(cpUserID, addMargin)
			e.positionCache.Update(sameDir.ID, func(cp *cache.CachedPosition) {
				cp.EntryPrice = newEntry.String()
				cp.Quantity = newQty.String()
				cp.Margin = newMargin.String()
			})
			log.Printf("[Counterparty] increased position %d, qty=%s, entry=%s", sameDir.ID, newQty, newEntry.StringFixed(2))
		} else {
			// No existing position → create new
			e.createCounterpartyPosition(cpUserID, cpSide, tradePrice, tradeQty)
		}
	}
}

// createCounterpartyPosition creates a new position for the counterparty (typically market maker).
func (e *Engine) createCounterpartyPosition(userID int64, side int, price, qty decimal.Decimal) {
	leverage := 1 // market maker uses 1x leverage (fully collateralized)
	margin := price.Mul(qty) // full notional as margin
	e.memAccounts.Deduct(userID, margin)

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
	log.Printf("[Counterparty] created position %d for user %d, side=%d, qty=%s, entry=%s",
		e.nextPosID, userID, side, qty, price.StringFixed(2))
}

func LogError(op string, err error) {
	if err != nil { log.Printf("[TradingEngine] %s error: %v", op, err) }
}
