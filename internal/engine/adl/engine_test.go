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

	// currentPrice=65000, bankruptcyPrice=61000 (above both entries 55000 and 60000)
	ranked := RankPositions(positions, 1, d("65000"), d("61000"))

	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked positions, got %d", len(ranked))
	}
	if ranked[0].Position.ID != 2 {
		t.Errorf("expected position 2 (most profitable) first, got ID=%d", ranked[0].Position.ID)
	}
}

func TestRankPositions_OnlyProfitablePositions(t *testing.T) {
	positions := []*cache.CachedPosition{
		{ID: 1, Side: 1, Leverage: 10, EntryPrice: "60000", Quantity: "0.01", Margin: "60"}, // losing at market
		{ID: 2, Side: 1, Leverage: 10, EntryPrice: "50000", Quantity: "0.01", Margin: "50"}, // winning
	}

	// At market=55000, bankruptcyPrice=52000 (above entry 50000, so long is still profitable at bankruptcy)
	ranked := RankPositions(positions, 1, d("55000"), d("52000"))

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

	// Target short positions at market=55000, bankruptcyPrice=58000 (short entry 60000, profitable at both)
	ranked := RankPositions(positions, -1, d("55000"), d("58000"))

	if len(ranked) != 1 {
		t.Fatalf("expected 1 ranked position, got %d", len(ranked))
	}
	if ranked[0].Position.ID != 2 {
		t.Error("expected short position")
	}
}

func TestRankPositions_ExcludesUnprofitableAtBankruptcyPrice(t *testing.T) {
	positions := []*cache.CachedPosition{
		// Long @ 53000, profitable at market (55000) but NOT at bankruptcy (52000)
		{ID: 1, Side: 1, Leverage: 10, EntryPrice: "53000", Quantity: "0.01", Margin: "53"},
		// Long @ 40000, profitable at BOTH market and bankruptcy
		{ID: 2, Side: 1, Leverage: 10, EntryPrice: "40000", Quantity: "0.01", Margin: "40"},
	}

	ranked := RankPositions(positions, 1, d("55000"), d("52000"))

	if len(ranked) != 1 {
		t.Fatalf("expected 1 ranked (only profitable at both prices), got %d", len(ranked))
	}
	if ranked[0].Position.ID != 2 {
		t.Error("expected position 2 (profitable at both market and bankruptcy)")
	}
}

func TestExecuteADL_FullyClosed(t *testing.T) {
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: 1, Leverage: 10,
				EntryPrice: "50000", Quantity: "0.01", Margin: "50",
			},
		},
	}

	// Long @ 50000, bankruptcy=45000, market=60000
	// Profit given up per unit = (60000-50000) - (45000-50000) = 10000 - (-5000)
	// Wait, settlement is capped at entry for this case since bankruptcy(45000) < entry(50000)
	// settlementPrice = 50000 (capped), profitGivenUp = (60000-50000)*1 - (50000-50000)*1 = 10000
	// neededQty = 1000 / 10000 = 0.1, but position only has 0.01
	results, remaining := ExecuteADL(ranked, d("1000"), d("45000"), d("60000"))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].FullyClosed {
		t.Error("expected position to be fully closed")
	}
	if !results[0].ReducedQty.Equal(d("0.01")) {
		t.Errorf("expected reduced qty=0.01, got %s", results[0].ReducedQty)
	}
	if !remaining.IsPositive() {
		t.Error("deficit should not be fully covered with only 0.01 BTC")
	}
}

func TestExecuteADL_PartiallyClosed(t *testing.T) {
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: -1, Leverage: 10,
				EntryPrice: "60000", Quantity: "1", Margin: "6000",
			},
		},
	}

	// Short @ 60000, bankruptcy=66000, market=53000
	// settlementPrice = min(66000, 60000) = 60000 (capped at entry)
	// marketPnlPerUnit = (53000-60000)*(-1) = 7000
	// settlePnlPerUnit = (60000-60000)*(-1) = 0
	// profitGivenUp = 7000 - 0 = 7000
	// neededQty = 600 / 7000 ≈ 0.0857
	results, remaining := ExecuteADL(ranked, d("600"), d("66000"), d("53000"))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FullyClosed {
		t.Error("position should not be fully closed")
	}
	if remaining.GreaterThan(d("0.01")) {
		t.Errorf("deficit should be roughly covered, remaining=%s", remaining)
	}
	// Counterparty's realized PnL should be 0 (settled at entry price)
	if !results[0].RealizedPnl.IsZero() {
		t.Errorf("expected 0 realized PnL (settled at entry), got %s", results[0].RealizedPnl)
	}
}

func TestExecuteADL_NeverCreatesLoss(t *testing.T) {
	// Counterparty long @ 53500, market @ 53000 (barely profitable)
	// Bankruptcy price @ 54000 — ABOVE entry, would cause loss without capping
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: -1, Leverage: 10,
				EntryPrice: "53500", Quantity: "0.1", Margin: "535",
			},
		},
	}

	// Short @ 53500, market=55000 (losing), bankruptcy=54000
	// upnl at market = (53500-55000)*(-1)*0.1 = -150 (losing!)
	// This position should not be ranked (filtered in RankPositions)
	// But if it somehow gets through, ExecuteADL should skip it

	results, _ := ExecuteADL(ranked, d("100"), d("54000"), d("55000"))

	// Should produce no results because settlement can't extract profit
	for _, r := range results {
		if r.RealizedPnl.IsNegative() {
			t.Errorf("ADL created a loss for counterparty: pnl=%s", r.RealizedPnl)
		}
	}
}

func TestExecuteADL_MultiplePositions(t *testing.T) {
	// Two short counterparties, deficit needs both to cover
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: -1, Leverage: 10,
				EntryPrice: "60000", Quantity: "0.01", Margin: "60",
			},
		},
		{
			Position: &cache.CachedPosition{
				ID: 2, UserID: 2, Side: -1, Leverage: 10,
				EntryPrice: "60000", Quantity: "0.01", Margin: "60",
			},
		},
	}

	// Short @ 60000, market=53000, bankruptcy=58000
	// settlement capped at 58000 (< entry 60000, so OK for short)
	// marketPnlPerUnit = (53000-60000)*(-1) = 7000
	// settlePnlPerUnit = (58000-60000)*(-1) = 2000
	// profitGivenUp = 7000-2000 = 5000 per unit
	// deficit=80, neededQty = 80/5000 = 0.016
	// Position 1 has 0.01, covers 0.01*5000=50, remaining=30
	// Position 2 has 0.01, covers 30/5000=0.006
	results, remaining := ExecuteADL(ranked, d("80"), d("58000"), d("53000"))

	if len(results) != 2 {
		t.Fatalf("expected 2 results (deficit needs both positions), got %d", len(results))
	}
	if remaining.GreaterThan(d("0.01")) {
		t.Errorf("deficit should be covered, remaining=%s", remaining)
	}
	// First position fully closed
	if !results[0].FullyClosed {
		t.Error("first position should be fully closed")
	}
	// Second position partially closed
	if results[1].FullyClosed {
		t.Error("second position should be partially closed")
	}
}

func TestExecuteADL_DeficitNotFullyCovered(t *testing.T) {
	// Only one small position, can't cover full deficit
	ranked := []*RankedPosition{
		{
			Position: &cache.CachedPosition{
				ID: 1, UserID: 1, Side: -1, Leverage: 10,
				EntryPrice: "60000", Quantity: "0.001", Margin: "6",
			},
		},
	}

	// deficit=1000, but position can only cover 0.001 * 5000 = 5
	results, remaining := ExecuteADL(ranked, d("1000"), d("58000"), d("53000"))

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !remaining.IsPositive() {
		t.Error("remaining deficit should be positive (not fully covered)")
	}
	if !results[0].FullyClosed {
		t.Error("position should be fully closed (exhausted)")
	}
}

func TestGetADLIndicator(t *testing.T) {
	tests := []struct {
		pnlRatio string
		expected int
	}{
		{"50", 1},
		{"150", 2},
		{"250", 3},
		{"350", 4},
		{"450", 5},
	}

	for _, tt := range tests {
		got := GetADLIndicator(d(tt.pnlRatio), 10)
		if got != tt.expected {
			t.Errorf("pnlRatio=%s: expected indicator=%d, got %d", tt.pnlRatio, tt.expected, got)
		}
	}
}
