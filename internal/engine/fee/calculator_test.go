package fee

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dd(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestGetTier_Default(t *testing.T) {
	c := NewCalculator(nil)
	tier := c.GetTier(dd("0"))
	if tier.Name != "普通用户" {
		t.Errorf("expected 普通用户, got %s", tier.Name)
	}
}

func TestGetTier_VIP1(t *testing.T) {
	c := NewCalculator(nil)
	tier := c.GetTier(dd("100000"))
	if tier.Name != "VIP1" {
		t.Errorf("expected VIP1, got %s", tier.Name)
	}
}

func TestGetTier_VIP3(t *testing.T) {
	c := NewCalculator(nil)
	tier := c.GetTier(dd("5000000"))
	if tier.Name != "VIP3" {
		t.Errorf("expected VIP3, got %s", tier.Name)
	}
}

func TestGetTier_MarketMaker(t *testing.T) {
	c := NewCalculator(nil)
	tier := c.GetTier(dd("50000000"))
	if tier.Name != "做市商" {
		t.Errorf("expected 做市商, got %s", tier.Name)
	}
	// Maker rate should be negative (rebate)
	if !tier.MakerRate.IsNegative() {
		t.Error("做市商 maker rate should be negative (rebate)")
	}
}

func TestCalcFee_Taker(t *testing.T) {
	c := NewCalculator(nil)
	// Default taker: 0.04%
	fee, rate := c.CalcFee(dd("10000"), dd("0"), false)
	expectedFee := dd("4") // 10000 * 0.0004
	expectedRate := dd("0.0004")

	if !fee.Equal(expectedFee) {
		t.Errorf("expected fee=%s, got %s", expectedFee, fee)
	}
	if !rate.Equal(expectedRate) {
		t.Errorf("expected rate=%s, got %s", expectedRate, rate)
	}
}

func TestCalcFee_Maker(t *testing.T) {
	c := NewCalculator(nil)
	// Default maker: 0.02%
	fee, rate := c.CalcFee(dd("10000"), dd("0"), true)
	expectedFee := dd("2") // 10000 * 0.0002
	expectedRate := dd("0.0002")

	if !fee.Equal(expectedFee) {
		t.Errorf("expected fee=%s, got %s", expectedFee, fee)
	}
	if !rate.Equal(expectedRate) {
		t.Errorf("expected rate=%s, got %s", expectedRate, rate)
	}
}

func TestCalcFee_MakerRebate(t *testing.T) {
	c := NewCalculator(nil)
	// Market maker: -0.005% maker rate
	fee, _ := c.CalcFee(dd("10000"), dd("50000000"), true)
	if !fee.IsNegative() {
		t.Errorf("expected negative fee (rebate), got %s", fee)
	}
}

func TestCalcFee_VIPDiscount(t *testing.T) {
	c := NewCalculator(nil)
	defaultFee, _ := c.CalcFee(dd("10000"), dd("0"), false)
	vipFee, _ := c.CalcFee(dd("10000"), dd("2000000"), false) // VIP3

	if !vipFee.LessThan(defaultFee) {
		t.Errorf("VIP fee %s should be less than default %s", vipFee, defaultFee)
	}
}

func TestCalcSimpleFee(t *testing.T) {
	fee := CalcSimpleFee(dd("10000"), dd("0.0004"))
	if !fee.Equal(dd("4")) {
		t.Errorf("expected 4, got %s", fee)
	}
}
