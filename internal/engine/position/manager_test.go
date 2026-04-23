package position

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

// --- CalcUnrealizedPnL ---

func TestCalcUnrealizedPnL_LongProfit(t *testing.T) {
	// (65000 - 60000) * 0.1 * 1 = 500
	result := CalcUnrealizedPnL(d("60000"), d("65000"), d("0.1"), 1)
	expected := d("500")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcUnrealizedPnL_LongLoss(t *testing.T) {
	// (55000 - 60000) * 0.1 * 1 = -500
	result := CalcUnrealizedPnL(d("60000"), d("55000"), d("0.1"), 1)
	expected := d("-500")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcUnrealizedPnL_ShortProfit(t *testing.T) {
	// (55000 - 60000) * 0.1 * -1 = 500
	result := CalcUnrealizedPnL(d("60000"), d("55000"), d("0.1"), -1)
	expected := d("500")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcUnrealizedPnL_ShortLoss(t *testing.T) {
	// (65000 - 60000) * 0.1 * -1 = -500
	result := CalcUnrealizedPnL(d("60000"), d("65000"), d("0.1"), -1)
	expected := d("-500")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcUnrealizedPnL_NoPriceChange(t *testing.T) {
	result := CalcUnrealizedPnL(d("60000"), d("60000"), d("0.1"), 1)
	if !result.IsZero() {
		t.Errorf("expected 0, got %s", result)
	}
}

// --- CalcRealizedPnL ---

func TestCalcRealizedPnL_LongTakeProfit(t *testing.T) {
	// (65000 - 60000) * 0.01 * 1 - 0.4 = 50 - 0.4 = 49.6
	result := CalcRealizedPnL(d("60000"), d("65000"), d("0.01"), 1, d("0.4"))
	expected := d("49.6")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcRealizedPnL_ShortTakeProfit(t *testing.T) {
	// (55000 - 60000) * 0.01 * -1 - 0.4 = 50 - 0.4 = 49.6
	result := CalcRealizedPnL(d("60000"), d("55000"), d("0.01"), -1, d("0.4"))
	expected := d("49.6")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// --- CalcROI ---

func TestCalcROI_10xLeverage10PercentUp(t *testing.T) {
	// margin=100, upnl=100 (10x, price up 10%) -> ROI = 100%
	result := CalcROI(d("100"), d("100"))
	expected := d("100")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcROI_ZeroMargin(t *testing.T) {
	result := CalcROI(d("100"), d("0"))
	if !result.IsZero() {
		t.Errorf("expected 0, got %s", result)
	}
}

// --- CalcMarginRatio ---

func TestCalcMarginRatio(t *testing.T) {
	// margin=100, upnl=-90, qty=0.01, price=60000
	// ratio = (100-90) / (0.01*60000) = 10/600 = 0.0167
	result := CalcMarginRatio(d("100"), d("-90"), d("0.01"), d("60000"))
	expected := d("10").Div(d("600"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// --- CalcLiquidationPrice ---

func TestCalcLiquidationPrice_Long10x(t *testing.T) {
	// Exact: 60000 * (1 - 1/10) / (1 - 0.005) = 60000 * 0.9 / 0.995 = 54271.356...
	result := CalcLiquidationPrice(d("60000"), 10, 1, d("0.005"))
	expected := d("60000").Mul(d("0.9")).Div(d("0.995"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcLiquidationPrice_Long20x(t *testing.T) {
	// 60000 * (1 - 1/20) / (1 - 0.005) = 60000 * 0.95 / 0.995
	result := CalcLiquidationPrice(d("60000"), 20, 1, d("0.005"))
	expected := d("60000").Mul(d("0.95")).Div(d("0.995"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcLiquidationPrice_Long100x(t *testing.T) {
	// 60000 * (1 - 1/100) / (1 - 0.005) = 60000 * 0.99 / 0.995
	result := CalcLiquidationPrice(d("60000"), 100, 1, d("0.005"))
	expected := d("60000").Mul(d("0.99")).Div(d("0.995"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcLiquidationPrice_Short10x(t *testing.T) {
	// 60000 * (1 + 1/10) / (1 + 0.005) = 60000 * 1.1 / 1.005
	result := CalcLiquidationPrice(d("60000"), 10, -1, d("0.005"))
	expected := d("60000").Mul(d("1.1")).Div(d("1.005"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcLiquidationPriceWithFunding_PaidMakesMoreDangerous(t *testing.T) {
	// Paid 10U funding → long liq price moves UP (closer to current price)
	noFunding := CalcLiquidationPrice(d("60000"), 10, 1, d("0.005"))
	paidFunding := CalcLiquidationPriceWithFunding(d("60000"), 10, 1, d("0.005"), d("-10"), d("0.01"))

	if paidFunding.LessThanOrEqual(noFunding) {
		t.Errorf("paid funding liq %s should be above no-funding liq %s", paidFunding, noFunding)
	}
}

func TestCalcLiquidationPriceWithFunding_ReceivedMakesSafer(t *testing.T) {
	// Received 10U funding → long liq price moves DOWN (safer)
	noFunding := CalcLiquidationPrice(d("60000"), 10, 1, d("0.005"))
	received := CalcLiquidationPriceWithFunding(d("60000"), 10, 1, d("0.005"), d("10"), d("0.01"))

	if received.GreaterThanOrEqual(noFunding) {
		t.Errorf("received funding liq %s should be below no-funding liq %s", received, noFunding)
	}
}

func TestCalcLiquidationPriceWithFunding_ZeroFundingEqualsOriginal(t *testing.T) {
	noFunding := CalcLiquidationPrice(d("60000"), 10, 1, d("0.005"))
	zeroFunding := CalcLiquidationPriceWithFunding(d("60000"), 10, 1, d("0.005"), d("0"), d("0.01"))

	if !noFunding.Equal(zeroFunding) {
		t.Errorf("zero funding %s should equal no-funding %s", zeroFunding, noFunding)
	}
}

func TestCalcLiquidationPrice_Symmetry(t *testing.T) {
	// Long liq price should be below entry, short liq price should be above
	entry := d("60000")
	longLiq := CalcLiquidationPrice(entry, 10, 1, d("0.005"))
	shortLiq := CalcLiquidationPrice(entry, 10, -1, d("0.005"))

	if longLiq.GreaterThanOrEqual(entry) {
		t.Errorf("long liq %s should be below entry %s", longLiq, entry)
	}
	if shortLiq.LessThanOrEqual(entry) {
		t.Errorf("short liq %s should be above entry %s", shortLiq, entry)
	}
}

// --- CalcForceTpPrice ---

func TestCalcForceTpPrice_Long10x(t *testing.T) {
	// 60000 * (1 + 5/10) = 60000 * 1.5 = 90000
	result := CalcForceTpPrice(d("60000"), 10, 1, d("5"))
	expected := d("90000")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcForceTpPrice_Short10x(t *testing.T) {
	// 60000 * (1 - 5/10) = 60000 * 0.5 = 30000
	result := CalcForceTpPrice(d("60000"), 10, -1, d("5"))
	expected := d("30000")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcForceTpPrice_HigherLeverageCloser(t *testing.T) {
	ftp10 := CalcForceTpPrice(d("60000"), 10, 1, d("5"))
	ftp20 := CalcForceTpPrice(d("60000"), 20, 1, d("5"))
	// Higher leverage -> force TP price closer to entry
	if ftp20.GreaterThanOrEqual(ftp10) {
		t.Errorf("20x force TP (%s) should be less than 10x (%s)", ftp20, ftp10)
	}
}

// --- CalcFee ---

func TestCalcFee(t *testing.T) {
	// margin=100, leverage=10, feeRate=0.0004 -> 100*10*0.0004 = 0.4
	result := CalcFee(d("100"), 10, d("0.0004"))
	expected := d("0.4")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// --- CalcQuantity ---

func TestCalcQuantity(t *testing.T) {
	// margin=100, leverage=10, price=67890 -> 1000/67890
	result := CalcQuantity(d("100"), 10, d("67890"))
	expected := d("1000").Div(d("67890"))
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcQuantity_ZeroPrice(t *testing.T) {
	result := CalcQuantity(d("100"), 10, d("0"))
	if !result.IsZero() {
		t.Errorf("expected 0, got %s", result)
	}
}

// --- CalcFundingPayment ---

func TestCalcFundingPayment_LongPays(t *testing.T) {
	// rate > 0, long pays: qty * price * rate * (-1)
	// 0.01 * 60000 * 0.0001 * (-1) = -0.06
	result := CalcFundingPayment(d("0.01"), d("60000"), d("0.0001"), 1)
	expected := d("-0.06")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcFundingPayment_ShortReceives(t *testing.T) {
	// rate > 0, short receives: qty * price * rate * (1)
	// 0.01 * 60000 * 0.0001 * 1 = 0.06
	result := CalcFundingPayment(d("0.01"), d("60000"), d("0.0001"), -1)
	expected := d("0.06")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestCalcFundingPayment_NegativeRate(t *testing.T) {
	// rate < 0, long receives: qty * price * (-0.0001) * (-1) = positive
	result := CalcFundingPayment(d("0.01"), d("60000"), d("-0.0001"), 1)
	expected := d("0.06")
	if !result.Equal(expected) {
		t.Errorf("expected %s, got %s", expected, result)
	}
}
