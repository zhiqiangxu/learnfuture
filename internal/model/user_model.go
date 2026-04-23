package model

import (
	"database/sql"
	"time"
)

type User struct {
	ID        int64     `db:"id"`
	OpenID    string    `db:"openid"`
	Nickname  string    `db:"nickname"`
	AvatarURL string    `db:"avatar_url"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type UserModel struct {
	db *sql.DB
}

func NewUserModel(db *sql.DB) *UserModel {
	return &UserModel{db: db}
}

func (m *UserModel) FindByOpenID(openID string) (*User, error) {
	user := &User{}
	err := m.db.QueryRow(
		`SELECT id, openid, nickname, avatar_url, created_at, updated_at FROM users WHERE openid = $1`,
		openID,
	).Scan(&user.ID, &user.OpenID, &user.Nickname, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (m *UserModel) FindByID(id int64) (*User, error) {
	user := &User{}
	err := m.db.QueryRow(
		`SELECT id, openid, nickname, avatar_url, created_at, updated_at FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.OpenID, &user.Nickname, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (m *UserModel) Create(openID, nickname, avatarURL string) (*User, error) {
	user := &User{}
	err := m.db.QueryRow(
		`INSERT INTO users (openid, nickname, avatar_url) VALUES ($1, $2, $3)
		 RETURNING id, openid, nickname, avatar_url, created_at, updated_at`,
		openID, nickname, avatarURL,
	).Scan(&user.ID, &user.OpenID, &user.Nickname, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}
