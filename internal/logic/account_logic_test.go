package logic

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/testutil"
)

func TestAccountLogic_Reset_NoPositionsNoOrders(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(500), decimal.Zero)

	l := NewAccountLogic(svcCtx)
	// AccountModel is nil, so DB call will panic. We test that position/order guards pass.
	// Use recover to verify it gets past the guards into the DB call.
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic from nil AccountModel (meaning guards passed)")
			}
			// Panic from nil AccountModel means position+order guards passed successfully
		}()
		l.Reset(1)
	}()
}

func TestAccountLogic_Reset_HasPositions(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PositionCache.Add(&cache.CachedPosition{ID: 1, UserID: 1})

	l := NewAccountLogic(svcCtx)
	_, err := l.Reset(1)
	if err == nil {
		t.Error("expected error when positions exist")
	}
	if err.Error() != "please close all positions before resetting" {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestAccountLogic_Reset_HasPendingOrders(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.OrderCache.Add(&cache.CachedOrder{ID: 1, UserID: 1, Price: decimal.NewFromInt(60000)})

	l := NewAccountLogic(svcCtx)
	_, err := l.Reset(1)
	if err == nil {
		t.Error("expected error when pending orders exist")
	}
	if err.Error() != "please cancel all pending orders before resetting" {
		t.Errorf("unexpected error: %s", err)
	}
}
