package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

type FundingRate struct {
	ID         int64           `db:"id"`
	Symbol     string          `db:"symbol"`
	Rate       decimal.Decimal `db:"rate"`
	SettleTime time.Time       `db:"settle_time"`
	CreatedAt  time.Time       `db:"created_at"`
}

type FundingSettlement struct {
	ID            int64           `db:"id"`
	UserID        int64           `db:"user_id"`
	PositionID    int64           `db:"position_id"`
	Rate          decimal.Decimal `db:"rate"`
	PositionValue decimal.Decimal `db:"position_value"`
	Payment       decimal.Decimal `db:"payment"`
	SettleTime    time.Time       `db:"settle_time"`
	CreatedAt     time.Time       `db:"created_at"`
}

type FundingModel struct {
	db *sql.DB
}

func NewFundingModel(db *sql.DB) *FundingModel {
	return &FundingModel{db: db}
}

func (m *FundingModel) CreateRate(rate *FundingRate) error {
	return m.db.QueryRow(
		`INSERT INTO funding_rates (symbol, rate, settle_time) VALUES ($1, $2, $3) RETURNING id, created_at`,
		rate.Symbol, rate.Rate, rate.SettleTime,
	).Scan(&rate.ID, &rate.CreatedAt)
}

func (m *FundingModel) GetLatestRate(symbol string) (*FundingRate, error) {
	r := &FundingRate{}
	err := m.db.QueryRow(
		`SELECT id, symbol, rate, settle_time, created_at FROM funding_rates WHERE symbol = $1 ORDER BY settle_time DESC LIMIT 1`,
		symbol,
	).Scan(&r.ID, &r.Symbol, &r.Rate, &r.SettleTime, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (m *FundingModel) CreateSettlement(s *FundingSettlement) error {
	return m.CreateSettlementTx(m.db, s)
}

func (m *FundingModel) CreateSettlementTx(db Executor, s *FundingSettlement) error {
	return db.QueryRow(
		`INSERT INTO funding_settlements (user_id, position_id, rate, position_value, payment, settle_time)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		s.UserID, s.PositionID, s.Rate, s.PositionValue, s.Payment, s.SettleTime,
	).Scan(&s.ID, &s.CreatedAt)
}

func (m *FundingModel) FindSettlementsByUser(userID int64, offset, limit int) ([]*FundingSettlement, error) {
	rows, err := m.db.Query(
		`SELECT id, user_id, position_id, rate, position_value, payment, settle_time, created_at
		 FROM funding_settlements WHERE user_id = $1 ORDER BY settle_time DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*FundingSettlement
	for rows.Next() {
		s := &FundingSettlement{}
		err := rows.Scan(&s.ID, &s.UserID, &s.PositionID, &s.Rate, &s.PositionValue, &s.Payment, &s.SettleTime, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (m *FundingModel) GetTotalFunding(userID int64) (decimal.Decimal, error) {
	var total decimal.Decimal
	err := m.db.QueryRow(
		`SELECT COALESCE(SUM(payment), 0) FROM funding_settlements WHERE user_id = $1`, userID,
	).Scan(&total)
	return total, err
}
