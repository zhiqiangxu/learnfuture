package cache

import (
	"sync"
	"testing"

	"github.com/shopspring/decimal"
)

func TestAccountCache_LoadAndGet(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.Zero)

	bal := ac.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(10000)) {
		t.Errorf("expected 10000, got %s", bal)
	}
}

func TestAccountCache_GetBalance_WithFrozen(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.NewFromInt(3000))

	// available = 10000 - 3000 = 7000
	bal := ac.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(7000)) {
		t.Errorf("expected 7000, got %s", bal)
	}
}

func TestAccountCache_Deduct_Success(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.Zero)

	ok := ac.Deduct(1, decimal.NewFromInt(3000))
	if !ok {
		t.Error("deduct should succeed")
	}
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(7000)) {
		t.Errorf("expected 7000 after deduct, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_Deduct_Insufficient(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(100), decimal.Zero)

	ok := ac.Deduct(1, decimal.NewFromInt(200))
	if ok {
		t.Error("deduct should fail for insufficient balance")
	}
	// Balance unchanged
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(100)) {
		t.Errorf("balance should be unchanged, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_Deduct_AccountsForFrozen(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.NewFromInt(8000))

	// available = 2000, try to deduct 3000
	ok := ac.Deduct(1, decimal.NewFromInt(3000))
	if ok {
		t.Error("deduct should fail when frozen reduces available below amount")
	}
}

func TestAccountCache_Freeze_Success(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.Zero)

	ok := ac.Freeze(1, decimal.NewFromInt(5000))
	if !ok {
		t.Error("freeze should succeed")
	}
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(5000)) {
		t.Errorf("available should be 5000, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_Freeze_Insufficient(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(1000), decimal.Zero)

	ok := ac.Freeze(1, decimal.NewFromInt(2000))
	if ok {
		t.Error("freeze should fail")
	}
}

func TestAccountCache_Unfreeze(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.NewFromInt(5000))

	ac.Unfreeze(1, decimal.NewFromInt(3000))
	// available = 10000 - (5000-3000) = 8000
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(8000)) {
		t.Errorf("expected 8000, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_ReturnMarginWithPnl(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(9000), decimal.Zero)

	ac.ReturnMarginWithPnl(1, decimal.NewFromInt(1000), decimal.NewFromInt(500))
	// 9000 + 1000 + 500 = 10500
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(10500)) {
		t.Errorf("expected 10500, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_AddPnl_Negative(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.Zero)

	ac.AddPnl(1, decimal.NewFromInt(-500))
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(9500)) {
		t.Errorf("expected 9500, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_Reset(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(500), decimal.NewFromInt(200))

	ac.Reset(1, decimal.NewFromInt(10000))
	if !ac.GetBalance(1).Equal(decimal.NewFromInt(10000)) {
		t.Errorf("expected 10000, got %s", ac.GetBalance(1))
	}
}

func TestAccountCache_NonExistentUser(t *testing.T) {
	ac := NewAccountCache()
	bal := ac.GetBalance(999)
	if !bal.IsZero() {
		t.Errorf("expected 0 for non-existent user, got %s", bal)
	}
}

func TestAccountCache_ConcurrentDeduct(t *testing.T) {
	ac := NewAccountCache()
	ac.Load(1, decimal.NewFromInt(10000), decimal.Zero)

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// 100 goroutines each try to deduct 200 (total 20000 > 10000)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := ac.Deduct(1, decimal.NewFromInt(200))
			if ok {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Exactly 50 should succeed (10000/200)
	if successCount != 50 {
		t.Errorf("expected 50 successful deductions, got %d", successCount)
	}
	if !ac.GetBalance(1).IsZero() {
		t.Errorf("expected 0 balance, got %s", ac.GetBalance(1))
	}
}
