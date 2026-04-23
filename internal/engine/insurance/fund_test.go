package insurance

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestFund_InitialBalance(t *testing.T) {
	f := NewFund(d("10000"))
	if !f.GetBalance().Equal(d("10000")) {
		t.Errorf("expected 10000, got %s", f.GetBalance())
	}
}

func TestFund_Contribute(t *testing.T) {
	f := NewFund(d("10000"))
	f.Contribute(d("500"))
	if !f.GetBalance().Equal(d("10500")) {
		t.Errorf("expected 10500, got %s", f.GetBalance())
	}
}

func TestFund_ContributeNegative(t *testing.T) {
	f := NewFund(d("10000"))
	f.Contribute(d("-100"))
	if !f.GetBalance().Equal(d("10000")) {
		t.Error("negative contribute should be ignored")
	}
}

func TestFund_CoverFullyCovered(t *testing.T) {
	f := NewFund(d("10000"))
	covered, needADL := f.Cover(d("500"))

	if !covered.Equal(d("500")) {
		t.Errorf("expected covered=500, got %s", covered)
	}
	if needADL {
		t.Error("should not need ADL")
	}
	if !f.GetBalance().Equal(d("9500")) {
		t.Errorf("expected balance=9500, got %s", f.GetBalance())
	}
}

func TestFund_CoverPartialNeedsADL(t *testing.T) {
	f := NewFund(d("100"))
	covered, needADL := f.Cover(d("500"))

	if !covered.Equal(d("100")) {
		t.Errorf("expected covered=100, got %s", covered)
	}
	if !needADL {
		t.Error("should need ADL")
	}
	if !f.GetBalance().IsZero() {
		t.Errorf("expected balance=0, got %s", f.GetBalance())
	}
}

func TestFund_CoverZeroDeficit(t *testing.T) {
	f := NewFund(d("10000"))
	covered, needADL := f.Cover(d("0"))
	if !covered.IsZero() {
		t.Error("zero deficit should return zero covered")
	}
	if needADL {
		t.Error("zero deficit should not need ADL")
	}
}

func TestCalcLiquidationSurplus_LongSurplus(t *testing.T) {
	// Long: entry=60000, lev=10
	// bankruptcyPrice = 60000*(1-1/10) = 54000
	// liqPrice = 60000*(1-1/10+0.005) = 54300
	// surplus = (54300-54000)*qty*1 = 300*qty
	qty := d("0.01")
	surplus := CalcLiquidationSurplus(d("60000"), d("54300"), qty, 1, 10)
	expected := d("300").Mul(qty) // 3.0
	if !surplus.Equal(expected) {
		t.Errorf("expected surplus=%s, got %s", expected, surplus)
	}
}

func TestCalcLiquidationSurplus_ShortSurplus(t *testing.T) {
	// Short: entry=60000, lev=10
	// bankruptcyPrice = 60000*(1+1/10) = 66000
	// liqPrice = 60000*(1+1/10-0.005) = 65700
	// surplus = (65700-66000)*qty*(-1) = 300*qty
	qty := d("0.01")
	surplus := CalcLiquidationSurplus(d("60000"), d("65700"), qty, -1, 10)
	expected := d("300").Mul(qty) // 3.0
	if !surplus.Equal(expected) {
		t.Errorf("expected surplus=%s, got %s", expected, surplus)
	}
}

func TestCalcLiquidationSurplus_Deficit(t *testing.T) {
	// If liqPrice is worse than bankruptcy (market gapped)
	// Long: bankrupt=54000, liqPrice=53000 (market gapped down)
	// surplus = (53000-54000)*0.01*1 = -10 (deficit)
	qty := d("0.01")
	surplus := CalcLiquidationSurplus(d("60000"), d("53000"), qty, 1, 10)
	if !surplus.IsNegative() {
		t.Errorf("expected negative surplus (deficit), got %s", surplus)
	}
}

func TestProcessLiquidation_Surplus(t *testing.T) {
	f := NewFund(d("10000"))
	result := f.ProcessLiquidation(d("60000"), d("54300"), d("0.01"), 1, 10)

	if result.Surplus.IsNegative() {
		t.Error("expected positive surplus")
	}
	if result.NeedADL {
		t.Error("should not need ADL")
	}
	if f.GetBalance().LessThanOrEqual(d("10000")) {
		t.Error("fund balance should have increased")
	}
}

func TestProcessLiquidation_DeficitCoveredByFund(t *testing.T) {
	f := NewFund(d("10000"))
	result := f.ProcessLiquidation(d("60000"), d("53000"), d("0.01"), 1, 10)

	if result.Surplus.IsPositive() {
		t.Error("expected negative surplus (deficit)")
	}
	if result.NeedADL {
		t.Error("fund should cover fully")
	}
	if result.FundCovered.IsZero() {
		t.Error("fund should have covered something")
	}
}

func TestProcessLiquidation_FundDepletedNeedsADL(t *testing.T) {
	f := NewFund(d("1")) // very small fund
	result := f.ProcessLiquidation(d("60000"), d("50000"), d("1"), 1, 10)

	// Huge deficit: (50000-54000)*1*1 = -4000
	if !result.NeedADL {
		t.Error("should need ADL when fund depleted")
	}
	if result.ADLDeficit.IsZero() {
		t.Error("ADL deficit should be non-zero")
	}
}
