package clearing

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/model"
)

// Event types flowing from matching engine → settler
type EventType int

const (
	EventCreateOrder  EventType = iota // Write order to DB (once per user action)
	EventTrade                         // Write trade + position changes + balance
	EventCancelOrder                   // Cancel a pending order
	EventFundingSettle                 // Funding rate settlement
	EventBalanceUpdate                 // Balance adjustment (frozen/unfrozen)
)

// SettleEvent is the unit of work sent from matching to clearing.
type SettleEvent struct {
	Type      EventType
	Timestamp time.Time
	UserID    int64

	// EventCreateOrder: write order to DB
	Order *model.Order

	// EventTrade: write trade + position changes
	Trade    *model.Trade
	Position *model.Position // non-nil for new position (open)

	// Position close/reduce fields
	PositionID      int64
	CloseReason     int
	ClosePrice      decimal.Decimal
	Margin          decimal.Decimal
	RealizedPnl     decimal.Decimal
	Fee             decimal.Decimal
	NetPnl          decimal.Decimal
	IsPartialClose  bool
	RemainingQty    decimal.Decimal
	RemainingMargin decimal.Decimal

	// EventCancelOrder
	OrderID int64

	// Balance
	BalanceDelta decimal.Decimal
	FrozenDelta  decimal.Decimal
	PnlDelta     decimal.Decimal

	// EventFundingSettle
	FundingSettlement *model.FundingSettlement
	FundingPnlDelta   decimal.Decimal
}

type Settler struct {
	db            *sql.DB
	events        chan *SettleEvent
	orderModel    *model.OrderModel
	positionModel *model.PositionModel
	tradeModel    *model.TradeModel
	accountModel  *model.AccountModel
	fundingModel  *model.FundingModel
	done          chan struct{}
}

func NewSettler(
	db *sql.DB,
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
		db:            db,
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
// Blocks if the queue is full — never drops events.
func (s *Settler) Submit(event *SettleEvent) {
	event.Timestamp = time.Now()
	select {
	case s.events <- event:
	default:
		log.Printf("[Settler] WARNING: event channel full, blocking until space available (type=%d)", event.Type)
		s.events <- event // blocking send
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
	case EventCreateOrder:
		s.settleCreateOrder(evt)
	case EventTrade:
		s.settleTrade(evt)
	case EventCancelOrder:
		s.settleCancelOrder(evt)
	case EventBalanceUpdate:
		s.settleBalanceUpdate(evt)
	case EventFundingSettle:
		s.settleFunding(evt)
	}
}

// settleCreateOrder writes a new order to DB.
func (s *Settler) settleCreateOrder(evt *SettleEvent) {
	if err := s.execTx(func(tx *sql.Tx) error {
		if err := s.orderModel.CreateTx(tx, evt.Order); err != nil {
			return fmt.Errorf("create order: %w", err)
		}
		// For resting limit orders: freeze balance
		if evt.UserID > 0 && (!evt.BalanceDelta.IsZero() || !evt.FrozenDelta.IsZero()) {
			if err := s.accountModel.UpdateBalanceTx(tx, evt.UserID, evt.BalanceDelta, evt.FrozenDelta); err != nil {
				return fmt.Errorf("update balance: %w", err)
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Settler] settleCreateOrder error: %v", err)
	}
}

// settleTrade writes a trade record + handles position/balance changes.
func (s *Settler) settleTrade(evt *SettleEvent) {
	if err := s.execTx(func(tx *sql.Tx) error {
		// Create new position if needed (open)
		if evt.Position != nil {
			if err := s.positionModel.CreateTx(tx, evt.Position); err != nil {
				return fmt.Errorf("create position: %w", err)
			}
			// Link trade to the DB-assigned position ID
			if evt.Trade != nil {
				evt.Trade.PositionID = &evt.Position.ID
			}
		}

		// Write trade
		if evt.Trade != nil {
			if err := s.tradeModel.CreateTx(tx, evt.Trade); err != nil {
				return fmt.Errorf("create trade: %w", err)
			}
		}

		// Position close/reduce
		if evt.PositionID > 0 {
			if evt.IsPartialClose {
				if err := s.positionModel.UpdateQuantityAndMarginTx(tx, evt.PositionID, evt.RemainingQty, evt.RemainingMargin); err != nil {
					return fmt.Errorf("update position partial close: %w", err)
				}
			} else if evt.CloseReason > 0 || evt.Trade != nil && evt.Trade.IsClose {
				closeStatus := model.PositionStatusClosed
				switch evt.CloseReason {
				case model.CloseReasonLiquidation:
					closeStatus = model.PositionStatusLiquidated
				case model.CloseReasonForceTp:
					closeStatus = model.PositionStatusForceTp
				}
				if err := s.positionModel.CloseTx(tx, evt.PositionID, closeStatus); err != nil {
					return fmt.Errorf("close position: %w", err)
				}
			}
		}

		// Balance update
		if evt.UserID > 0 && !evt.BalanceDelta.IsZero() {
			if err := s.accountModel.UpdateBalanceTx(tx, evt.UserID, evt.BalanceDelta, evt.FrozenDelta); err != nil {
				return fmt.Errorf("update balance: %w", err)
			}
		}

		return nil
	}); err != nil {
		log.Printf("[Settler] settleTrade error: %v", err)
	}
}

func (s *Settler) settleCancelOrder(evt *SettleEvent) {
	if err := s.execTx(func(tx *sql.Tx) error {
		if err := s.orderModel.CancelTx(tx, evt.OrderID, evt.UserID); err != nil {
			return fmt.Errorf("cancel order: %w", err)
		}
		if evt.UserID > 0 && !evt.BalanceDelta.IsZero() {
			if err := s.accountModel.UpdateBalanceTx(tx, evt.UserID, evt.BalanceDelta, evt.FrozenDelta); err != nil {
				return fmt.Errorf("update balance: %w", err)
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Settler] settleCancelOrder error: %v", err)
	}
}

func (s *Settler) settleBalanceUpdate(evt *SettleEvent) {
	if err := s.execTx(func(tx *sql.Tx) error {
		if !evt.BalanceDelta.IsZero() || !evt.FrozenDelta.IsZero() {
			if err := s.accountModel.UpdateBalanceTx(tx, evt.UserID, evt.BalanceDelta, evt.FrozenDelta); err != nil {
				return fmt.Errorf("update balance: %w", err)
			}
		}
		if !evt.PnlDelta.IsZero() {
			if err := s.accountModel.AddPnlTx(tx, evt.UserID, evt.PnlDelta); err != nil {
				return fmt.Errorf("add pnl: %w", err)
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Settler] settleBalanceUpdate error: %v", err)
	}
}

func (s *Settler) settleFunding(evt *SettleEvent) {
	if err := s.execTx(func(tx *sql.Tx) error {
		if evt.FundingSettlement != nil {
			if err := s.fundingModel.CreateSettlementTx(tx, evt.FundingSettlement); err != nil {
				return fmt.Errorf("create settlement: %w", err)
			}
		}
		if evt.PositionID > 0 && !evt.FundingPnlDelta.IsZero() {
			if err := s.positionModel.UpdateFundingPnlTx(tx, evt.PositionID, evt.FundingPnlDelta); err != nil {
				return fmt.Errorf("update funding pnl: %w", err)
			}
		}
		if evt.UserID > 0 && !evt.PnlDelta.IsZero() {
			if err := s.accountModel.AddPnlTx(tx, evt.UserID, evt.PnlDelta); err != nil {
				return fmt.Errorf("add pnl: %w", err)
			}
		}
		return nil
	}); err != nil {
		log.Printf("[Settler] settleFunding error: %v", err)
	}
}

// execTx runs a function within a database transaction.
func (s *Settler) execTx(fn func(tx *sql.Tx) error) error {
	if s.db == nil {
		return nil // no DB (test mode)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
