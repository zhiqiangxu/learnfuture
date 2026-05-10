package marketmaker

import (
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/orderbook"
)

// Bot is a simulated market maker that provides liquidity in the order book.
//
// Behavior mirrors real market makers:
// 1. Tracks external reference price (Gate.io)
// 2. Places symmetric bid/ask orders around the reference price
// 3. Maintains a configurable spread
// 4. Refreshes orders when price moves beyond threshold
// 5. Provides multiple depth levels (ladder quoting)
//
// The bot uses a virtual "system" user ID (0) with unlimited funds.
const SystemUserID = 0

type Config struct {
	SpreadBps    int     // half-spread in basis points (e.g., 5 = 0.05%)
	Levels       int     // number of price levels per side (e.g., 10)
	BaseQty      float64 // base quantity per level in BTC (e.g., 0.5)
	QtyMultiplier float64 // multiply qty for deeper levels (e.g., 1.5)
	RefreshMs    int     // refresh interval in milliseconds (e.g., 500)
	MoveThreshBps int    // re-quote when price moves this many bps (e.g., 3)
}

var DefaultConfig = Config{
	SpreadBps:     5,    // 0.05% half-spread (0.1% total)
	Levels:        10,   // 10 levels per side
	BaseQty:       0.5,  // 0.5 BTC per level
	QtyMultiplier: 1.3,  // deeper levels have more quantity
	RefreshMs:     500,  // refresh every 500ms
	MoveThreshBps: 3,    // re-quote on 0.03% move
}

// TradeCallback is called when market maker's PlaceLimit produces trades
// (i.e., a user's limit order was matched by the market maker's new quote).
type TradeCallback func(trades []*orderbook.Trade, makerUserID int64, makerSide int)

type Bot struct {
	mu         sync.Mutex
	book       *orderbook.Book
	priceCache *cache.PriceCache
	cfg        Config
	done       chan struct{}
	onTrades   TradeCallback

	lastQuotePrice decimal.Decimal
	activeOrders   []int64 // order IDs placed by bot
}

func NewBot(book *orderbook.Book, priceCache *cache.PriceCache, cfg Config, onTrades TradeCallback) *Bot {
	if cfg.SpreadBps == 0 {
		cfg = DefaultConfig
	}
	return &Bot{
		book:       book,
		priceCache: priceCache,
		cfg:        cfg,
		done:       make(chan struct{}),
		onTrades:   onTrades,
	}
}

// Start begins the market making loop.
func (b *Bot) Start() {
	go b.run()
}

// Stop stops the market maker.
func (b *Bot) Stop() {
	close(b.done)
}

func (b *Bot) run() {
	ticker := time.NewTicker(time.Duration(b.cfg.RefreshMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.tick()
		case <-b.done:
			return
		}
	}
}

func (b *Bot) tick() {
	refPrice := b.priceCache.GetPrice()
	if refPrice.IsZero() {
		return
	}

	// Collect trades under lock, call onTrades callback after releasing lock to avoid deadlock
	type tradeResult struct {
		trades []*orderbook.Trade
		side   int
	}
	var pendingTrades []tradeResult

	b.mu.Lock()
	// Note: mu.Unlock() is called explicitly before onTrades callbacks (not deferred)

	// Check if price moved enough to warrant re-quoting
	if !b.lastQuotePrice.IsZero() {
		moveRatio := refPrice.Sub(b.lastQuotePrice).Abs().Div(b.lastQuotePrice)
		threshold := decimal.NewFromFloat(float64(b.cfg.MoveThreshBps) / 10000)
		if moveRatio.LessThan(threshold) {
			b.mu.Unlock()
			return // price hasn't moved enough, keep current quotes
		}
	}

	// Cancel all existing bot orders
	for _, orderID := range b.activeOrders {
		b.book.Cancel(orderID)
	}
	b.activeOrders = b.activeOrders[:0]

	// Place new orders: ladder on both sides
	halfSpread := decimal.NewFromFloat(float64(b.cfg.SpreadBps) / 10000)
	levelStep := halfSpread // each additional level adds one more spread unit
	baseQty := decimal.NewFromFloat(b.cfg.BaseQty)
	qtyMult := decimal.NewFromFloat(b.cfg.QtyMultiplier)

	for i := 0; i < b.cfg.Levels; i++ {
		offset := halfSpread.Add(levelStep.Mul(decimal.NewFromInt(int64(i))))
		qty := baseQty.Mul(qtyMult.Pow(decimal.NewFromInt(int64(i))))

		// Bid (buy) side: refPrice * (1 - offset)
		bidPrice := refPrice.Mul(decimal.NewFromInt(1).Sub(offset)).Round(2)
		bidID := b.book.NextOrderID()
		bidTrades, _ := b.book.PlaceLimit(&orderbook.Order{
			ID:       bidID,
			UserID:   SystemUserID,
			Side:     1,
			Price:    bidPrice,
			Quantity: qty.Round(8),
		})
		if len(bidTrades) > 0 {
			pendingTrades = append(pendingTrades, tradeResult{bidTrades, 1})
		}
		b.activeOrders = append(b.activeOrders, bidID)

		// Ask (sell) side: refPrice * (1 + offset)
		askPrice := refPrice.Mul(decimal.NewFromInt(1).Add(offset)).Round(2)
		askID := b.book.NextOrderID()
		askTrades, _ := b.book.PlaceLimit(&orderbook.Order{
			ID:       askID,
			UserID:   SystemUserID,
			Side:     -1,
			Price:    askPrice,
			Quantity: qty.Round(8),
		})
		if len(askTrades) > 0 {
			pendingTrades = append(pendingTrades, tradeResult{askTrades, -1})
		}
		b.activeOrders = append(b.activeOrders, askID)
	}

	b.lastQuotePrice = refPrice
	log.Printf("[MarketMaker] quoted %d levels, ref=%s, spread=%.2fbps, orders=%d",
		b.cfg.Levels, refPrice.StringFixed(2),
		float64(b.cfg.SpreadBps)*2, len(b.activeOrders))
	b.mu.Unlock()

	// Process trades AFTER releasing bot.mu to avoid lock nesting (bot.mu → engine.mu)
	if b.onTrades != nil {
		for _, tr := range pendingTrades {
			b.onTrades(tr.trades, SystemUserID, tr.side)
		}
	}
}

// GetStats returns current market maker stats.
type Stats struct {
	RefPrice     string `json:"ref_price"`
	BestBid      string `json:"best_bid"`
	BestAsk      string `json:"best_ask"`
	Spread       string `json:"spread"`
	ActiveOrders int    `json:"active_orders"`
	TotalLevels  int    `json:"total_levels"`
}

func (b *Bot) GetStats() *Stats {
	b.mu.Lock()
	defer b.mu.Unlock()

	bid, _ := b.book.BestBid()
	ask, _ := b.book.BestAsk()
	spread := b.book.Spread()

	return &Stats{
		RefPrice:     b.lastQuotePrice.StringFixed(2),
		BestBid:      bid.StringFixed(2),
		BestAsk:      ask.StringFixed(2),
		Spread:       spread.StringFixed(2),
		ActiveOrders: len(b.activeOrders),
		TotalLevels:  b.cfg.Levels,
	}
}
