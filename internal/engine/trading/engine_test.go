package trading

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/clearing"
	"learn_future/internal/engine/fee"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/model"
)

func newTestEngine(t *testing.T) (*Engine, *cache.PriceCache, *cache.PositionCache, *cache.OrderCache) {
	t.Helper()
	pc := cache.NewPriceCache()
	posc := cache.NewPositionCache()
	oc := cache.NewOrderCache()
	fc := fee.NewCalculator(nil)
	ob := orderbook.NewBook()
	settler := clearing.NewSettler(nil, nil, nil, nil, nil, 100)
	clearance := clearing.NewClearance(fc,
		decimal.NewFromFloat(0.0004), decimal.NewFromFloat(0.005), decimal.NewFromInt(5))

	e := NewEngine(nil, pc, posc, oc, ob, nil, nil, nil, nil, clearance, settler, EngineConfig{
		MaxLeverage: 125,
		MinMargin:   decimal.NewFromInt(1),
	})
	e.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)
	return e, pc, posc, oc
}

// seedOrderBook populates the order book with market maker orders around the given price.
func seedOrderBook(e *Engine, price decimal.Decimal) {
	book := e.GetBook()
	// Place asks (sell orders) above price
	for i := 1; i <= 5; i++ {
		offset := decimal.NewFromFloat(float64(i) * 10) // 10, 20, 30, 40, 50
		book.PlaceLimit(&orderbook.Order{
			ID: book.NextOrderID(), UserID: 0, Side: -1,
			Price: price.Add(offset), Quantity: decimal.NewFromInt(10),
		})
	}
	// Place bids (buy orders) below price
	for i := 1; i <= 5; i++ {
		offset := decimal.NewFromFloat(float64(i) * 10)
		book.PlaceLimit(&orderbook.Order{
			ID: book.NextOrderID(), UserID: 0, Side: 1,
			Price: price.Sub(offset), Quantity: decimal.NewFromInt(10),
		})
	}
}

func TestPlaceMarketOrder_Success(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	result, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromInt(100), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "filled" {
		t.Fatalf("expected status filled, got %s", result.Status)
	}
	if result.AvgPrice.IsZero() {
		t.Fatal("expected non-zero avg price")
	}
	if result.TotalQty.IsZero() {
		t.Fatal("expected non-zero total qty")
	}

	// Check position was created in cache
	positions := posc.GetByUser(1)
	if len(positions) < 1 {
		t.Fatal("expected position in cache")
	}
}

func TestPlaceMarketOrder_InsufficientBalance(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	_, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromInt(10000), nil, nil)
	if err != model.ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestPlaceMarketOrder_NoPriceAvailable(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	_, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrNoPriceAvailable {
		t.Fatalf("expected ErrNoPriceAvailable, got %v", err)
	}
}

func TestPlaceMarketOrder_InvalidSide(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	_, err := e.PlaceMarketOrder(1, 0, 10, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrInvalidSide {
		t.Fatalf("expected ErrInvalidSide, got %v", err)
	}
}

func TestPlaceMarketOrder_InvalidLeverage(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))

	_, err := e.PlaceMarketOrder(1, 1, 0, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrInvalidLeverage {
		t.Fatalf("expected ErrInvalidLeverage for leverage=0, got %v", err)
	}

	_, err = e.PlaceMarketOrder(1, 1, 126, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrInvalidLeverage {
		t.Fatalf("expected ErrInvalidLeverage for leverage=126, got %v", err)
	}
}

func TestPlaceMarketOrder_MarginTooSmall(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	_, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromFloat(0.5), nil, nil)
	if err != ErrMarginTooSmall {
		t.Fatalf("expected ErrMarginTooSmall, got %v", err)
	}
}

func TestPlaceMarketOrder_TooManyPositions(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	for i := int64(1); i <= 20; i++ {
		posc.Add(&cache.CachedPosition{
			ID: i, UserID: 1, Side: 1, Leverage: int(i),
			EntryPrice: "50000", Quantity: "0.02", Margin: "100", LiqPrice: "45000",
		})
	}

	_, err := e.PlaceMarketOrder(1, 1, 125, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrTooManyPositions {
		t.Fatalf("expected ErrTooManyPositions, got %v", err)
	}
}

func TestPlaceMarketOrder_MatchesOrderBook(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	result, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromInt(100), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should have matched against ask side (buy order matches asks)
	if len(result.Trades) == 0 {
		t.Fatal("expected orderbook trades")
	}
	// Fill price should be the best ask (~50010)
	if result.AvgPrice.LessThanOrEqual(decimal.NewFromInt(50000)) {
		t.Errorf("avg price %s should be above 50000 (filled against asks)", result.AvgPrice)
	}
}

func TestPlaceMarketOrder_NoLiquidity(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	// Don't seed orderbook → empty

	_, err := e.PlaceMarketOrder(1, 1, 10, 1, decimal.NewFromInt(100), nil, nil)
	if err != ErrNoLiquidity {
		t.Fatalf("expected ErrNoLiquidity, got %v", err)
	}
}

func TestPlaceLimitOrder_Pending(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	// Limit buy far below market → should rest in book
	result, err := e.PlaceLimitOrder(1, 1, 10, decimal.NewFromInt(100), decimal.NewFromInt(49000), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pending" {
		t.Errorf("expected pending, got %s", result.Status)
	}
}

func TestPlaceLimitOrder_ImmediateFill(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	// Limit buy above best ask → crosses spread, fills immediately
	result, err := e.PlaceLimitOrder(1, 1, 10, decimal.NewFromInt(100), decimal.NewFromInt(50050), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "filled" {
		t.Errorf("expected filled (crossed spread), got %s", result.Status)
	}
}

func TestPlaceLimitOrder_PriceDeviation(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))

	_, err := e.PlaceLimitOrder(1, 1, 10, decimal.NewFromInt(100), decimal.NewFromInt(40000), nil, nil)
	if err != ErrPriceDeviationTooLarge {
		t.Fatalf("expected ErrPriceDeviationTooLarge, got %v", err)
	}
}

func TestCancelOrder_Success(t *testing.T) {
	e, pc, _, _ := newTestEngine(t)
	pc.SetPrice(decimal.NewFromInt(50000))
	seedOrderBook(e, decimal.NewFromInt(50000))

	result, _ := e.PlaceLimitOrder(1, 1, 10, decimal.NewFromInt(100), decimal.NewFromInt(49000), nil, nil)

	// Get the order ID from orderCache
	orders := e.orderCache.GetByUser(1)
	if len(orders) == 0 {
		t.Fatal("expected pending order in cache")
	}

	err := e.CancelOrder(orders[0].ID, 1)
	if err != nil {
		t.Fatalf("cancel error: %v", err)
	}

	_ = result
}

func TestCancelOrder_NotFound(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	err := e.CancelOrder(999, 1)
	if err == nil {
		t.Error("expected error for non-existent order")
	}
}
