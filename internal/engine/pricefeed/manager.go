package pricefeed

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
)

// PriceCallback is called on each new price.
type PriceCallback func(price decimal.Decimal, ts time.Time)

// Manager manages the external price feed WebSocket connection.
// Supports both Binance and Gate.io WebSocket formats.
type Manager struct {
	wsURL              string
	reconnectBaseDelay int
	reconnectMaxDelay  int
	pingInterval       int
	pongTimeout        int

	priceCache *cache.PriceCache
	callbacks  []PriceCallback

	mu   sync.Mutex
	conn *websocket.Conn
	done chan struct{}
}

func NewManager(wsURL string, priceCache *cache.PriceCache, reconnectBase, reconnectMax, pingInterval, pongTimeout int) *Manager {
	return &Manager{
		wsURL:              wsURL,
		reconnectBaseDelay: reconnectBase,
		reconnectMaxDelay:  reconnectMax,
		pingInterval:       pingInterval,
		pongTimeout:        pongTimeout,
		priceCache:         priceCache,
		done:               make(chan struct{}),
	}
}

func (m *Manager) OnPrice(cb PriceCallback) {
	m.callbacks = append(m.callbacks, cb)
}

func (m *Manager) Start() {
	go m.connectLoop()
}

func (m *Manager) Stop() {
	close(m.done)
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.mu.Unlock()
}

func (m *Manager) connectLoop() {
	attempt := 0
	for {
		select {
		case <-m.done:
			return
		default:
		}

		err := m.connectAndListen()
		if err != nil {
			attempt++
			delay := m.calcDelay(attempt)
			log.Printf("[PriceFeed] connection error: %v, reconnecting in %v", err, delay)
			select {
			case <-time.After(delay):
			case <-m.done:
				return
			}
		} else {
			attempt = 0
		}
	}
}

func (m *Manager) connectAndListen() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, _, err := dialer.Dial(m.wsURL, nil)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()

	log.Printf("[PriceFeed] connected to price source")

	// Subscribe to trades based on the WS URL format
	if err := m.subscribe(conn); err != nil {
		conn.Close()
		return err
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(m.pongTimeout) * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(time.Duration(m.pongTimeout) * time.Second))
		return nil
	})

	// Ping goroutine
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(m.pingInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				m.mu.Unlock()
				if err != nil {
					return
				}
			case <-pingDone:
				return
			case <-m.done:
				return
			}
		}
	}()

	defer func() {
		close(pingDone)
		conn.Close()
	}()

	for {
		select {
		case <-m.done:
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		m.handleMessage(msg)
	}
}

// subscribe sends the subscription message for the price source.
// Gate.io requires an explicit subscribe message after connecting.
// Binance subscribes via the URL path.
func (m *Manager) subscribe(conn *websocket.Conn) error {
	// Detect Gate.io by URL pattern
	if isGateIO(m.wsURL) {
		// Gate.io futures WebSocket subscribe format
		sub := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": "futures.trades",
			"event":   "subscribe",
			"payload": []string{"BTC_USDT"},
		}
		data, _ := json.Marshal(sub)
		log.Printf("[PriceFeed] subscribing to Gate.io futures trades")
		return conn.WriteMessage(websocket.TextMessage, data)
	}
	// Binance: subscription is via URL path, no message needed
	return nil
}

func isGateIO(url string) bool {
	return len(url) > 0 && (contains(url, "gate.io") || contains(url, "gateio"))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (m *Manager) handleMessage(msg []byte) {
	// Try Gate.io format first
	var gateMsg gateTradeMsg
	if err := json.Unmarshal(msg, &gateMsg); err == nil && gateMsg.Channel == "futures.trades" && gateMsg.Event == "update" {
		for _, trade := range gateMsg.Result {
			price, err := decimal.NewFromString(trade.Price)
			if err != nil {
				continue
			}
			ts := time.Unix(trade.CreateTime, 0)
			m.priceCache.SetPrice(price)
			for _, cb := range m.callbacks {
				cb(price, ts)
			}
		}
		return
	}

	// Try Binance format
	var binanceTrade binanceTradeMsg
	if err := json.Unmarshal(msg, &binanceTrade); err == nil && binanceTrade.Price != "" {
		price, err := decimal.NewFromString(binanceTrade.Price)
		if err != nil {
			return
		}
		ts := time.UnixMilli(binanceTrade.Time)
		m.priceCache.SetPrice(price)
		for _, cb := range m.callbacks {
			cb(price, ts)
		}
	}
}

func (m *Manager) calcDelay(attempt int) time.Duration {
	delay := float64(m.reconnectBaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(m.reconnectMaxDelay) {
		delay = float64(m.reconnectMaxDelay)
	}
	return time.Duration(delay) * time.Second
}

// --- Message formats ---

// Binance trade message
type binanceTradeMsg struct {
	Price string `json:"p"`
	Time  int64  `json:"T"`
}

// Gate.io futures trade message
type gateTradeMsg struct {
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Result  []gateTrade     `json:"result"`
}

type gateTrade struct {
	Price      string `json:"price"`
	Size       int64  `json:"size"`
	CreateTime int64  `json:"create_time"`
}
