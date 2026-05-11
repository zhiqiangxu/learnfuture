package cache

import (
	"sync"

	"github.com/shopspring/decimal"
)

type CachedOrder struct {
	ID         int64
	UserID     int64
	Side       int // 1=long, -1=short
	Price      decimal.Decimal
	Quantity   decimal.Decimal
	MarginCost decimal.Decimal
	Leverage   int
	TakeProfit string
	StopLoss   string
}

type OrderCache struct {
	mu     sync.RWMutex
	orders map[int64]*CachedOrder // orderID -> order
}

func NewOrderCache() *OrderCache {
	return &OrderCache{
		orders: make(map[int64]*CachedOrder),
	}
}

func (c *OrderCache) Add(order *CachedOrder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orders[order.ID] = order
}

func (c *OrderCache) Remove(orderID int64) *CachedOrder {
	c.mu.Lock()
	defer c.mu.Unlock()
	order, ok := c.orders[orderID]
	if ok {
		delete(c.orders, orderID)
	}
	return order
}

func (c *OrderCache) Get(orderID int64) (*CachedOrder, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	o, ok := c.orders[orderID]
	return o, ok
}

func (c *OrderCache) GetByUser(userID int64) []*CachedOrder {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []*CachedOrder
	for _, o := range c.orders {
		if o.UserID == userID {
			result = append(result, o)
		}
	}
	return result
}

