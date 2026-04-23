package cache

import (
	"sync"
	"testing"

	"github.com/shopspring/decimal"
)

func TestPriceCache_SetAndGet(t *testing.T) {
	pc := NewPriceCache()
	price := decimal.NewFromFloat(67890.12)
	pc.SetPrice(price)

	got := pc.GetPrice()
	if !got.Equal(price) {
		t.Errorf("expected %s, got %s", price, got)
	}
}

func TestPriceCache_InitialZero(t *testing.T) {
	pc := NewPriceCache()
	got := pc.GetPrice()
	if !got.IsZero() {
		t.Errorf("expected zero, got %s", got)
	}
}

func TestPriceCache_ConcurrentReadWrite(t *testing.T) {
	pc := NewPriceCache()
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			pc.SetPrice(decimal.NewFromInt(int64(v)))
		}(i)
		go func() {
			defer wg.Done()
			_ = pc.GetPrice()
		}()
	}
	wg.Wait()
}

func TestPriceCache_TickerHighLow(t *testing.T) {
	pc := NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(100))
	pc.SetPrice(decimal.NewFromInt(200))
	pc.SetPrice(decimal.NewFromInt(150))

	ticker := pc.GetTicker()
	if !ticker.High.Equal(decimal.NewFromInt(200)) {
		t.Errorf("expected high=200, got %s", ticker.High)
	}
	if !ticker.Low.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected low=100, got %s", ticker.Low)
	}
	if !ticker.Price.Equal(decimal.NewFromInt(150)) {
		t.Errorf("expected price=150, got %s", ticker.Price)
	}
}

func TestPriceCache_HighOnlyUpdatesHigher(t *testing.T) {
	pc := NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(200))
	pc.SetPrice(decimal.NewFromInt(100))

	ticker := pc.GetTicker()
	if !ticker.High.Equal(decimal.NewFromInt(200)) {
		t.Errorf("high should stay 200, got %s", ticker.High)
	}
}

func TestPriceCache_LowOnlyUpdatesLower(t *testing.T) {
	pc := NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(100))
	pc.SetPrice(decimal.NewFromInt(200))

	ticker := pc.GetTicker()
	if !ticker.Low.Equal(decimal.NewFromInt(100)) {
		t.Errorf("low should stay 100, got %s", ticker.Low)
	}
}
