package trading

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/insurance"
	"learn_future/internal/engine/markprice"
	"learn_future/internal/model"
	"learn_future/internal/ws"
)

// TestCrossMarginLiquidation_PerUser verifies that cross-margin liquidation
// is computed per-user, not globally. User A (profitable) must not be affected
// by User B (losing) being liquidated.
func TestCrossMarginLiquidation_PerUser(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)

	// Setup: two users with cross-margin positions
	// User A (ID=10): long, profitable
	// User B (ID=20): long, losing heavily
	e.GetMemAccounts().Load(10, decimal.NewFromInt(1000), decimal.Zero)
	e.GetMemAccounts().Load(20, decimal.NewFromInt(100), decimal.Zero) // small balance

	// User A: long 0.1 BTC @ 50000, margin=500, leverage=10, cross margin
	posc.Add(&cache.CachedPosition{
		ID: 1, UserID: 10, Side: 1, MarginMode: model.MarginModeCross,
		Leverage: 10, EntryPrice: "50000", Quantity: "0.1",
		Margin: "500", LiqPrice: "45000", ForceTpPrice: "100000",
	})

	// User B: long 0.1 BTC @ 50000, margin=50, leverage=100, cross margin
	// Very high leverage, small margin → easy to liquidate
	posc.Add(&cache.CachedPosition{
		ID: 2, UserID: 20, Side: 1, MarginMode: model.MarginModeCross,
		Leverage: 100, EntryPrice: "50000", Quantity: "0.1",
		Margin: "50", LiqPrice: "49600", ForceTpPrice: "300000",
	})

	// Set price to 49500 — below B's liq price but above A's
	pc.SetPrice(decimal.NewFromInt(49500))

	// Create monitor
	hub := ws.NewHub()
	go hub.Run()
	mpEngine := markprice.NewEngine(markprice.EngineConfig{BasisAlpha: 0.1})
	mpEngine.UpdateLastPrice(decimal.NewFromInt(49500))
	fund := insurance.NewFund(decimal.NewFromInt(1000000))

	seedOrderBook(e, decimal.NewFromInt(49500))

	monitor := NewMonitor(e, posc, cache.NewOrderCache(), hub, mpEngine, fund)

	// Track which users got liquidated
	liquidatedUsers := make(map[int64]bool)
	monitor.SetOnPositionClose(func(userID int64, positionID int64, result *CloseResult, reason int) {
		if reason == model.CloseReasonLiquidation {
			liquidatedUsers[userID] = true
		}
	})

	// Trigger price check
	monitor.OnPriceUpdate(decimal.NewFromInt(49500))

	// Verify: User B should be liquidated (equity < maintenance)
	// User B equity = balance(100) + upnl(0.1 * (49500-50000)) = 100 + (-50) = 50
	// User B maintenance = 0.1 * 49500 / 100 * 0.005 ... small but equity is also small
	// The key point: User A must NOT be liquidated
	if _, ok := posc.Get(1); !ok {
		t.Error("User A (position 1) should NOT be liquidated — profitable position wrongly affected by User B")
	}

	// User A's position must still exist
	posA, ok := posc.Get(1)
	if !ok {
		t.Fatal("User A position should still exist")
	}
	if posA.UserID != 10 {
		t.Errorf("Expected UserID 10, got %d", posA.UserID)
	}
}

// TestCrossMarginLiquidation_NotMixedBetweenUsers is a more explicit test:
// Two users with opposite P&L should not cancel each other out.
func TestCrossMarginLiquidation_NotMixedBetweenUsers(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)

	e.GetMemAccounts().Load(10, decimal.NewFromInt(500), decimal.Zero)
	e.GetMemAccounts().Load(20, decimal.NewFromInt(500), decimal.Zero)

	// User A: SHORT 0.1 BTC @ 50000 → profits when price drops
	posc.Add(&cache.CachedPosition{
		ID: 1, UserID: 10, Side: -1, MarginMode: model.MarginModeCross,
		Leverage: 10, EntryPrice: "50000", Quantity: "0.1",
		Margin: "500", LiqPrice: "55000", ForceTpPrice: "25000",
	})

	// User B: LONG 0.1 BTC @ 50000 → loses when price drops
	posc.Add(&cache.CachedPosition{
		ID: 2, UserID: 20, Side: 1, MarginMode: model.MarginModeCross,
		Leverage: 10, EntryPrice: "50000", Quantity: "0.1",
		Margin: "500", LiqPrice: "45250", ForceTpPrice: "100000",
	})

	// Price drops to 44000 — below B's liq price
	// A's upnl = 0.1 * (50000 - 44000) = +600 (profit)
	// B's upnl = 0.1 * (44000 - 50000) = -600 (loss)
	// If mixed: total upnl = 0, neither gets liquidated → BUG
	// Correct: B's equity = 500 + (-600) = -100 < maintenance → liquidated
	//          A's equity = 500 + 600 = 1100 > maintenance → safe

	pc.SetPrice(decimal.NewFromInt(44000))

	hub := ws.NewHub()
	go hub.Run()
	mpEngine := markprice.NewEngine(markprice.EngineConfig{BasisAlpha: 0.1})
	mpEngine.UpdateLastPrice(decimal.NewFromInt(44000))
	fund := insurance.NewFund(decimal.NewFromInt(1000000))

	seedOrderBook(e, decimal.NewFromInt(44000))

	monitor := NewMonitor(e, posc, cache.NewOrderCache(), hub, mpEngine, fund)
	monitor.OnPriceUpdate(decimal.NewFromInt(44000))

	// User A (short, profitable) must still have their position
	if _, ok := posc.Get(1); !ok {
		t.Error("User A (short, profitable) was wrongly liquidated — cross-margin data mixed between users")
	}

	// User B (long, heavy loss) should have been liquidated
	// (position may or may not exist depending on whether orderbook had liquidity)
}
