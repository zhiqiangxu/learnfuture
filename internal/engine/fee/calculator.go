package fee

import (
	"github.com/shopspring/decimal"
)

// Maker/Taker Fee System
//
// Real exchange fee model:
// - Taker: market orders that "take" liquidity from the orderbook → higher fee
// - Maker: limit orders that "make" liquidity, sitting in the orderbook → lower fee (or rebate)
//
// VIP tiers: higher volume → lower fees
// Some exchanges give maker rebates (negative fees = you earn money for providing liquidity)

type Tier struct {
	Name      string
	Volume30d decimal.Decimal // 30-day volume threshold (USDT)
	MakerRate decimal.Decimal
	TakerRate decimal.Decimal
}

var DefaultTiers = []Tier{
	{Name: "普通用户", Volume30d: decimal.Zero, MakerRate: d("0.0002"), TakerRate: d("0.0004")},
	{Name: "VIP1", Volume30d: d("100000"), MakerRate: d("0.00018"), TakerRate: d("0.00035")},
	{Name: "VIP2", Volume30d: d("500000"), MakerRate: d("0.00015"), TakerRate: d("0.0003")},
	{Name: "VIP3", Volume30d: d("2000000"), MakerRate: d("0.0001"), TakerRate: d("0.00025")},
	{Name: "VIP4", Volume30d: d("10000000"), MakerRate: d("0.00005"), TakerRate: d("0.0002")},
	{Name: "做市商", Volume30d: d("50000000"), MakerRate: d("-0.00005"), TakerRate: d("0.00015")}, // maker rebate!
}

type Calculator struct {
	tiers []Tier
}

func NewCalculator(tiers []Tier) *Calculator {
	if len(tiers) == 0 {
		tiers = DefaultTiers
	}
	return &Calculator{tiers: tiers}
}

// GetTier returns the fee tier for a given 30-day volume.
func (c *Calculator) GetTier(volume30d decimal.Decimal) *Tier {
	var matched *Tier
	for i := range c.tiers {
		if volume30d.GreaterThanOrEqual(c.tiers[i].Volume30d) {
			matched = &c.tiers[i]
		}
	}
	if matched == nil {
		matched = &c.tiers[0]
	}
	return matched
}

// CalcFee calculates the fee for an order.
// isMaker: true for limit orders that rest in the book, false for market orders.
func (c *Calculator) CalcFee(positionValue decimal.Decimal, volume30d decimal.Decimal, isMaker bool) (fee decimal.Decimal, rate decimal.Decimal) {
	tier := c.GetTier(volume30d)
	if isMaker {
		rate = tier.MakerRate
	} else {
		rate = tier.TakerRate
	}
	fee = positionValue.Mul(rate)
	return fee, rate
}

// CalcSimpleFee calculates fee using a fixed rate (for the simulation default).
func CalcSimpleFee(positionValue, feeRate decimal.Decimal) decimal.Decimal {
	return positionValue.Mul(feeRate)
}

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}
