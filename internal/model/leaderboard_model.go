package model

import (
	"database/sql"

	"github.com/shopspring/decimal"
)

type LeaderboardEntry struct {
	Rank     int
	UserID   int64
	Nickname string
	Value    decimal.Decimal
}

type LeaderboardModel struct {
	db *sql.DB
}

func NewLeaderboardModel(db *sql.DB) *LeaderboardModel {
	return &LeaderboardModel{db: db}
}

// GetPnlRanking returns users ranked by total_pnl descending.
func (m *LeaderboardModel) GetPnlRanking(limit int) ([]*LeaderboardEntry, error) {
	rows, err := m.db.Query(
		`SELECT u.id, u.nickname, a.total_pnl,
		        ROW_NUMBER() OVER (ORDER BY a.total_pnl DESC) as rank
		 FROM accounts a JOIN users u ON a.user_id = u.id
		 WHERE a.total_pnl != 0
		 ORDER BY a.total_pnl DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLeaderboard(rows)
}

// GetROIRanking returns users ranked by ROI (total_pnl / initial_balance * 100).
func (m *LeaderboardModel) GetROIRanking(limit int, initialBalance decimal.Decimal) ([]*LeaderboardEntry, error) {
	rows, err := m.db.Query(
		`SELECT u.id, u.nickname,
		        CASE WHEN $2 > 0 THEN (a.total_pnl / $2 * 100) ELSE 0 END as roi,
		        ROW_NUMBER() OVER (ORDER BY a.total_pnl DESC) as rank
		 FROM accounts a JOIN users u ON a.user_id = u.id
		 WHERE a.total_pnl != 0
		 ORDER BY roi DESC LIMIT $1`, limit, initialBalance,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLeaderboard(rows)
}

// GetUserRank returns a specific user's rank by total_pnl.
func (m *LeaderboardModel) GetUserPnlRank(userID int64) (*LeaderboardEntry, error) {
	e := &LeaderboardEntry{}
	err := m.db.QueryRow(
		`SELECT rank, user_id, nickname, total_pnl FROM (
		   SELECT u.id as user_id, u.nickname, a.total_pnl,
		          ROW_NUMBER() OVER (ORDER BY a.total_pnl DESC) as rank
		   FROM accounts a JOIN users u ON a.user_id = u.id
		 ) sub WHERE user_id = $1`, userID,
	).Scan(&e.Rank, &e.UserID, &e.Nickname, &e.Value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

func scanLeaderboard(rows *sql.Rows) ([]*LeaderboardEntry, error) {
	var entries []*LeaderboardEntry
	for rows.Next() {
		e := &LeaderboardEntry{}
		if err := rows.Scan(&e.UserID, &e.Nickname, &e.Value, &e.Rank); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
