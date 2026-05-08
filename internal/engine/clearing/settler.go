package clearing

import (
	"log"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/model"
)

// Event types flowing from matching engine → settler
type EventType int

const (
	EventOpenPosition EventType = iota
	EventClosePosition
	EventFillLimitOrder
	EventCancelOrder
	EventFundingSettle
	EventBalanceUpdate
)

// SettleEvent is the unit of work sent from matching to clearing.
type SettleEvent struct {
	Type      EventType
	Timestamp time.Time

	// For EventOpenPosition
	Order    *model.Order
	Position *model.Position
	Trade    *model.Trade

	// For EventClosePosition
	PositionID  int64
	UserID      int64
	CloseReason int
	ClosePrice  decimal.Decimal
	Margin      decimal.Decimal // position margin (returned or lost)
	RealizedPnl decimal.Decimal
	Fee         decimal.Decimal // trading fee
	NetPnl      decimal.Decimal

	// For EventCancelOrder
	OrderID int64

	// For EventBalanceUpdate
	BalanceDelta decimal.Decimal
	FrozenDelta  decimal.Decimal
	PnlDelta     decimal.Decimal

	// For EventFundingSettle
	FundingSettlement *model.FundingSettlement
	FundingPnlDelta   decimal.Decimal
}

type Settler struct {
	events        chan *SettleEvent
	orderModel    *model.OrderModel
	positionModel *model.PositionModel
	tradeModel    *model.TradeModel
	accountModel  *model.AccountModel
	fundingModel  *model.FundingModel
	done          chan struct{}
}

func NewSettler(
	orderModel *model.OrderModel,
	positionModel *model.PositionModel,
	tradeModel *model.TradeModel,
	accountModel *model.AccountModel,
	fundingModel *model.FundingModel,
	bufferSize int,
) *Settler {
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	return &Settler{
		events:        make(chan *SettleEvent, bufferSize),
		orderModel:    orderModel,
		positionModel: positionModel,
		tradeModel:    tradeModel,
		accountModel:  accountModel,
		fundingModel:  fundingModel,
		done:          make(chan struct{}),
	}
}

// Submit sends a settle event to the async processing queue.
func (s *Settler) Submit(event *SettleEvent) {
	event.Timestamp = time.Now()
	select {
	case s.events <- event:
	default:
		log.Printf("[Settler] WARNING: event channel full, dropping event type=%d", event.Type)
	}
}

// Start begins the settlement worker goroutine.
func (s *Settler) Start() {
	go s.processLoop()
}

// Stop gracefully shuts down, draining remaining events.
func (s *Settler) Stop() {
	close(s.done)
	for {
		select {
		case evt := <-s.events:
			s.processEvent(evt)
		default:
			return
		}
	}
}

// QueueLen returns the current number of pending events.
func (s *Settler) QueueLen() int {
	return len(s.events)
}

func (s *Settler) processLoop() {
	for {
		select {
		case evt := <-s.events:
			s.processEvent(evt)
		case <-s.done:
			return
		}
	}
}

func (s *Settler) processEvent(evt *SettleEvent) {
	switch evt.Type {
	case EventOpenPosition:
		s.settleOpen(evt)
	case EventClosePosition:
		s.settleClose(evt)
	case EventCancelOrder:
		s.settleCancelOrder(evt)
	case EventBalanceUpdate:
		s.settleBalanceUpdate(evt)
	case EventFundingSettle:
		s.settleFunding(evt)
	}

	// Mark WAL entries as committed after successful DB write
}

func (s *Settler) settleOpen(evt *SettleEvent) {
	if evt.Order != nil {
		if err := s.orderModel.Create(evt.Order); err != nil {
			log.Printf("[Settler] persist order error: %v", err)
		}
	}
	if evt.Position != nil {
		if err := s.positionModel.Create(evt.Position); err != nil {
			log.Printf("[Settler] persist position error: %v", err)
		}
		if evt.Order != nil && evt.Position.ID > 0 {
			filledPrice := evt.Position.EntryPrice
			s.orderModel.Fill(evt.Order.ID, filledPrice, evt.Position.ID)
		}
	}
	if evt.Trade != nil {
		if evt.Position != nil && evt.Position.ID > 0 {
			evt.Trade.PositionID = &evt.Position.ID
		}
		if err := s.tradeModel.Create(evt.Trade); err != nil {
			log.Printf("[Settler] persist trade error: %v", err)
		}
	}
	// Balance update (combined into open event)
	if evt.UserID > 0 && !evt.BalanceDelta.IsZero() {
		s.accountModel.UpdateBalance(evt.UserID, evt.BalanceDelta, evt.FrozenDelta)
	}
}

func (s *Settler) settleClose(evt *SettleEvent) {
	closeStatus := model.PositionStatusClosed
	switch evt.CloseReason {
	case model.CloseReasonLiquidation:
		closeStatus = model.PositionStatusLiquidated
	case model.CloseReasonForceTp:
		closeStatus = model.PositionStatusForceTp
	}
	s.positionModel.Close(evt.PositionID, closeStatus)

	if evt.Order != nil {
		s.orderModel.Create(evt.Order)
	}
	if evt.Trade != nil {
		s.tradeModel.Create(evt.Trade)
	}

	// Account settlement: use Margin (not Fee) for margin operations
	if evt.CloseReason == model.CloseReasonLiquidation {
		// Liquidation: margin already deducted at open, just record the loss in total_pnl
		s.accountModel.LiquidateMargin(evt.UserID, evt.Margin)
	} else {
		// Normal close: return margin + net PnL
		s.accountModel.ReturnMarginWithPnl(evt.UserID, evt.Margin, evt.NetPnl)
	}
}

func (s *Settler) settleCancelOrder(evt *SettleEvent) {
	s.orderModel.Cancel(evt.OrderID, evt.UserID)
	if evt.UserID > 0 && !evt.BalanceDelta.IsZero() {
		s.accountModel.UpdateBalance(evt.UserID, evt.BalanceDelta, evt.FrozenDelta)
	}
}

func (s *Settler) settleBalanceUpdate(evt *SettleEvent) {
	if !evt.BalanceDelta.IsZero() || !evt.FrozenDelta.IsZero() {
		s.accountModel.UpdateBalance(evt.UserID, evt.BalanceDelta, evt.FrozenDelta)
	}
	if !evt.PnlDelta.IsZero() {
		s.accountModel.AddPnl(evt.UserID, evt.PnlDelta)
	}
}

func (s *Settler) settleFunding(evt *SettleEvent) {
	if evt.FundingSettlement != nil {
		s.fundingModel.CreateSettlement(evt.FundingSettlement)
	}
	if evt.PositionID > 0 && !evt.FundingPnlDelta.IsZero() {
		s.positionModel.UpdateFundingPnl(evt.PositionID, evt.FundingPnlDelta)
	}
	if evt.UserID > 0 && !evt.PnlDelta.IsZero() {
		s.accountModel.AddPnl(evt.UserID, evt.PnlDelta)
	}
}

