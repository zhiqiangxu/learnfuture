package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestBook_LimitOrder_RestInBook(t *testing.T) {
	book := NewBook()

	// Place buy limit at 60000, no asks to match → rests in book
	order := &Order{ID: 1, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("0.1")}
	trades, remaining := book.PlaceLimit(order)

	if len(trades) != 0 {
		t.Error("no trades expected")
	}
	if remaining == nil {
		t.Error("order should rest in book")
	}

	bid, qty := book.BestBid()
	if !bid.Equal(d("60000")) {
		t.Errorf("best bid=%s, want 60000", bid)
	}
	if !qty.Equal(d("0.1")) {
		t.Errorf("bid qty=%s, want 0.1", qty)
	}
}

func TestBook_MarketOrder_MatchesBestPrice(t *testing.T) {
	book := NewBook()

	// Place sell limit (ask) at 60100
	book.PlaceLimit(&Order{ID: 1, UserID: 10, Side: -1, Price: d("60100"), Quantity: d("0.5")})
	// Place sell limit (ask) at 60200
	book.PlaceLimit(&Order{ID: 2, UserID: 11, Side: -1, Price: d("60200"), Quantity: d("0.5")})

	// Market buy 0.3 BTC → should match at best ask 60100
	trades := book.PlaceMarket(&Order{ID: 3, UserID: 1, Side: 1, Quantity: d("0.3")})

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if !trades[0].Price.Equal(d("60100")) {
		t.Errorf("trade price=%s, want 60100 (maker's price)", trades[0].Price)
	}
	if !trades[0].Quantity.Equal(d("0.3")) {
		t.Errorf("trade qty=%s, want 0.3", trades[0].Quantity)
	}
}

func TestBook_MarketOrder_EatsMultipleLevels(t *testing.T) {
	book := NewBook()

	// Ask side: 0.2 @ 60100, 0.3 @ 60200
	book.PlaceLimit(&Order{ID: 1, UserID: 10, Side: -1, Price: d("60100"), Quantity: d("0.2")})
	book.PlaceLimit(&Order{ID: 2, UserID: 11, Side: -1, Price: d("60200"), Quantity: d("0.3")})

	// Market buy 0.4 → eats 0.2@60100 + 0.2@60200
	trades := book.PlaceMarket(&Order{ID: 3, UserID: 1, Side: 1, Quantity: d("0.4")})

	if len(trades) != 2 {
		t.Fatalf("expected 2 trades (2 price levels), got %d", len(trades))
	}
	if !trades[0].Price.Equal(d("60100")) || !trades[0].Quantity.Equal(d("0.2")) {
		t.Errorf("trade[0]: price=%s qty=%s", trades[0].Price, trades[0].Quantity)
	}
	if !trades[1].Price.Equal(d("60200")) || !trades[1].Quantity.Equal(d("0.2")) {
		t.Errorf("trade[1]: price=%s qty=%s", trades[1].Price, trades[1].Quantity)
	}

	// Ask side should have 0.1 remaining at 60200
	ask, qty := book.BestAsk()
	if !ask.Equal(d("60200")) {
		t.Errorf("best ask=%s, want 60200", ask)
	}
	if !qty.Equal(d("0.1")) {
		t.Errorf("remaining ask qty=%s, want 0.1", qty)
	}
}

func TestBook_LimitOrder_ImmediateMatch(t *testing.T) {
	book := NewBook()

	// Sell limit at 60000 (ask)
	book.PlaceLimit(&Order{ID: 1, UserID: 10, Side: -1, Price: d("60000"), Quantity: d("0.5")})

	// Buy limit at 60100 (crosses the spread → immediate match)
	trades, remaining := book.PlaceLimit(&Order{ID: 2, UserID: 1, Side: 1, Price: d("60100"), Quantity: d("0.3")})

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	// Executes at maker's price (60000), not taker's (60100)
	if !trades[0].Price.Equal(d("60000")) {
		t.Errorf("should execute at maker price 60000, got %s", trades[0].Price)
	}
	if remaining != nil {
		t.Error("buy fully filled, should not rest")
	}
}

func TestBook_PricePriority(t *testing.T) {
	book := NewBook()

	// Two bids at different prices
	book.PlaceLimit(&Order{ID: 1, UserID: 10, Side: 1, Price: d("59900"), Quantity: d("1")})
	book.PlaceLimit(&Order{ID: 2, UserID: 11, Side: 1, Price: d("60000"), Quantity: d("1")})

	// Best bid should be 60000 (higher = better for buyer)
	bid, _ := book.BestBid()
	if !bid.Equal(d("60000")) {
		t.Errorf("best bid=%s, want 60000", bid)
	}

	// Sell market → matches best bid (60000) first
	trades := book.PlaceMarket(&Order{ID: 3, UserID: 1, Side: -1, Quantity: d("0.5")})
	if !trades[0].Price.Equal(d("60000")) {
		t.Errorf("should match best bid 60000 first, got %s", trades[0].Price)
	}
}

func TestBook_TimePriority(t *testing.T) {
	book := NewBook()

	// Two bids at same price, different times
	book.PlaceLimit(&Order{ID: 1, UserID: 10, Side: 1, Price: d("60000"), Quantity: d("1")})
	book.PlaceLimit(&Order{ID: 2, UserID: 11, Side: 1, Price: d("60000"), Quantity: d("1")})

	// Sell → should match order 1 first (earlier)
	trades := book.PlaceMarket(&Order{ID: 3, UserID: 1, Side: -1, Quantity: d("0.5")})
	if trades[0].BuyOrderID != 1 {
		t.Errorf("should match order 1 first (time priority), matched %d", trades[0].BuyOrderID)
	}
}

func TestBook_Cancel(t *testing.T) {
	book := NewBook()

	book.PlaceLimit(&Order{ID: 1, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("1")})

	cancelled := book.Cancel(1)
	if cancelled == nil {
		t.Fatal("expected order to be cancelled")
	}

	bid, _ := book.BestBid()
	if !bid.IsZero() {
		t.Errorf("book should be empty after cancel, bid=%s", bid)
	}
}

func TestBook_Cancel_NonExistent(t *testing.T) {
	book := NewBook()
	cancelled := book.Cancel(999)
	if cancelled != nil {
		t.Error("should return nil for non-existent order")
	}
}

func TestBook_Depth(t *testing.T) {
	book := NewBook()

	book.PlaceLimit(&Order{ID: 1, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("1")})
	book.PlaceLimit(&Order{ID: 2, UserID: 2, Side: 1, Price: d("59900"), Quantity: d("2")})
	book.PlaceLimit(&Order{ID: 3, UserID: 3, Side: -1, Price: d("60100"), Quantity: d("0.5")})
	book.PlaceLimit(&Order{ID: 4, UserID: 4, Side: -1, Price: d("60200"), Quantity: d("0.3")})

	depth := book.Depth(5)

	if len(depth.Bids) != 2 {
		t.Errorf("expected 2 bid levels, got %d", len(depth.Bids))
	}
	if len(depth.Asks) != 2 {
		t.Errorf("expected 2 ask levels, got %d", len(depth.Asks))
	}
	if depth.Bids[0].Price != "60000" {
		t.Errorf("best bid should be 60000, got %s", depth.Bids[0].Price)
	}
	if depth.Asks[0].Price != "60100" {
		t.Errorf("best ask should be 60100, got %s", depth.Asks[0].Price)
	}
}

func TestBook_Spread(t *testing.T) {
	book := NewBook()

	book.PlaceLimit(&Order{ID: 1, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("1")})
	book.PlaceLimit(&Order{ID: 2, UserID: 2, Side: -1, Price: d("60050"), Quantity: d("1")})

	spread := book.Spread()
	if !spread.Equal(d("50")) {
		t.Errorf("spread=%s, want 50", spread)
	}
}

func TestBook_NoLiquidity_MarketOrder(t *testing.T) {
	book := NewBook()

	// Market buy with empty ask side → no trades
	trades := book.PlaceMarket(&Order{ID: 1, UserID: 1, Side: 1, Quantity: d("1")})
	if len(trades) != 0 {
		t.Error("no trades expected with empty book")
	}
}

func TestBook_SelfTrade_Allowed(t *testing.T) {
	book := NewBook()

	// Same user places both sides (in real exchange this might be prevented)
	book.PlaceLimit(&Order{ID: 1, UserID: 1, Side: -1, Price: d("60000"), Quantity: d("1")})
	trades, _ := book.PlaceLimit(&Order{ID: 2, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("0.5")})

	// For simulation, self-trade is fine (market maker bot trades with users)
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
}

func TestBook_OrderCount(t *testing.T) {
	book := NewBook()
	book.PlaceLimit(&Order{ID: 1, UserID: 1, Side: 1, Price: d("60000"), Quantity: d("1")})
	book.PlaceLimit(&Order{ID: 2, UserID: 2, Side: -1, Price: d("60100"), Quantity: d("1")})

	if book.OrderCount() != 2 {
		t.Errorf("expected 2 orders, got %d", book.OrderCount())
	}
}
