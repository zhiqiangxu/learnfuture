package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

const (
	CloseReasonManual      = 0
	CloseReasonTakeProfit  = 1
	CloseReasonStopLoss    = 2
	CloseReasonLiquidation = 3
	CloseReasonForceTp     = 4
	CloseReasonADL         = 5
)

type Trade struct {
	ID          int64           `db:"id"`
	UserID      int64           `db:"user_id"`
	OrderID     int64           `db:"order_id"`
	PositionID  *int64          `db:"position_id"`
	Symbol      string          `db:"symbol"`
	Side        int             `db:"side"`
	Price       decimal.Decimal `db:"price"`
	Quantity    decimal.Decimal `db:"quantity"`
	Fee         decimal.Decimal `db:"fee"`
	RealizedPnl decimal.Decimal `db:"realized_pnl"`
	IsClose     bool            `db:"is_close"`
	CloseReason int             `db:"close_reason"`
	CreatedAt   time.Time       `db:"created_at"`
}

type TradeModel struct {
	db *sql.DB
}

func NewTradeModel(db *sql.DB) *TradeModel {
	return &TradeModel{db: db}
}

func (m *TradeModel) Create(t *Trade) error {
	return m.db.QueryRow(
		`INSERT INTO trades (user_id, order_id, position_id, symbol, side, price, quantity, fee, realized_pnl, is_close, close_reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, created_at`,
		t.UserID, t.OrderID, t.PositionID, t.Symbol, t.Side, t.Price, t.Quantity, t.Fee, t.RealizedPnl, t.IsClose, t.CloseReason,
	).Scan(&t.ID, &t.CreatedAt)
}

func (m *TradeModel) FindByUserPaged(userID int64, offset, limit int) ([]*Trade, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, order_id, position_id, symbol, side, price, quantity, fee, realized_pnl, is_close, close_reason, created_at
		 FROM trades WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTrades(rows)
}

func (m *TradeModel) GetPnlSummary(userID int64) (*PnlSummaryResult, error) {
	r := &PnlSummaryResult{}
	err := m.db.QueryRow(
		`SELECT COALESCE(SUM(realized_pnl), 0), COALESCE(SUM(fee), 0),
		        COUNT(CASE WHEN is_close AND realized_pnl > 0 THEN 1 END),
		        COUNT(CASE WHEN is_close AND realized_pnl <= 0 THEN 1 END),
		        COALESCE(MAX(CASE WHEN is_close THEN realized_pnl END), 0),
		        COALESCE(MIN(CASE WHEN is_close THEN realized_pnl END), 0),
		        COUNT(CASE WHEN close_reason = 3 THEN 1 END),
		        COUNT(CASE WHEN close_reason = 4 THEN 1 END)
		 FROM trades WHERE user_id = $1`,
		userID,
	).Scan(&r.TotalPnl, &r.TotalFee, &r.WinCount, &r.LossCount, &r.BestTrade, &r.WorstTrade,
		&r.LiquidatedCount, &r.ForceTpCount)
	return r, err
}

type PnlSummaryResult struct {
	TotalPnl        decimal.Decimal
	TotalFee        decimal.Decimal
	WinCount        int
	LossCount       int
	BestTrade       decimal.Decimal
	WorstTrade      decimal.Decimal
	LiquidatedCount int
	ForceTpCount    int
}

func scanTrades(rows *sql.Rows) ([]*Trade, error) {
	var trades []*Trade
	for rows.Next() {
		t := &Trade{}
		err := rows.Scan(&t.ID, &t.UserID, &t.OrderID, &t.PositionID, &t.Symbol, &t.Side,
			&t.Price, &t.Quantity, &t.Fee, &t.RealizedPnl, &t.IsClose, &t.CloseReason, &t.CreatedAt)
		if err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
