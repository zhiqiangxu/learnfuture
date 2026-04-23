package liquidation

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestCheckLiquidation_LongTriggered(t *testing.T) {
	// Long: entry=60000, qty=0.01, margin=60, current=54000 (below liq price)
	// upnl = (54000-60000)*0.01*1 = -60
	// marginRatio = (60-60)/(0.01*54000) = 0/540 = 0 <= 0.005
	result := CheckLiquidation(d("60000"), d("54000"), d("0.01"), d("60"), 1, d("0.005"))
	if !result {
		t.Error("expected liquidation triggered")
	}
}

func TestCheckLiquidation_LongNotTriggered(t *testing.T) {
	// upnl = (59000-60000)*0.01*1 = -10
	// marginRatio = (60-10)/(0.01*59000) = 50/590 = 0.0847 > 0.005
	result := CheckLiquidation(d("60000"), d("59000"), d("0.01"), d("60"), 1, d("0.005"))
	if result {
		t.Error("expected no liquidation")
	}
}

func TestCheckLiquidation_ShortTriggered(t *testing.T) {
	// Short: entry=60000, qty=0.01, margin=60, current=66000
	// upnl = (66000-60000)*0.01*(-1) = -60
	// marginRatio = (60-60)/(0.01*66000) = 0
	result := CheckLiquidation(d("60000"), d("66000"), d("0.01"), d("60"), -1, d("0.005"))
	if !result {
		t.Error("expected liquidation triggered for short")
	}
}

func TestCheckLiquidation_ShortNotTriggered(t *testing.T) {
	result := CheckLiquidation(d("60000"), d("61000"), d("0.01"), d("60"), -1, d("0.005"))
	if result {
		t.Error("expected no liquidation for short")
	}
}

func TestCheckForceTakeProfit_LongTriggered(t *testing.T) {
	// 10x leverage: margin=100, entry=60000, qty=100*10/60000=0.01667
	// Current=90000: upnl = (90000-60000)*0.01667 = 500.1
	// ROI = 500.1/100*100 = 500.1% >= 500%
	qty := d("100").Mul(d("10")).Div(d("60000"))
	result := CheckForceTakeProfit(d("60000"), d("90000"), d("100"), qty, 1, d("5"))
	if !result {
		t.Error("expected force TP triggered")
	}
}

func TestCheckForceTakeProfit_NotTriggered(t *testing.T) {
	qty := d("100").Mul(d("10")).Div(d("60000"))
	result := CheckForceTakeProfit(d("60000"), d("70000"), d("100"), qty, 1, d("5"))
	if result {
		t.Error("expected no force TP")
	}
}

func TestShouldLiquidateAtPrice_Long(t *testing.T) {
	if !ShouldLiquidateAtPrice(d("54000"), d("54300"), 1) {
		t.Error("price below liq price should trigger")
	}
	if ShouldLiquidateAtPrice(d("55000"), d("54300"), 1) {
		t.Error("price above liq price should not trigger")
	}
}

func TestShouldLiquidateAtPrice_Short(t *testing.T) {
	if !ShouldLiquidateAtPrice(d("66000"), d("65700"), -1) {
		t.Error("price above liq price should trigger for short")
	}
	if ShouldLiquidateAtPrice(d("65000"), d("65700"), -1) {
		t.Error("price below liq price should not trigger for short")
	}
}

func TestShouldForceTPAtPrice_Long(t *testing.T) {
	if !ShouldForceTPAtPrice(d("90000"), d("90000"), 1) {
		t.Error("price at force TP should trigger")
	}
	if ShouldForceTPAtPrice(d("89000"), d("90000"), 1) {
		t.Error("price below force TP should not trigger for long")
	}
}

func TestShouldForceTPAtPrice_Short(t *testing.T) {
	if !ShouldForceTPAtPrice(d("30000"), d("30000"), -1) {
		t.Error("price at force TP should trigger for short")
	}
	if ShouldForceTPAtPrice(d("31000"), d("30000"), -1) {
		t.Error("price above force TP should not trigger for short")
	}
}
