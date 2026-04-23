package logic

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/engine/orderbook"
	"learn_future/internal/svc"
	"learn_future/internal/types"
	"learn_future/testutil"
)

func seedBook(svcCtx *svc.ServiceContext, price decimal.Decimal) {
	book := svcCtx.TradingEngine.GetBook()
	for i := 1; i <= 5; i++ {
		offset := decimal.NewFromFloat(float64(i) * 10)
		book.PlaceLimit(&orderbook.Order{
			ID: book.NextOrderID(), UserID: 0, Side: -1,
			Price: price.Add(offset), Quantity: decimal.NewFromInt(10),
		})
		book.PlaceLimit(&orderbook.Order{
			ID: book.NextOrderID(), UserID: 0, Side: 1,
			Price: price.Sub(offset), Quantity: decimal.NewFromInt(10),
		})
	}
}

func TestTradeLogic_PlaceOrder_InvalidMargin(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)

	l := NewTradeLogic(svcCtx)
	_, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 1,
		Leverage:  10,
		Margin:    "0.5", // below MinMargin=1
	})
	if err == nil {
		t.Error("expected error for margin too small")
	}
}

func TestTradeLogic_PlaceOrder_InvalidSide(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)

	l := NewTradeLogic(svcCtx)
	_, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      0, // invalid
		OrderType: 1,
		Leverage:  10,
		Margin:    "100",
	})
	if err == nil {
		t.Error("expected error for invalid side")
	}
}

func TestTradeLogic_PlaceOrder_BadMarginFormat(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	l := NewTradeLogic(svcCtx)
	_, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 1,
		Leverage:  10,
		Margin:    "not_a_number",
	})
	if err == nil {
		t.Error("expected error for bad margin format")
	}
}

func TestTradeLogic_PlaceMarketOrder_Success(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)
	seedBook(svcCtx, decimal.NewFromInt(60000))

	l := NewTradeLogic(svcCtx)
	resp, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 1,
		Leverage:  10,
		Margin:    "100",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "filled" {
		t.Errorf("expected status=filled, got %s", resp.Status)
	}
}

func TestTradeLogic_PlaceMarketOrder_InsufficientBalance(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(50), decimal.Zero)
	seedBook(svcCtx, decimal.NewFromInt(60000))

	l := NewTradeLogic(svcCtx)
	_, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 1,
		Leverage:  10,
		Margin:    "100", // need 100 + fee > 50 available
	})
	if err == nil {
		t.Error("expected insufficient balance error")
	}
}

func TestTradeLogic_PlaceLimitOrder_Success(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)
	seedBook(svcCtx, decimal.NewFromInt(60000))

	l := NewTradeLogic(svcCtx)
	resp, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 2,
		Leverage:  10,
		Margin:    "100",
		Price:     "59000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "pending" {
		t.Errorf("expected status=pending, got %s", resp.Status)
	}
}

func TestTradeLogic_PlaceLimitOrder_PriceDeviationTooLarge(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromInt(60000))
	svcCtx.TradingEngine.GetMemAccounts().Load(1, decimal.NewFromInt(10000), decimal.Zero)
	seedBook(svcCtx, decimal.NewFromInt(60000))

	l := NewTradeLogic(svcCtx)
	_, _, err := l.PlaceOrder(1, &types.PlaceOrderReq{
		Side:      1,
		OrderType: 2,
		Leverage:  10,
		Margin:    "100",
		Price:     "50000", // >10% deviation
	})
	if err == nil {
		t.Error("expected price deviation error")
	}
}

func TestTradeLogic_CancelOrder_NotFound(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	l := NewTradeLogic(svcCtx)
	err := l.CancelOrder(1, 999)
	if err == nil {
		t.Error("expected error for non-existent order")
	}
}

func TestTradeLogic_getTutorialCard_FirstOrder(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	// With nil TutorialModel, GetCompleted will fail, but getTutorialCard handles it
	l := NewTradeLogic(svcCtx)

	req := &types.PlaceOrderReq{Side: 1, OrderType: 1, Leverage: 10, Margin: "100"}
	card := l.getTutorialCard(1, req)
	// With nil TutorialModel, completed map is nil → all topics "not completed"
	// First topic should be perpetual_intro
	if card != nil && card.ID != "perpetual_intro" {
		t.Errorf("expected perpetual_intro tutorial, got %s", card.ID)
	}
}
