package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

const (
	PositionStatusActive      = 1
	PositionStatusClosed      = 2
	PositionStatusLiquidated  = 3
	PositionStatusForceTp     = 4
	PositionStatusLiquidating = 5 // 强平中: 仓位已被接管，等待订单簿成交
	PositionStatusForceTPing  = 6 // 强盈中: 同上

	SideLong  = 1
	SideShort = -1

	MarginModeIsolated = 1 // 逐仓: 每个仓位独立保证金，爆仓只亏该仓位
	MarginModeCross    = 2 // 全仓: 共享账户余额，不容易爆但爆了全爆
)

type Position struct {
	ID             int64            `db:"id"`
	UserID         int64            `db:"user_id"`
	Symbol         string           `db:"symbol"`
	Side           int              `db:"side"`
	MarginMode     int              `db:"margin_mode"` // 1=逐仓, 2=全仓
	Leverage       int              `db:"leverage"`
	EntryPrice     decimal.Decimal  `db:"entry_price"`
	Quantity       decimal.Decimal  `db:"quantity"`
	Margin         decimal.Decimal  `db:"margin"`
	LiqPrice       decimal.Decimal  `db:"liq_price"`
	ForceTpPrice   decimal.Decimal  `db:"force_tp_price"`
	TakeProfit     *decimal.Decimal `db:"take_profit"`
	StopLoss       *decimal.Decimal `db:"stop_loss"`
	UnrealizedPnl  decimal.Decimal  `db:"unrealized_pnl"`
	FundingPnl     decimal.Decimal  `db:"funding_pnl"`
	Status         int              `db:"status"`
	OpenedAt       time.Time        `db:"opened_at"`
	ClosedAt       *time.Time       `db:"closed_at"`
	CreatedAt      time.Time        `db:"created_at"`
	UpdatedAt      time.Time        `db:"updated_at"`
}

type PositionModel struct {
	db *sql.DB
}

func NewPositionModel(db *sql.DB) *PositionModel {
	return &PositionModel{db: db}
}

func (m *PositionModel) Create(p *Position) error {
	return m.db.QueryRow(
		`INSERT INTO positions (user_id, symbol, side, leverage, entry_price, quantity, margin, liq_price, force_tp_price, take_profit, stop_loss)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, created_at, updated_at, opened_at`,
		p.UserID, p.Symbol, p.Side, p.Leverage, p.EntryPrice, p.Quantity, p.Margin, p.LiqPrice, p.ForceTpPrice, p.TakeProfit, p.StopLoss,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.OpenedAt)
}

func (m *PositionModel) FindActiveByUser(userID int64) ([]*Position, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, symbol, side, leverage, entry_price, quantity, margin, liq_price, force_tp_price,
		        take_profit, stop_loss, unrealized_pnl, funding_pnl, status, opened_at, closed_at, created_at, updated_at
		 FROM positions WHERE user_id = $1 AND status = $2 ORDER BY opened_at DESC`,
		userID, PositionStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPositions(rows)
}

func (m *PositionModel) FindAllActive() ([]*Position, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, symbol, side, leverage, entry_price, quantity, margin, liq_price, force_tp_price,
		        take_profit, stop_loss, unrealized_pnl, funding_pnl, status, opened_at, closed_at, created_at, updated_at
		 FROM positions WHERE status = $1`,
		PositionStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPositions(rows)
}

func (m *PositionModel) FindByID(id int64) (*Position, error) {
	p := &Position{}
	err := m.db.QueryRow(
		`SELECT id, user_id, symbol, side, leverage, entry_price, quantity, margin, liq_price, force_tp_price,
		        take_profit, stop_loss, unrealized_pnl, funding_pnl, status, opened_at, closed_at, created_at, updated_at
		 FROM positions WHERE id = $1`, id,
	).Scan(&p.ID, &p.UserID, &p.Symbol, &p.Side, &p.Leverage, &p.EntryPrice, &p.Quantity, &p.Margin,
		&p.LiqPrice, &p.ForceTpPrice, &p.TakeProfit, &p.StopLoss, &p.UnrealizedPnl, &p.FundingPnl,
		&p.Status, &p.OpenedAt, &p.ClosedAt, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (m *PositionModel) Close(id int64, status int) error {
	_, err := m.db.Exec(
		`UPDATE positions SET status = $1, closed_at = NOW(), updated_at = NOW() WHERE id = $2`,
		status, id,
	)
	return err
}

func (m *PositionModel) UpdateTPSL(id int64, tp, sl *decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE positions SET take_profit = $1, stop_loss = $2, updated_at = NOW() WHERE id = $3 AND status = $4`,
		tp, sl, id, PositionStatusActive,
	)
	return err
}

func (m *PositionModel) UpdateFundingPnl(id int64, fundingPayment decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE positions SET funding_pnl = funding_pnl + $1, updated_at = NOW() WHERE id = $2`,
		fundingPayment, id,
	)
	return err
}

func (m *PositionModel) UpdateMargin(id int64, newMargin, newLiqPrice decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE positions SET margin = $1, liq_price = $2, updated_at = NOW() WHERE id = $3`,
		newMargin, newLiqPrice, id,
	)
	return err
}

func scanPositions(rows *sql.Rows) ([]*Position, error) {
	var positions []*Position
	for rows.Next() {
		p := &Position{}
		err := rows.Scan(&p.ID, &p.UserID, &p.Symbol, &p.Side, &p.Leverage, &p.EntryPrice, &p.Quantity, &p.Margin,
			&p.LiqPrice, &p.ForceTpPrice, &p.TakeProfit, &p.StopLoss, &p.UnrealizedPnl, &p.FundingPnl,
			&p.Status, &p.OpenedAt, &p.ClosedAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}
	return positions, rows.Err()
}
