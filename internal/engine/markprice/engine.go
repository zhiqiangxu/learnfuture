package markprice

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Engine calculates the mark price used for liquidation calculations.
//
// Real exchange approach (even with a single source like Binance):
// - Index Price = SPOT price (现货价格), represents the "true" value
// - Last Price  = FUTURES price (合约最新成交价), includes market premium
// - These two naturally have a basis because futures carry a premium/discount
//
// Mark Price = Index Price × (1 + EMA(basis))
// Where basis = (futures_last - spot_index) / spot_index
//
// Why separate spot vs futures from the SAME source:
// - Spot = harder to manipulate (no leverage)
// - Futures = easier to manipulate (high leverage, can spike)
// - Using spot-based mark price protects users from futures manipulation
type Engine struct {
	mu sync.RWMutex

	lastPrice  decimal.Decimal // futures last trade price (from WS)
	indexPrice decimal.Decimal // spot price (fetched periodically via REST)
	markPrice  decimal.Decimal // calculated: indexPrice * (1 + EMA(basis))

	emaBasis   decimal.Decimal // EMA of basis
	basisAlpha decimal.Decimal // EMA smoothing factor

	spotFetchURL string         // REST endpoint for spot price
	client       *http.Client
	done         chan struct{}
	initialized  bool
}

type EngineConfig struct {
	BasisAlpha   float64 // EMA smoothing factor (default 0.1)
	SpotFetchURL string  // Spot price REST endpoint
	FetchInterval time.Duration // How often to fetch spot price (default 5s)
}

func NewEngine(cfg EngineConfig) *Engine {
	alpha := cfg.BasisAlpha
	if alpha == 0 {
		alpha = 0.1
	}
	return &Engine{
		basisAlpha:   decimal.NewFromFloat(alpha),
		spotFetchURL: cfg.SpotFetchURL,
		client:       &http.Client{Timeout: 5 * time.Second},
		done:         make(chan struct{}),
	}
}

// Start begins periodic spot price fetching.
func (e *Engine) Start() {
	if e.spotFetchURL == "" {
		return
	}
	go e.fetchLoop()
}

// Stop stops the spot price fetch loop.
func (e *Engine) Stop() {
	select {
	case <-e.done:
	default:
		close(e.done)
	}
}

func (e *Engine) fetchLoop() {
	// Initial fetch
	e.fetchSpotPrice()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.fetchSpotPrice()
		case <-e.done:
			return
		}
	}
}

func (e *Engine) fetchSpotPrice() {
	resp, err := e.client.Get(e.spotFetchURL)
	if err != nil {
		log.Printf("[MarkPrice] failed to fetch spot price: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	price := e.parseSpotPrice(body)
	if price.IsZero() {
		return
	}

	e.UpdateIndexPrice(price)
}

// parseSpotPrice tries multiple formats to extract spot price.
func (e *Engine) parseSpotPrice(body []byte) decimal.Decimal {
	// Try Binance format: {"price":"75000.00"}
	var binance struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &binance); err == nil && binance.Price != "" {
		if p, err := decimal.NewFromString(binance.Price); err == nil {
			return p
		}
	}

	// Try Gate.io spot format: [{"last":"75000.00",...}]
	var gateArr []struct {
		Last string `json:"last"`
	}
	if err := json.Unmarshal(body, &gateArr); err == nil && len(gateArr) > 0 {
		if p, err := decimal.NewFromString(gateArr[0].Last); err == nil {
			return p
		}
	}

	// Try Gate.io futures format: [{"index_price":"75000.00",...}]
	var gateFutures []struct {
		IndexPrice string `json:"index_price"`
	}
	if err := json.Unmarshal(body, &gateFutures); err == nil && len(gateFutures) > 0 {
		if p, err := decimal.NewFromString(gateFutures[0].IndexPrice); err == nil {
			return p
		}
	}

	return decimal.Zero
}

// UpdateLastPrice updates the futures last trade price (called on each WS trade).
func (e *Engine) UpdateLastPrice(price decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lastPrice = price

	if !e.initialized {
		// Before spot price is fetched, use futures price as fallback
		e.indexPrice = price
		e.markPrice = price
		e.initialized = true
		return
	}

	e.recalculate()
}

// UpdateIndexPrice updates the spot/index price (called periodically from REST).
func (e *Engine) UpdateIndexPrice(price decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.indexPrice = price

	if !e.initialized {
		e.lastPrice = price
		e.markPrice = price
		e.initialized = true
		return
	}

	e.recalculate()
}

func (e *Engine) recalculate() {
	if e.indexPrice.IsZero() {
		e.markPrice = e.lastPrice
		return
	}

	one := decimal.NewFromInt(1)

	// basis = (futures_last - spot_index) / spot_index
	basis := e.lastPrice.Sub(e.indexPrice).Div(e.indexPrice)

	// EMA of basis: emaBasis = alpha * basis + (1-alpha) * emaBasis
	e.emaBasis = e.basisAlpha.Mul(basis).Add(one.Sub(e.basisAlpha).Mul(e.emaBasis))

	// markPrice = indexPrice * (1 + emaBasis)
	e.markPrice = e.indexPrice.Mul(one.Add(e.emaBasis))
}

// GetMarkPrice returns the current mark price (used for liquidation).
func (e *Engine) GetMarkPrice() decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.markPrice
}

// GetLastPrice returns the futures last trade price.
func (e *Engine) GetLastPrice() decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastPrice
}

// GetIndexPrice returns the spot index price.
func (e *Engine) GetIndexPrice() decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.indexPrice
}

// GetPrices returns all three prices at once.
func (e *Engine) GetPrices() (last, mark, index decimal.Decimal) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastPrice, e.markPrice, e.indexPrice
}

// GetBasis returns the current EMA basis (for educational display).
func (e *Engine) GetBasis() decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.emaBasis
}
