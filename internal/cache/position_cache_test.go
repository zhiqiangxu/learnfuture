package cache

import (
	"sync"
	"testing"
)

func TestPositionCache_AddAndGet(t *testing.T) {
	pc := NewPositionCache()
	pos := &CachedPosition{ID: 1, UserID: 100, Side: 1}
	pc.Add(pos)

	got, ok := pc.Get(1)
	if !ok {
		t.Fatal("expected to find position")
	}
	if got.UserID != 100 {
		t.Errorf("expected userID 100, got %d", got.UserID)
	}
}

func TestPositionCache_GetByUser(t *testing.T) {
	pc := NewPositionCache()
	pc.Add(&CachedPosition{ID: 1, UserID: 100})
	pc.Add(&CachedPosition{ID: 2, UserID: 100})
	pc.Add(&CachedPosition{ID: 3, UserID: 200})

	positions := pc.GetByUser(100)
	if len(positions) != 2 {
		t.Errorf("expected 2 positions for user 100, got %d", len(positions))
	}
}

func TestPositionCache_Remove(t *testing.T) {
	pc := NewPositionCache()
	pc.Add(&CachedPosition{ID: 1, UserID: 100})
	pc.Remove(1)

	_, ok := pc.Get(1)
	if ok {
		t.Error("expected position to be removed")
	}

	positions := pc.GetByUser(100)
	if len(positions) != 0 {
		t.Errorf("expected 0 positions after removal, got %d", len(positions))
	}
}

func TestPositionCache_GetAll(t *testing.T) {
	pc := NewPositionCache()
	pc.Add(&CachedPosition{ID: 1, UserID: 100})
	pc.Add(&CachedPosition{ID: 2, UserID: 200})

	all := pc.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 positions, got %d", len(all))
	}
}

func TestPositionCache_ConcurrentAddRemove(t *testing.T) {
	pc := NewPositionCache()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(id int64) {
			defer wg.Done()
			pc.Add(&CachedPosition{ID: id, UserID: 1})
		}(int64(i))
		go func(id int64) {
			defer wg.Done()
			pc.Remove(id)
		}(int64(i))
	}
	wg.Wait()
}

func TestPositionCache_Update(t *testing.T) {
	pc := NewPositionCache()
	pc.Add(&CachedPosition{ID: 1, UserID: 100, TakeProfit: ""})

	pc.Update(1, func(pos *CachedPosition) {
		pos.TakeProfit = "70000"
	})

	got, _ := pc.Get(1)
	if got.TakeProfit != "70000" {
		t.Errorf("expected TakeProfit=70000, got %s", got.TakeProfit)
	}
}
