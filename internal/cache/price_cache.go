package cache

import (
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

type Ticker struct {
	Price  decimal.Decimal
	High   decimal.Decimal
	Low    decimal.Decimal
	Change decimal.Decimal // 24h change %
}

type PriceCache struct {
	price atomic.Value // stores decimal.Decimal

	mu     sync.RWMutex
	ticker Ticker
	open24 decimal.Decimal // 24h ago price for change calc
}

func NewPriceCache() *PriceCache {
	pc := &PriceCache{}
	pc.price.Store(decimal.Zero)
	return pc
}

func (c *PriceCache) SetPrice(price decimal.Decimal) {
	c.price.Store(price)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ticker.High.IsZero() || price.GreaterThan(c.ticker.High) {
		c.ticker.High = price
	}
	if c.ticker.Low.IsZero() || price.LessThan(c.ticker.Low) {
		c.ticker.Low = price
	}
	c.ticker.Price = price

	if !c.open24.IsZero() {
		c.ticker.Change = price.Sub(c.open24).Div(c.open24).Mul(decimal.NewFromInt(100))
	}
}

func (c *PriceCache) GetPrice() decimal.Decimal {
	v := c.price.Load()
	if v == nil {
		return decimal.Zero
	}
	return v.(decimal.Decimal)
}

func (c *PriceCache) GetTicker() Ticker {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ticker
}

func (c *PriceCache) SetOpen24(price decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open24 = price
}

func (c *PriceCache) Reset24H(openPrice decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open24 = openPrice
	c.ticker.High = c.ticker.Price
	c.ticker.Low = c.ticker.Price
}
