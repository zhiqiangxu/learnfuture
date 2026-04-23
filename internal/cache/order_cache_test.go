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

func TestOrderCache_GetTriggered_LongBuy(t *testing.T) {
	oc := NewOrderCache()
	// Long limit order at 59000 - triggers when price <= 59000
	oc.Add(&CachedOrder{ID: 1, Side: 1, Price: decimal.NewFromInt(59000)})

	// Price at 58900 - should trigger
	triggered := oc.GetTriggered(decimal.NewFromInt(58900))
	if len(triggered) != 1 {
		t.Errorf("expected 1 triggered order, got %d", len(triggered))
	}

	// Price at 59100 - should not trigger
	triggered = oc.GetTriggered(decimal.NewFromInt(59100))
	if len(triggered) != 0 {
		t.Errorf("expected 0 triggered orders, got %d", len(triggered))
	}
}

func TestOrderCache_GetTriggered_ShortSell(t *testing.T) {
	oc := NewOrderCache()
	// Short limit order at 61000 - triggers when price >= 61000
	oc.Add(&CachedOrder{ID: 1, Side: -1, Price: decimal.NewFromInt(61000)})

	// Price at 61100 - should trigger
	triggered := oc.GetTriggered(decimal.NewFromInt(61100))
	if len(triggered) != 1 {
		t.Errorf("expected 1 triggered order, got %d", len(triggered))
	}

	// Price at 60900 - should not trigger
	triggered = oc.GetTriggered(decimal.NewFromInt(60900))
	if len(triggered) != 0 {
		t.Errorf("expected 0 triggered orders, got %d", len(triggered))
	}
}

func TestOrderCache_GetTriggered_NoOrders(t *testing.T) {
	oc := NewOrderCache()
	triggered := oc.GetTriggered(decimal.NewFromInt(60000))
	if len(triggered) != 0 {
		t.Errorf("expected 0, got %d", len(triggered))
	}
}

func TestOrderCache_GetAllSorted(t *testing.T) {
	oc := NewOrderCache()
	oc.Add(&CachedOrder{ID: 1, Side: 1, Price: decimal.NewFromInt(59000)})
	oc.Add(&CachedOrder{ID: 2, Side: 1, Price: decimal.NewFromInt(60000)})
	oc.Add(&CachedOrder{ID: 3, Side: -1, Price: decimal.NewFromInt(61000)})
	oc.Add(&CachedOrder{ID: 4, Side: -1, Price: decimal.NewFromInt(62000)})

	sorted := oc.GetAllSorted()
	if len(sorted) != 4 {
		t.Fatalf("expected 4 orders, got %d", len(sorted))
	}
	// Longs first (desc), then shorts (asc)
	if sorted[0].ID != 2 { // long 60000 first
		t.Errorf("expected long 60000 first, got ID=%d price=%s", sorted[0].ID, sorted[0].Price)
	}
	if sorted[2].ID != 3 { // short 61000 before 62000
		t.Errorf("expected short 61000 third, got ID=%d", sorted[2].ID)
	}
}
