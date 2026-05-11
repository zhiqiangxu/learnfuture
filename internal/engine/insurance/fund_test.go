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

