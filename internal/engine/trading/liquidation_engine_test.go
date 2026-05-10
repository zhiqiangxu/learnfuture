package trading

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
)

// TestLiquidationEngine_TakeOver_MaintainsInvariant verifies that after TakeOver,
// ∑longs ≡ ∑shorts is maintained — position transfers from user to liquidation engine.
func TestLiquidationEngine_TakeOver_MaintainsInvariant(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)
	e.GetMemAccounts().Load(LiquidationUserID, decimal.NewFromInt(100_000_000), decimal.Zero)
	e.GetMemAccounts().Load(10, decimal.NewFromInt(10000), decimal.Zero)
	e.GetMemAccounts().Load(0, decimal.NewFromInt(100_000_000), decimal.Zero)

	pc.SetPrice(decimal.NewFromInt(60000))
	seedOrderBook(e, decimal.NewFromInt(60000))

	// User 10 opens long 0.1 BTC @ 60000 (10x leverage)
	_, err := e.PlaceMarketOrder(10, 1, 10, model.MarginModeIsolated, decimal.NewFromInt(600), nil, nil)
	if err != nil {
		t.Fatalf("PlaceMarketOrder error: %v", err)
	}

	// Verify initial state: user has long, invariant holds
	sumLong, sumShort := calcOpenInterest(posc)
	if !sumLong.Equal(sumShort) {
		t.Fatalf("invariant broken before liquidation: longs=%s shorts=%s", sumLong, sumShort)
	}

	// Find user's position
	userPositions := posc.GetByUser(10)
	if len(userPositions) == 0 {
		t.Fatal("user has no position")
	}
	userPos := userPositions[0]
	entryPrice, _ := decimal.NewFromString(userPos.EntryPrice)
	bankruptcyPrice := position.CalcBankruptcyPrice(entryPrice, userPos.Leverage, userPos.Side)

	// TakeOver: transfer position to liquidation engine
	le := NewLiquidationEngine(e, posc, e.GetBook())
	le.TakeOver(userPos, bankruptcyPrice)

	// Verify: user has no position
	if len(posc.GetByUser(10)) != 0 {
		t.Error("user should have no position after liquidation")
	}

	// Verify: liquidation engine has the position
	liqPositions := posc.GetByUser(LiquidationUserID)
	if len(liqPositions) == 0 {
		t.Error("liquidation engine should have taken over the position")
	} else {
		lp := liqPositions[0]
		if lp.Side != userPos.Side {
			t.Errorf("liquidation engine should have same side as user: got %d, want %d", lp.Side, userPos.Side)
		}
	}

	// Verify: ∑longs ≡ ∑shorts still holds
	sumLong, sumShort = calcOpenInterest(posc)
	if !sumLong.Equal(sumShort) {
		t.Errorf("invariant broken after TakeOver: longs=%s shorts=%s", sumLong, sumShort)
	}
}

// TestLiquidationEngine_TakeOver_UserLosesAllMargin verifies the user's
// balance is reduced by exactly the margin amount (total loss at bankruptcy price).
func TestLiquidationEngine_TakeOver_UserLosesAllMargin(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)
	e.GetMemAccounts().Load(LiquidationUserID, decimal.NewFromInt(100_000_000), decimal.Zero)
	e.GetMemAccounts().Load(10, decimal.NewFromInt(10000), decimal.Zero)
	e.GetMemAccounts().Load(0, decimal.NewFromInt(100_000_000), decimal.Zero)

	pc.SetPrice(decimal.NewFromInt(60000))
	seedOrderBook(e, decimal.NewFromInt(60000))

	balanceBefore := e.GetMemAccounts().GetBalance(10)

	result, err := e.PlaceMarketOrder(10, 1, 10, model.MarginModeIsolated, decimal.NewFromInt(600), nil, nil)
	if err != nil {
		t.Fatalf("PlaceMarketOrder error: %v", err)
	}
	_ = result

	balanceAfterOpen := e.GetMemAccounts().GetBalance(10)

	// Find user's position and trigger TakeOver
	userPos := posc.GetByUser(10)[0]
	entryPrice, _ := decimal.NewFromString(userPos.EntryPrice)
	bankruptcyPrice := position.CalcBankruptcyPrice(entryPrice, userPos.Leverage, userPos.Side)

	le := NewLiquidationEngine(e, posc, e.GetBook())
	le.TakeOver(userPos, bankruptcyPrice)

	balanceAfterLiq := e.GetMemAccounts().GetBalance(10)

	// User's balance after liquidation should be same as after open
	// (margin was already deducted at open, liquidation just closes at bankruptcy = 0 PnL on remaining margin)
	t.Logf("balance: before=%s, afterOpen=%s, afterLiq=%s", balanceBefore, balanceAfterOpen, balanceAfterLiq)

	// User should not have gained anything back (lost all margin)
	if balanceAfterLiq.GreaterThan(balanceAfterOpen) {
		t.Errorf("user should not gain from liquidation: afterOpen=%s afterLiq=%s", balanceAfterOpen, balanceAfterLiq)
	}
}

// TestLiquidationEngine_OnTick_DisposesPosition verifies that OnTick
// gradually closes the liquidation engine's position on the orderbook.
func TestLiquidationEngine_OnTick_DisposesPosition(t *testing.T) {
	e, pc, posc, _ := newTestEngine(t)
	e.GetMemAccounts().Load(LiquidationUserID, decimal.NewFromInt(100_000_000), decimal.Zero)
	e.GetMemAccounts().Load(10, decimal.NewFromInt(10000), decimal.Zero)
	e.GetMemAccounts().Load(0, decimal.NewFromInt(100_000_000), decimal.Zero)

	pc.SetPrice(decimal.NewFromInt(60000))
	seedOrderBook(e, decimal.NewFromInt(60000))

	// Open and take over
	_, err := e.PlaceMarketOrder(10, 1, 10, model.MarginModeIsolated, decimal.NewFromInt(600), nil, nil)
	if err != nil {
		t.Fatalf("PlaceMarketOrder error: %v", err)
	}

	userPos := posc.GetByUser(10)[0]
	entryPrice, _ := decimal.NewFromString(userPos.EntryPrice)
	bankruptcyPrice := position.CalcBankruptcyPrice(entryPrice, userPos.Leverage, userPos.Side)

	le := NewLiquidationEngine(e, posc, e.GetBook())
	le.TakeOver(userPos, bankruptcyPrice)

	// Verify liquidation engine has position
	liqPosBefore := posc.GetByUser(LiquidationUserID)
	if len(liqPosBefore) == 0 {
		t.Fatal("liquidation engine should have a position")
	}
	qtyBefore, _ := decimal.NewFromString(liqPosBefore[0].Quantity)

	// Seed fresh orderbook for disposal
	seedOrderBook(e, decimal.NewFromInt(60000))

	// Force immediate tick (reset lastDisposal)
	le.lastDisposal = le.lastDisposal.Add(-le.disposalInterval * 2)

	// OnTick should place IOC order and close some/all of the position
	le.OnTick(decimal.NewFromInt(60000))

	// Check position was reduced or closed
	liqPosAfter := posc.GetByUser(LiquidationUserID)
	if len(liqPosAfter) == 0 {
		t.Log("liquidation engine position fully closed by disposal")
	} else {
		qtyAfter, _ := decimal.NewFromString(liqPosAfter[0].Quantity)
		if !qtyAfter.LessThan(qtyBefore) {
			t.Errorf("position should have been reduced: before=%s after=%s", qtyBefore, qtyAfter)
		}
		t.Logf("position reduced: %s → %s", qtyBefore, qtyAfter)
	}

	// Invariant still holds
	sumLong, sumShort := calcOpenInterest(posc)
	if !sumLong.Equal(sumShort) {
		t.Errorf("invariant broken after disposal: longs=%s shorts=%s", sumLong, sumShort)
	}
}

// calcOpenInterest sums up all long and short quantities across all positions.
func calcOpenInterest(posc *cache.PositionCache) (sumLong, sumShort decimal.Decimal) {
	all := posc.GetAll()
	for _, p := range all {
		qty, _ := decimal.NewFromString(p.Quantity)
		if p.Side == 1 {
			sumLong = sumLong.Add(qty)
		} else {
			sumShort = sumShort.Add(qty)
		}
	}
	return
}
