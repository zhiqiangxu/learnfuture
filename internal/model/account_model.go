package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

type Account struct {
	ID         int64           `db:"id"`
	UserID     int64           `db:"user_id"`
	Balance    decimal.Decimal `db:"balance"`
	Frozen     decimal.Decimal `db:"frozen"`
	TotalPnl   decimal.Decimal `db:"total_pnl"`
	ResetCount int             `db:"reset_count"`
	CreatedAt  time.Time       `db:"created_at"`
	UpdatedAt  time.Time       `db:"updated_at"`
}

type AccountModel struct {
	db *sql.DB
}

func NewAccountModel(db *sql.DB) *AccountModel {
	return &AccountModel{db: db}
}

func (m *AccountModel) FindByUserID(userID int64) (*Account, error) {
	a := &Account{}
	err := m.db.QueryRow(
		`SELECT id, user_id, balance, frozen, total_pnl, reset_count, created_at, updated_at
		 FROM accounts WHERE user_id = $1`, userID,
	).Scan(&a.ID, &a.UserID, &a.Balance, &a.Frozen, &a.TotalPnl, &a.ResetCount, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (m *AccountModel) Create(userID int64, initialBalance decimal.Decimal) (*Account, error) {
	a := &Account{}
	err := m.db.QueryRow(
		`INSERT INTO accounts (user_id, balance) VALUES ($1, $2)
		 RETURNING id, user_id, balance, frozen, total_pnl, reset_count, created_at, updated_at`,
		userID, initialBalance,
	).Scan(&a.ID, &a.UserID, &a.Balance, &a.Frozen, &a.TotalPnl, &a.ResetCount, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (m *AccountModel) UpdateBalance(userID int64, balanceDelta, frozenDelta decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET balance = balance + $1, frozen = frozen + $2, updated_at = NOW() WHERE user_id = $3`,
		balanceDelta, frozenDelta, userID,
	)
	return err
}

func (m *AccountModel) AddPnl(userID int64, pnl decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET balance = balance + $1, total_pnl = total_pnl + $1, updated_at = NOW() WHERE user_id = $2`,
		pnl, userID,
	)
	return err
}

func (m *AccountModel) Reset(userID int64, initialBalance decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET balance = $1, frozen = 0, total_pnl = 0, reset_count = reset_count + 1, updated_at = NOW()
		 WHERE user_id = $2`,
		initialBalance, userID,
	)
	return err
}

// DeductMargin deducts margin from balance atomically, returns error if insufficient.
func (m *AccountModel) DeductMargin(userID int64, amount decimal.Decimal) error {
	result, err := m.db.Exec(
		`UPDATE accounts SET balance = balance - $1, updated_at = NOW()
		 WHERE user_id = $2 AND balance >= $1`,
		amount, userID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// FreezeMargin moves amount from balance to frozen.
func (m *AccountModel) FreezeMargin(userID int64, amount decimal.Decimal) error {
	result, err := m.db.Exec(
		`UPDATE accounts SET balance = balance - $1, frozen = frozen + $1, updated_at = NOW()
		 WHERE user_id = $2 AND balance >= $1`,
		amount, userID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// UnfreezeMargin moves amount from frozen back to balance.
func (m *AccountModel) UnfreezeMargin(userID int64, amount decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET balance = balance + $1, frozen = frozen - $1, updated_at = NOW()
		 WHERE user_id = $2`,
		amount, userID,
	)
	return err
}

// ReturnMarginWithPnl returns margin to balance and applies PnL.
func (m *AccountModel) ReturnMarginWithPnl(userID int64, margin, pnl decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET balance = balance + $1 + $2, total_pnl = total_pnl + $2, updated_at = NOW()
		 WHERE user_id = $3`,
		margin, pnl, userID,
	)
	return err
}

// LiquidateMargin removes margin (total loss) and records the loss in total_pnl.
func (m *AccountModel) LiquidateMargin(userID int64, margin decimal.Decimal) error {
	_, err := m.db.Exec(
		`UPDATE accounts SET total_pnl = total_pnl - $1, updated_at = NOW() WHERE user_id = $2`,
		margin, userID,
	)
	return err
}
