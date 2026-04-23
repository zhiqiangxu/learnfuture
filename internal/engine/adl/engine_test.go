package adl

import (
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestRankPositions_SortByProfitability(t *testing.T) {
	positions := []*cache.CachedPosition{
		{ID: 1, UserID: 1, Side: 1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"},
		{ID: 2, UserID: 2, Side: 1, Leverage: 10, EntryPrice: "55000", Quantity: "0.01", Margin: "55"},
		{ID: 3, UserID: 3, Side: -1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"}, // wrong side
	}

	// currentPrice=65000, targeting long positions
	ranked := RankPositions(positions, 1, d("65000"))

	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked positions, got %d", len(ranked))
	}

	// Position 2 (entry 55000) has higher profit at 65000 than position 1 (entry 60000)
	if ranked[0].Position.ID != 2 {
		t.Errorf("expected position 2 (most profitable) first, got ID=%d", ranked[0].Position.ID)
	}
}

func TestRankPositions_OnlyProfitablePositions(t *testing.T) {
	positions := []*cache.CachedPosition{
		{ID: 1, Side: 1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"},  // losing
		{ID: 2, Side: 1, Leverage: 10, EntryPrice: "50000", Quantity: "0.01", Margin: "50"},  // winning
	}

	// At price 55000, only position 2 is profitable for long
	ranked := RankPositions(positions, 1, d("55000"))

	if len(ranked) != 1 {
		t.Fatalf("expected 1 ranked position, got %d", len(ranked))
	}
	if ranked[0].Position.ID != 2 {
		t.Error("expected only profitable position")
	}
}

func TestRankPositions_FilterBySide(t *testing.T) {
	positions := []*cache.CachedPosition{
		{ID: 1, Side: 1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"},
		{ID: 2, Side: -1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"},
	}

	// Target short positions at price 55000 (short is profitable)
	ranked := RankPositions(positions, -1, d("55000"))

	if len(ranked) != 1 {
		t.Fatalf("expected 1 ranked position, got %d", len(ranked))
	}
	if ranked[0].Position.ID != 2 {
		t.Error("expected short position")
	}
}

func TestCalcADLQuantity(t *testing.T) {
	// deficit=600, price=60000 -> qty=0.01
	qty := CalcADLQuantity(d("600"), d("60000"))
	if !qty.Equal(d("0.01")) {
		t.Errorf("expected 0.01, got %s", qty)
	}
}

func TestCalcADLQuantity_ZeroPrice(t *testing.T) {
	qty := CalcADLQuantity(d("600"), d("0"))
	if !qty.IsZero() {
		t.Error("expected zero for zero price")
	}
}

func TestExecuteADL_FullyClosed(t *testing.T) {
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: 1, Leverage: 10,
				EntryPrice: "50000", Quantity: "0.01", Margin: "50",
			},
			PnlRatio: d("200"),
			Score:    d("2000"),
		},
	}

	// deficit=1000 at price=60000 -> needs 1000/60000=0.0167 BTC
	// Position only has 0.01, so fully closed
	results, _ := ExecuteADL(ranked, d("1000"), d("60000"))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].FullyClosed {
		t.Error("expected position to be fully closed")
	}
	if !results[0].ReducedQty.Equal(d("0.01")) {
		t.Errorf("expected reduced qty=0.01, got %s", results[0].ReducedQty)
	}
}

func TestExecuteADL_PartiallyClosed(t *testing.T) {
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: 1, Leverage: 10,
				EntryPrice: "50000", Quantity: "1", Margin: "5000",
			},
			PnlRatio: d("200"),
			Score:    d("2000"),
		},
	}

	// deficit=600 at price=60000 -> needs 0.01 BTC, position has 1 BTC
	results, remaining := ExecuteADL(ranked, d("600"), d("60000"))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FullyClosed {
		t.Error("position should not be fully closed")
	}
	if remaining.IsPositive() {
		t.Errorf("deficit should be covered, remaining=%s", remaining)
	}
}

func TestGetADLIndicator(t *testing.T) {
	tests := []struct {
		pnlRatio string
		expected int
	}{
		{"50", 1},    // 50% ROI
		{"150", 2},   // 150% ROI
		{"250", 3},   // 250% ROI
		{"350", 4},   // 350% ROI
		{"450", 5},   // 450% ROI
	}

	for _, tt := range tests {
		got := GetADLIndicator(d(tt.pnlRatio), 10)
		if got != tt.expected {
			t.Errorf("pnlRatio=%s: expected indicator=%d, got %d", tt.pnlRatio, tt.expected, got)
		}
	}
}
