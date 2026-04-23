//go:build integration
// +build integration

package model

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// These tests require a real PostgreSQL database.
// Run with: go test -tags=integration -v ./internal/model/...
// Set TEST_PG_DSN env var or use default localhost.

func getTestDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=postgres password=postgres dbname=learn_future_test sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("skipping integration test: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("skipping integration test: %v", err)
	}
	return db
}

func TestUserModel_CreateAndFind(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	m := NewUserModel(db)
	user, err := m.Create(fmt.Sprintf("test_openid_%d", os.Getpid()), "TestUser", "")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 {
		t.Error("expected non-zero ID")
	}

	found, err := m.FindByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.OpenID != user.OpenID {
		t.Errorf("expected openid=%s, got %s", user.OpenID, found.OpenID)
	}

	// Cleanup
	db.Exec("DELETE FROM users WHERE id = $1", user.ID)
}

func TestAccountModel_CreateAndDeduct(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	um := NewUserModel(db)
	user, _ := um.Create(fmt.Sprintf("test_acct_%d", os.Getpid()), "", "")

	m := NewAccountModel(db)
	acct, err := m.Create(user.ID, decimal.NewFromInt(10000))
	if err != nil {
		t.Fatal(err)
	}
	if !acct.Balance.Equal(decimal.NewFromInt(10000)) {
		t.Errorf("expected balance=10000, got %s", acct.Balance)
	}

	err = m.DeductMargin(user.ID, decimal.NewFromInt(500))
	if err != nil {
		t.Fatal(err)
	}

	acct2, _ := m.FindByUserID(user.ID)
	if !acct2.Balance.Equal(decimal.NewFromInt(9500)) {
		t.Errorf("expected 9500 after deduct, got %s", acct2.Balance)
	}

	// Test insufficient
	err = m.DeductMargin(user.ID, decimal.NewFromInt(99999))
	if err != ErrInsufficientBalance {
		t.Errorf("expected ErrInsufficientBalance, got %v", err)
	}

	// Cleanup
	db.Exec("DELETE FROM accounts WHERE user_id = $1", user.ID)
	db.Exec("DELETE FROM users WHERE id = $1", user.ID)
}

func TestPositionModel_CreateAndClose(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	um := NewUserModel(db)
	user, _ := um.Create(fmt.Sprintf("test_pos_%d", os.Getpid()), "", "")

	m := NewPositionModel(db)
	pos := &Position{
		UserID: user.ID, Symbol: "BTCUSDT", Side: 1, Leverage: 10,
		EntryPrice: decimal.NewFromInt(60000), Quantity: decimal.NewFromFloat(0.01),
		Margin: decimal.NewFromInt(60), LiqPrice: decimal.NewFromInt(54300),
		ForceTpPrice: decimal.NewFromInt(90000), Status: PositionStatusActive,
	}
	err := m.Create(pos)
	if err != nil {
		t.Fatal(err)
	}
	if pos.ID == 0 {
		t.Error("expected non-zero position ID")
	}

	// Find active
	active, err := m.FindActiveByUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active, got %d", len(active))
	}

	// Close
	m.Close(pos.ID, PositionStatusClosed)
	active2, _ := m.FindActiveByUser(user.ID)
	if len(active2) != 0 {
		t.Errorf("expected 0 active after close, got %d", len(active2))
	}

	// Cleanup
	db.Exec("DELETE FROM positions WHERE user_id = $1", user.ID)
	db.Exec("DELETE FROM users WHERE id = $1", user.ID)
}

func TestTutorialModel_MarkAndGet(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	um := NewUserModel(db)
	user, _ := um.Create(fmt.Sprintf("test_tut_%d", os.Getpid()), "", "")

	am := NewAccountModel(db)
	am.Create(user.ID, decimal.NewFromInt(10000))

	m := NewTutorialModel(db)

	// Mark complete
	err := m.MarkComplete(user.ID, "leverage_explained")
	if err != nil {
		t.Fatal(err)
	}

	// Mark again (idempotent)
	err = m.MarkComplete(user.ID, "leverage_explained")
	if err != nil {
		t.Fatal(err)
	}

	// Get completed
	completed, err := m.GetCompleted(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !completed["leverage_explained"] {
		t.Error("expected leverage_explained to be completed")
	}

	count, _ := m.GetCompletedCount(user.ID)
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// Cleanup
	db.Exec("DELETE FROM tutorial_progress WHERE user_id = $1", user.ID)
	db.Exec("DELETE FROM accounts WHERE user_id = $1", user.ID)
	db.Exec("DELETE FROM users WHERE id = $1", user.ID)
}
