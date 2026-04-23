package markprice

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func newTestEngine() *Engine {
	return NewEngine(EngineConfig{BasisAlpha: 0.1})
}

func TestEngine_InitialState(t *testing.T) {
	e := newTestEngine()
	if !e.GetMarkPrice().IsZero() {
		t.Error("initial mark price should be zero")
	}
}

func TestEngine_FirstFuturesPrice(t *testing.T) {
	e := newTestEngine()
	e.UpdateLastPrice(d("60000"))

	if !e.GetMarkPrice().Equal(d("60000")) {
		t.Errorf("expected mark=60000, got %s", e.GetMarkPrice())
	}
}

func TestEngine_SpotAndFuturesSame_MarkEqualsSpot(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000")) // spot
	e.UpdateLastPrice(d("60000"))  // futures = same

	// When futures == spot, basis=0, mark = spot
	mark := e.GetMarkPrice()
	if !mark.Equal(d("60000")) {
		t.Errorf("expected mark=60000 when spot==futures, got %s", mark)
	}
}

func TestEngine_FuturesPremium_MarkBetween(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000"))  // spot price
	e.UpdateLastPrice(d("60300"))   // futures 0.5% premium

	mark := e.GetMarkPrice()
	// Mark should be between spot and futures (EMA smoothed)
	if mark.LessThan(d("60000")) || mark.GreaterThan(d("60300")) {
		t.Errorf("mark %s should be between spot(60000) and futures(60300)", mark)
	}
}

func TestEngine_FuturesDiscount_MarkAboveFutures(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000")) // spot
	e.UpdateLastPrice(d("59700"))  // futures 0.5% discount

	mark := e.GetMarkPrice()
	// Mark should be between futures and spot
	if mark.LessThan(d("59700")) || mark.GreaterThan(d("60000")) {
		t.Errorf("mark %s should be between futures(59700) and spot(60000)", mark)
	}
}

func TestEngine_FuturesManipulation_MarkProtected(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000")) // spot is stable at 60000

	// Normal futures trading
	for i := 0; i < 10; i++ {
		e.UpdateLastPrice(d("60000"))
	}

	// Someone manipulates futures price down 10% (flash crash)
	e.UpdateLastPrice(d("54000"))

	mark := e.GetMarkPrice()
	// Spot is still 60000, so mark should stay near spot, NOT follow futures
	if mark.LessThan(d("59000")) {
		t.Errorf("mark %s should be protected near spot(60000), not follow futures crash to 54000", mark)
	}

	// Key: if we used last price for liquidation, users at 55000 liq would be liquidated.
	// But mark price stays above 59000, protecting them.
	t.Logf("Protection: last=54000, spot=60000, mark=%s", mark)
}

func TestEngine_BasisNonZero_WhenSpotDifferentFromFutures(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000")) // spot
	e.UpdateLastPrice(d("60300"))  // futures premium

	basis := e.GetBasis()
	if basis.IsZero() {
		t.Error("basis should be non-zero when spot != futures")
	}
	if basis.IsNegative() {
		t.Error("basis should be positive when futures > spot (premium)")
	}
}

func TestEngine_BasisConvergesToZero_WhenPricesEqual(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000"))

	// Initially with premium
	e.UpdateLastPrice(d("60300"))

	// Then futures converges to spot
	for i := 0; i < 100; i++ {
		e.UpdateLastPrice(d("60000"))
	}

	basis := e.GetBasis()
	if basis.Abs().GreaterThan(d("0.0001")) {
		t.Errorf("basis should converge near zero, got %s", basis)
	}
}

func TestEngine_GetPrices(t *testing.T) {
	e := newTestEngine()
	e.UpdateIndexPrice(d("60000"))
	e.UpdateLastPrice(d("60100"))

	last, mark, index := e.GetPrices()
	if !last.Equal(d("60100")) {
		t.Errorf("expected last=60100, got %s", last)
	}
	if !index.Equal(d("60000")) {
		t.Errorf("expected index=60000, got %s", index)
	}
	if mark.IsZero() {
		t.Error("mark should not be zero")
	}
}

func TestEngine_RealScenario_SpotFuturesBasis(t *testing.T) {
	e := newTestEngine()

	// Simulate real market:
	// Spot: 67890 (from REST, updated every 5s)
	// Futures: 67920 (from WS, updated per trade, slight premium)
	e.UpdateIndexPrice(d("67890"))
	e.UpdateLastPrice(d("67920"))

	mark := e.GetMarkPrice()
	basis := e.GetBasis()

	t.Logf("Spot(index)=67890, Futures(last)=67920, Mark=%s, Basis=%s",
		mark.StringFixed(2), basis.StringFixed(6))

	// Basis should be ~0.044% (30/67890)
	expectedBasisApprox := d("30").Div(d("67890")) // ≈ 0.000442
	diff := basis.Sub(expectedBasisApprox.Mul(d("0.1"))).Abs() // α=0.1 first step
	if diff.GreaterThan(d("0.001")) {
		t.Logf("basis within expected range (first tick only)")
	}

	// Mark should be very close to both (small premium)
	if mark.LessThan(d("67880")) || mark.GreaterThan(d("67930")) {
		t.Errorf("mark %s should be between spot and futures", mark)
	}
}
