package orderbook

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

// Order represents a single order in the order book.
type Order struct {
	ID        int64
	UserID    int64
	Side      int // 1=buy(bid), -1=sell(ask)
	Price     decimal.Decimal
	Quantity  decimal.Decimal // remaining unfilled quantity
	Timestamp time.Time
	IsMaker   bool // true for limit orders resting in the book
}

// Trade represents a matched trade between two orders.
type Trade struct {
	BuyOrderID  int64
	SellOrderID int64
	BuyUserID   int64
	SellUserID  int64
	Price       decimal.Decimal // execution price (maker's price)
	Quantity    decimal.Decimal
	Timestamp   time.Time
}

// PriceLevel represents all orders at a single price point.
type PriceLevel struct {
	Price  decimal.Decimal
	Orders []*Order
	Total  decimal.Decimal // total quantity at this level
}

// Book is a limit order book with price-time priority matching.
//
// Real exchange matching rules:
// 1. Price priority: better price first (highest bid, lowest ask)
// 2. Time priority: same price → earlier order first (FIFO)
// 3. Maker gets maker fee, taker gets taker fee
// 4. Trade executes at the maker's (resting) price
type Book struct {
	mu   sync.Mutex
	bids []*PriceLevel // sorted by price descending (best bid first)
	asks []*PriceLevel // sorted by price ascending (best ask first)

	orderIndex map[int64]*Order // orderID → order for fast lookup/cancel
	nextID     atomic.Int64
}

func NewBook() *Book {
	b := &Book{
		orderIndex: make(map[int64]*Order),
	}
	b.nextID.Store(1)
	return b
}

// NextOrderID returns a unique order ID.
func (b *Book) NextOrderID() int64 {
	return b.nextID.Add(1)
}

// PlaceLimit places a limit order. It first tries to match against the
// opposite side, then rests any remaining quantity in the book.
// Returns trades generated and the remaining order (nil if fully filled).
func (b *Book) PlaceLimit(order *Order) (trades []*Trade, remaining *Order) {
	b.mu.Lock()
	defer b.mu.Unlock()

	order.IsMaker = false // starts as taker, becomes maker if it rests
	order.Timestamp = time.Now()

	// Try to match against opposite side
	if order.Side == 1 { // buy order matches against asks
		trades = b.matchAgainst(&b.asks, order, true)
	} else { // sell order matches against bids
		trades = b.matchAgainst(&b.bids, order, false)
	}

	// If order still has remaining quantity, rest it in the book
	if order.Quantity.IsPositive() {
		order.IsMaker = true
		b.insertOrder(order)
		remaining = order
	}

	return trades, remaining
}

// PlaceMarket places a market order. It matches against the opposite side
// until filled or no more liquidity. Never rests in the book.
func (b *Book) PlaceMarket(order *Order) (trades []*Trade, err error) {
	if order.Price.IsPositive() {
		return nil, fmt.Errorf("PlaceMarket called with non-zero Price=%s, use PlaceLimit instead", order.Price)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	order.IsMaker = false
	order.Timestamp = time.Now()

	if order.Side == 1 {
		trades = b.matchAgainst(&b.asks, order, true)
	} else {
		trades = b.matchAgainst(&b.bids, order, false)
	}

	return trades, nil
}

// Cancel removes an order from the book. Returns the cancelled order or nil.
func (b *Book) Cancel(orderID int64) *Order {
	b.mu.Lock()
	defer b.mu.Unlock()

	order, ok := b.orderIndex[orderID]
	if !ok {
		return nil
	}

	b.removeOrder(order)
	return order
}

// matchAgainst tries to fill the incoming order against the opposite side.
// For a buy order matching asks: execute at ask price (ascending).
// For a sell order matching bids: execute at bid price (descending).
func (b *Book) matchAgainst(oppositeSide *[]*PriceLevel, taker *Order, isBuyer bool) []*Trade {
	var trades []*Trade

	for len(*oppositeSide) > 0 && taker.Quantity.IsPositive() {
		bestLevel := (*oppositeSide)[0]

		// Price check: buy order only matches asks <= buy price
		//              sell order only matches bids >= sell price
		if isBuyer && taker.Price.IsPositive() && bestLevel.Price.GreaterThan(taker.Price) {
			break // ask price too high for buyer's limit
		}
		if !isBuyer && taker.Price.IsPositive() && bestLevel.Price.LessThan(taker.Price) {
			break // bid price too low for seller's limit
		}

		for len(bestLevel.Orders) > 0 && taker.Quantity.IsPositive() {
			maker := bestLevel.Orders[0]

			// Determine fill quantity
			fillQty := decimal.Min(taker.Quantity, maker.Quantity)
			fillPrice := maker.Price // execute at maker's price

			// Create trade
			trade := &Trade{
				Price:    fillPrice,
				Quantity: fillQty,
				Timestamp: time.Now(),
			}
			if isBuyer {
				trade.BuyOrderID = taker.ID
				trade.BuyUserID = taker.UserID
				trade.SellOrderID = maker.ID
				trade.SellUserID = maker.UserID
			} else {
				trade.SellOrderID = taker.ID
				trade.SellUserID = taker.UserID
				trade.BuyOrderID = maker.ID
				trade.BuyUserID = maker.UserID
			}
			trades = append(trades, trade)

			// Update quantities
			taker.Quantity = taker.Quantity.Sub(fillQty)
			maker.Quantity = maker.Quantity.Sub(fillQty)
			bestLevel.Total = bestLevel.Total.Sub(fillQty)

			// Remove fully filled maker order
			if maker.Quantity.IsZero() {
				bestLevel.Orders = bestLevel.Orders[1:]
				delete(b.orderIndex, maker.ID)
			}
		}

		// Remove empty price level
		if len(bestLevel.Orders) == 0 {
			*oppositeSide = (*oppositeSide)[1:]
		}
	}

	return trades
}

// insertOrder adds an order to the correct price level.
func (b *Book) insertOrder(order *Order) {
	b.orderIndex[order.ID] = order

	if order.Side == 1 {
		b.insertIntoSide(&b.bids, order, true) // bids: descending
	} else {
		b.insertIntoSide(&b.asks, order, false) // asks: ascending
	}
}

func (b *Book) insertIntoSide(side *[]*PriceLevel, order *Order, descending bool) {
	// Find or create price level
	idx := sort.Search(len(*side), func(i int) bool {
		if descending {
			return (*side)[i].Price.LessThanOrEqual(order.Price)
		}
		return (*side)[i].Price.GreaterThanOrEqual(order.Price)
	})

	if idx < len(*side) && (*side)[idx].Price.Equal(order.Price) {
		// Append to existing level (time priority: FIFO)
		(*side)[idx].Orders = append((*side)[idx].Orders, order)
		(*side)[idx].Total = (*side)[idx].Total.Add(order.Quantity)
	} else {
		// Insert new level
		level := &PriceLevel{
			Price:  order.Price,
			Orders: []*Order{order},
			Total:  order.Quantity,
		}
		*side = append(*side, nil)
		copy((*side)[idx+1:], (*side)[idx:])
		(*side)[idx] = level
	}
}

// removeOrder removes an order from the book.
func (b *Book) removeOrder(order *Order) {
	delete(b.orderIndex, order.ID)

	var side *[]*PriceLevel
	if order.Side == 1 {
		side = &b.bids
	} else {
		side = &b.asks
	}

	for i, level := range *side {
		if level.Price.Equal(order.Price) {
			for j, o := range level.Orders {
				if o.ID == order.ID {
					level.Orders = append(level.Orders[:j], level.Orders[j+1:]...)
					level.Total = level.Total.Sub(order.Quantity)
					if len(level.Orders) == 0 {
						*side = append((*side)[:i], (*side)[i+1:]...)
					}
					return
				}
			}
		}
	}
}

// --- Query methods ---

// BestBid returns the highest bid price and total quantity, or zero if empty.
func (b *Book) BestBid() (decimal.Decimal, decimal.Decimal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bids) == 0 {
		return decimal.Zero, decimal.Zero
	}
	return b.bids[0].Price, b.bids[0].Total
}

// BestAsk returns the lowest ask price and total quantity, or zero if empty.
func (b *Book) BestAsk() (decimal.Decimal, decimal.Decimal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.asks) == 0 {
		return decimal.Zero, decimal.Zero
	}
	return b.asks[0].Price, b.asks[0].Total
}

// Spread returns the bid-ask spread.
func (b *Book) Spread() decimal.Decimal {
	bid, _ := b.BestBid()
	ask, _ := b.BestAsk()
	if bid.IsZero() || ask.IsZero() {
		return decimal.Zero
	}
	return ask.Sub(bid)
}

// Depth returns the top N price levels for each side.
type DepthLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

type DepthSnapshot struct {
	Bids []DepthLevel `json:"bids"`
	Asks []DepthLevel `json:"asks"`
}

func (b *Book) Depth(levels int) *DepthSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	snap := &DepthSnapshot{
		Bids: make([]DepthLevel, 0, levels),
		Asks: make([]DepthLevel, 0, levels),
	}

	for i := 0; i < levels && i < len(b.bids); i++ {
		snap.Bids = append(snap.Bids, DepthLevel{
			Price:    b.bids[i].Price.String(),
			Quantity: b.bids[i].Total.String(),
		})
	}
	for i := 0; i < levels && i < len(b.asks); i++ {
		snap.Asks = append(snap.Asks, DepthLevel{
			Price:    b.asks[i].Price.String(),
			Quantity: b.asks[i].Total.String(),
		})
	}

	return snap
}

// OrderCount returns the total number of orders in the book.
func (b *Book) OrderCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.orderIndex)
}
