package trading

import (
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/insurance"
	"learn_future/internal/engine/orderbook"
)

// LiquidationUserID is the system account that holds positions taken over from liquidated users.
// It is separate from the market maker (UserID=0) to keep concerns isolated.
const LiquidationUserID int64 = -1

// LiquidationEngine manages positions taken over from liquidated users.
// It periodically tries to close them on the orderbook via IOC limit orders.
//
// Flow:
//  1. Monitor detects distressed position → calls TakeOver()
//  2. TakeOver transfers position from user to LiquidationUserID at bankruptcy price
//  3. OnTick() periodically places IOC limit orders to close the network position
//  4. Surplus/deficit from closure goes to insurance fund
type LiquidationEngine struct {
	mu            sync.Mutex
	engine        *Engine
	positionCache *cache.PositionCache
	book          *orderbook.Book

	// Disposal strategy (inspired by Vega Protocol)
	disposalInterval time.Duration // how often to attempt disposal
	disposalFraction decimal.Decimal // fraction of position to close each tick (e.g., 0.5 = 50%)
	maxSlippage      decimal.Decimal // max price deviation from mid (e.g., 0.01 = 1%)
	insuranceFund    *insurance.Fund
	lastDisposal     time.Time
}

func NewLiquidationEngine(engine *Engine, positionCache *cache.PositionCache, book *orderbook.Book, fund *insurance.Fund) *LiquidationEngine {
	return &LiquidationEngine{
		engine:           engine,
		positionCache:    positionCache,
		book:             book,
		insuranceFund:    fund,
		disposalInterval: 1 * time.Second,
		disposalFraction: decimal.NewFromFloat(0.5),
		maxSlippage:      decimal.NewFromFloat(0.01),
	}
}

// TakeOver transfers a distressed user's position to the liquidation engine at bankruptcy price.
// Uses ProcessTrades with a synthetic trade so both sides are updated (∑longs ≡ ∑shorts maintained).
func (le *LiquidationEngine) TakeOver(pos *cache.CachedPosition, bankruptcyPrice decimal.Decimal) {
	quantity, _ := decimal.NewFromString(pos.Quantity)

	// Synthetic trade: user vs liquidation engine at bankruptcy price
	syntheticTrade := &orderbook.Trade{
		Price:    bankruptcyPrice,
		Quantity: quantity,
	}
	if pos.Side == 1 { // user is long → user sells, liq engine buys
		syntheticTrade.SellUserID = pos.UserID
		syntheticTrade.BuyUserID = LiquidationUserID
	} else { // user is short → user buys, liq engine sells
		syntheticTrade.BuyUserID = pos.UserID
		syntheticTrade.SellUserID = LiquidationUserID
	}

	// Atomically check position is still active (prevents concurrent close).
	// We don't change state here because ProcessTrades needs the position to be Active
	// for FindByUserSide to find it. ProcessTrades will remove the position via handleReduce.
	if pos.State.Load() != cache.PosStateActive {
		log.Printf("[LiquidationEngine] position %d state is %d (not active), skipping TakeOver", pos.ID, pos.State.Load())
		return
	}

	// ProcessTrades handles both sides:
	// - User: opposite trade closes their position (at bankruptcy price, margin = 0)
	// - LiquidationEngine: takes on the position
	le.engine.ProcessTrades(
		[]*orderbook.Trade{syntheticTrade},
		pos.UserID, -pos.Side, pos.Leverage,
	)

	log.Printf("[LiquidationEngine] took over position from user %d: side=%d qty=%s at bankruptcy price %s",
		pos.UserID, pos.Side, quantity, bankruptcyPrice.StringFixed(2))
}

// OnTick attempts to close some of the liquidation engine's positions on the orderbook.
// Called periodically (e.g., on each price update or on a timer).
func (le *LiquidationEngine) OnTick(midPrice decimal.Decimal) {
	le.mu.Lock()
	defer le.mu.Unlock()

	now := time.Now()
	if now.Before(le.lastDisposal.Add(le.disposalInterval)) {
		return
	}

	// Find all positions held by the liquidation engine
	positions := le.positionCache.GetByUser(LiquidationUserID)
	if len(positions) == 0 {
		return
	}

	le.lastDisposal = now

	for _, pos := range positions {
		quantity, _ := decimal.NewFromString(pos.Quantity)
		if quantity.IsZero() {
			continue
		}

		// Calculate disposal size
		disposeQty := quantity.Mul(le.disposalFraction)
		if disposeQty.LessThan(decimal.NewFromFloat(0.00001)) {
			disposeQty = quantity // close it all if tiny
		}

		// Calculate IOC limit price with slippage
		closeSide := -pos.Side
		var limitPrice decimal.Decimal
		if closeSide == -1 { // selling → price floor
			limitPrice = midPrice.Mul(decimal.NewFromInt(1).Sub(le.maxSlippage))
		} else { // buying → price ceiling
			limitPrice = midPrice.Mul(decimal.NewFromInt(1).Add(le.maxSlippage))
		}

		// Place IOC limit order: place limit, then cancel any remaining (simulates IOC)
		orderID := le.book.NextOrderID()
		trades, remaining := le.book.PlaceLimit(&orderbook.Order{
			ID:       orderID,
			UserID:   LiquidationUserID,
			Side:     closeSide,
			Price:    limitPrice.Round(2),
			Quantity: disposeQty,
		})

		// Cancel unfilled portion (IOC: don't leave resting orders)
		if remaining != nil {
			le.book.Cancel(orderID)
		}

		if len(trades) > 0 {
			le.engine.ProcessTrades(trades, LiquidationUserID, closeSide, 1)

			// Insurance fund: surplus/deficit = (disposal price - entry price) × qty × side
			// Entry price = bankruptcy price (the price at which the engine took over)
			entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
			for _, t := range trades {
				pnl := t.Price.Sub(entryPrice).Mul(t.Quantity).Mul(decimal.NewFromInt(int64(pos.Side)))
				if le.insuranceFund != nil {
					if pnl.IsPositive() {
						le.insuranceFund.Contribute(pnl) // surplus → fund grows
					} else {
						le.insuranceFund.Cover(pnl.Abs()) // deficit → fund pays
					}
				}
				log.Printf("[LiquidationEngine] disposed qty=%s at %s, entry=%s, pnl=%s",
					t.Quantity, t.Price.StringFixed(2), entryPrice.StringFixed(2), pnl.StringFixed(2))
			}
		}
	}
}
