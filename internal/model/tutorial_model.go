package model

import (
	"database/sql"
	"time"
)

type TutorialProgress struct {
	ID          int64     `db:"id"`
	UserID      int64     `db:"user_id"`
	TopicID     string    `db:"topic_id"`
	CompletedAt time.Time `db:"completed_at"`
}

type TutorialModel struct {
	db *sql.DB
}

func NewTutorialModel(db *sql.DB) *TutorialModel {
	return &TutorialModel{db: db}
}

func (m *TutorialModel) MarkComplete(userID int64, topicID string) error {
	_, err := m.db.Exec(
		`INSERT INTO tutorial_progress (user_id, topic_id) VALUES ($1, $2) ON CONFLICT (user_id, topic_id) DO NOTHING`,
		userID, topicID,
	)
	return err
}

func (m *TutorialModel) GetCompleted(userID int64) (map[string]bool, error) {
	rows, err := m.db.Query(
		`SELECT topic_id FROM tutorial_progress WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var topicID string
		if err := rows.Scan(&topicID); err != nil {
			return nil, err
		}
		result[topicID] = true
	}
	return result, rows.Err()
}

func (m *TutorialModel) GetCompletedCount(userID int64) (int, error) {
	var count int
	err := m.db.QueryRow(
		`SELECT COUNT(*) FROM tutorial_progress WHERE user_id = $1`, userID,
	).Scan(&count)
	return count, err
}
