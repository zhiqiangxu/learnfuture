package model

import (
	"database/sql"
	"time"

	"github.com/shopspring/decimal"
)

type Kline struct {
	ID        int64           `db:"id"`
	Interval  string          `db:"interval"`
	OpenTime  time.Time       `db:"open_time"`
	Open      decimal.Decimal `db:"open"`
	High      decimal.Decimal `db:"high"`
	Low       decimal.Decimal `db:"low"`
	Close     decimal.Decimal `db:"close"`
	Volume    decimal.Decimal `db:"volume"`
	CloseTime time.Time       `db:"close_time"`
}

type KlineModel struct {
	db *sql.DB
}

func NewKlineModel(db *sql.DB) *KlineModel {
	return &KlineModel{db: db}
}

func (m *KlineModel) Upsert(k *Kline) error {
	_, err := m.db.Exec(
		`INSERT INTO klines (interval, open_time, open, high, low, close, volume, close_time)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (interval, open_time) DO UPDATE SET high = $4, low = $5, close = $6, volume = $7, close_time = $8`,
		k.Interval, k.OpenTime, k.Open, k.High, k.Low, k.Close, k.Volume, k.CloseTime,
	)
	return err
}

func (m *KlineModel) Find(interval string, limit int) ([]*Kline, error) {
	rows, err := m.db.Query(
		`SELECT id, interval, open_time, open, high, low, close, volume, close_time
		 FROM klines WHERE interval = $1 ORDER BY open_time DESC LIMIT $2`,
		interval, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var klines []*Kline
	for rows.Next() {
		k := &Kline{}
		err := rows.Scan(&k.ID, &k.Interval, &k.OpenTime, &k.Open, &k.High, &k.Low, &k.Close, &k.Volume, &k.CloseTime)
		if err != nil {
			return nil, err
		}
		klines = append(klines, k)
	}
	return klines, rows.Err()
}
