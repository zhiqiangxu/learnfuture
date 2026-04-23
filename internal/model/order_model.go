package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

const (
	OrderStatusPending  = 1
	OrderStatusFilled   = 2
	OrderStatusCanceled = 3

	OrderTypeMarket = 1
	OrderTypeLimit  = 2
)

type Order struct {
	ID          int64            `db:"id"`
	UserID      int64            `db:"user_id"`
	Symbol      string           `db:"symbol"`
	Side        int              `db:"side"`
	OrderType   int              `db:"order_type"`
	Leverage    int              `db:"leverage"`
	Price       *decimal.Decimal `db:"price"`
	Quantity    decimal.Decimal  `db:"quantity"`
	FilledPrice *decimal.Decimal `db:"filled_price"`
	MarginCost  decimal.Decimal  `db:"margin_cost"`
	TakeProfit  *decimal.Decimal `db:"take_profit"`
	StopLoss    *decimal.Decimal `db:"stop_loss"`
	Status      int              `db:"status"`
	PositionID  *int64           `db:"position_id"`
	CreatedAt   time.Time        `db:"created_at"`
	UpdatedAt   time.Time        `db:"updated_at"`
}

type OrderModel struct {
	db *sql.DB
}

func NewOrderModel(db *sql.DB) *OrderModel {
	return &OrderModel{db: db}
}

func (m *OrderModel) Create(o *Order) error {
	return m.db.QueryRow(
		`INSERT INTO orders (user_id, symbol, side, order_type, leverage, price, quantity, margin_cost, take_profit, stop_loss, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, created_at, updated_at`,
		o.UserID, o.Symbol, o.Side, o.OrderType, o.Leverage, o.Price, o.Quantity, o.MarginCost, o.TakeProfit, o.StopLoss, o.Status,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
}

func (m *OrderModel) Fill(id int64, filledPrice decimal.Decimal, positionID int64) error {
	_, err := m.db.Exec(
		`UPDATE orders SET status = $1, filled_price = $2, position_id = $3, updated_at = NOW() WHERE id = $4`,
		OrderStatusFilled, filledPrice, positionID, id,
	)
	return err
}

func (m *OrderModel) Cancel(id int64, userID int64) error {
	result, err := m.db.Exec(
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3 AND status = $4`,
		OrderStatusCanceled, id, userID, OrderStatusPending,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrOrderNotPending
	}
	return nil
}

func (m *OrderModel) FindPendingByUser(userID int64) ([]*Order, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, symbol, side, order_type, leverage, price, quantity, filled_price, margin_cost,
		        take_profit, stop_loss, status, position_id, created_at, updated_at
		 FROM orders WHERE user_id = $1 AND status = $2 ORDER BY created_at DESC`,
		userID, OrderStatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (m *OrderModel) FindAllPending() ([]*Order, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, symbol, side, order_type, leverage, price, quantity, filled_price, margin_cost,
		        take_profit, stop_loss, status, position_id, created_at, updated_at
		 FROM orders WHERE status = $1`,
		OrderStatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (m *OrderModel) FindByUserPaged(userID int64, offset, limit int) ([]*Order, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, symbol, side, order_type, leverage, price, quantity, filled_price, margin_cost,
		        take_profit, stop_loss, status, position_id, created_at, updated_at
		 FROM orders WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (m *OrderModel) FindByID(id int64) (*Order, error) {
	o := &Order{}
	err := m.db.QueryRow(
		`SELECT id, user_id, symbol, side, order_type, leverage, price, quantity, filled_price, margin_cost,
		        take_profit, stop_loss, status, position_id, created_at, updated_at
		 FROM orders WHERE id = $1`, id,
	).Scan(&o.ID, &o.UserID, &o.Symbol, &o.Side, &o.OrderType, &o.Leverage, &o.Price, &o.Quantity,
		&o.FilledPrice, &o.MarginCost, &o.TakeProfit, &o.StopLoss, &o.Status, &o.PositionID, &o.CreatedAt, &o.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func scanOrders(rows *sql.Rows) ([]*Order, error) {
	var orders []*Order
	for rows.Next() {
		o := &Order{}
		err := rows.Scan(&o.ID, &o.UserID, &o.Symbol, &o.Side, &o.OrderType, &o.Leverage, &o.Price, &o.Quantity,
			&o.FilledPrice, &o.MarginCost, &o.TakeProfit, &o.StopLoss, &o.Status, &o.PositionID, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
