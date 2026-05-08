package marketmaker

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/orderbook"
)

func TestBot_PlacesOrdersOnBothSides(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(60000))

	bot := NewBot(book, pc, Config{
		SpreadBps: 5, Levels: 3, BaseQty: 0.1,
		QtyMultiplier: 1.0, RefreshMs: 100, MoveThreshBps: 1,
	}, nil)

	// Manually trigger tick
	bot.tick()

	// Should have orders on both sides
	bid, bidQty := book.BestBid()
	ask, askQty := book.BestAsk()

	if bid.IsZero() || ask.IsZero() {
		t.Fatal("expected orders on both sides")
	}
	if bidQty.IsZero() || askQty.IsZero() {
		t.Fatal("expected non-zero quantities")
	}

	// Bid should be below ref, ask above ref
	if bid.GreaterThanOrEqual(decimal.NewFromInt(60000)) {
		t.Errorf("bid %s should be below 60000", bid)
	}
	if ask.LessThanOrEqual(decimal.NewFromInt(60000)) {
		t.Errorf("ask %s should be above 60000", ask)
	}

	// Should have 6 orders total (3 levels × 2 sides)
	if book.OrderCount() != 6 {
		t.Errorf("expected 6 orders, got %d", book.OrderCount())
	}
}

func TestBot_RequotesOnPriceMove(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(60000))

	bot := NewBot(book, pc, Config{
		SpreadBps: 5, Levels: 2, BaseQty: 0.1,
		QtyMultiplier: 1.0, RefreshMs: 100, MoveThreshBps: 3,
	}, nil)

	bot.tick()
	firstBid, _ := book.BestBid()

	// Small move (< threshold) → no re-quote
	pc.SetPrice(decimal.NewFromInt(60001))
	bot.tick()
	sameBid, _ := book.BestBid()
	if !sameBid.Equal(firstBid) {
		t.Error("should not re-quote on small price move")
	}

	// Large move (> threshold) → re-quote
	pc.SetPrice(decimal.NewFromInt(61000))
	bot.tick()
	newBid, _ := book.BestBid()
	if newBid.Equal(firstBid) {
		t.Error("should re-quote on large price move")
	}
}

func TestBot_CancelsOldOrdersOnRequote(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(60000))

	bot := NewBot(book, pc, Config{
		SpreadBps: 5, Levels: 2, BaseQty: 0.1,
		QtyMultiplier: 1.0, RefreshMs: 100, MoveThreshBps: 1,
	}, nil)

	bot.tick()
	countAfterFirst := book.OrderCount()

	// Price moves → re-quote
	pc.SetPrice(decimal.NewFromInt(61000))
	bot.tick()
	countAfterSecond := book.OrderCount()

	// Should have same number of orders (old cancelled, new placed)
	if countAfterFirst != countAfterSecond {
		t.Errorf("order count should be same after re-quote: %d vs %d", countAfterFirst, countAfterSecond)
	}
}

func TestBot_NoPriceNoOrders(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache() // price = 0

	bot := NewBot(book, pc, DefaultConfig, nil)
	bot.tick()

	if book.OrderCount() != 0 {
		t.Error("should not place orders without price")
	}
}

func TestBot_StartStop(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(60000))

	bot := NewBot(book, pc, Config{
		SpreadBps: 5, Levels: 2, BaseQty: 0.1,
		QtyMultiplier: 1.0, RefreshMs: 50, MoveThreshBps: 1,
	}, nil)

	bot.Start()
	time.Sleep(200 * time.Millisecond)
	bot.Stop()

	// Should have placed some orders
	if book.OrderCount() == 0 {
		t.Error("expected orders after bot ran")
	}
}

func TestBot_GetStats(t *testing.T) {
	book := orderbook.NewBook()
	pc := cache.NewPriceCache()
	pc.SetPrice(decimal.NewFromInt(60000))

	bot := NewBot(book, pc, Config{
		SpreadBps: 5, Levels: 3, BaseQty: 0.1,
		QtyMultiplier: 1.0, RefreshMs: 100, MoveThreshBps: 1,
	}, nil)

	bot.tick()
	stats := bot.GetStats()

	if stats.RefPrice != "60000.00" {
		t.Errorf("ref price=%s, want 60000.00", stats.RefPrice)
	}
	if stats.ActiveOrders != 6 {
		t.Errorf("active orders=%d, want 6", stats.ActiveOrders)
	}
}
