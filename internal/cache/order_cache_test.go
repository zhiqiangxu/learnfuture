package cache

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestOrderCache_AddAndGet(t *testing.T) {
	oc := NewOrderCache()
	order := &CachedOrder{ID: 1, UserID: 100, Side: 1, Price: decimal.NewFromInt(59000)}
	oc.Add(order)

	got, ok := oc.Get(1)
	if !ok {
		t.Fatal("expected to find order")
	}
	if got.UserID != 100 {
		t.Errorf("expected userID 100, got %d", got.UserID)
	}
}

func TestOrderCache_Remove(t *testing.T) {
	oc := NewOrderCache()
	oc.Add(&CachedOrder{ID: 1, UserID: 100})
	removed := oc.Remove(1)
	if removed == nil {
		t.Fatal("expected removed order")
	}

	_, ok := oc.Get(1)
	if ok {
		t.Error("expected order to be removed")
	}
}

func TestOrderCache_GetByUser(t *testing.T) {
	oc := NewOrderCache()
	oc.Add(&CachedOrder{ID: 1, UserID: 100})
	oc.Add(&CachedOrder{ID: 2, UserID: 100})
	oc.Add(&CachedOrder{ID: 3, UserID: 200})

	orders := oc.GetByUser(100)
	if len(orders) != 2 {
		t.Errorf("expected 2 orders for user 100, got %d", len(orders))
	}
}

