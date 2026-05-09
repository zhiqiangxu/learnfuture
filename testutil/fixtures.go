package testutil

import (
	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/config"
	"learn_future/internal/engine/clearing"
	"learn_future/internal/engine/fee"
	"learn_future/internal/engine/funding"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/engine/pricefeed"
	"learn_future/internal/engine/insurance"
	"learn_future/internal/engine/markprice"
	"learn_future/internal/engine/trading"
	"learn_future/internal/svc"
	"learn_future/internal/ws"
)

// NewTestServiceContext creates a ServiceContext with in-memory components only.
// No real DB, no real WS connections. Suitable for handler/logic unit tests.
func NewTestServiceContext() *svc.ServiceContext {
	priceCache := cache.NewPriceCache()
	positionCache := cache.NewPositionCache()
	orderCache := cache.NewOrderCache()
	hub := ws.NewHub()
	go hub.Run()

	feeCalc := fee.NewCalculator(nil)
	settler := clearing.NewSettler(nil, nil, nil, nil, nil, nil, 100)
	markPriceEngine := markprice.NewEngine(markprice.EngineConfig{BasisAlpha: 0.1})
	insuranceFund := insurance.NewFund(decimal.NewFromInt(1000000))

	ob := orderbook.NewBook()

	clearance := clearing.NewClearance(feeCalc,
		decimal.NewFromFloat(0.0004), decimal.NewFromFloat(0.005), decimal.NewFromInt(5))

	tradingEngine := trading.NewEngine(
		nil, priceCache, positionCache, orderCache, ob,
		nil, nil, nil, nil,
		clearance, settler,
		trading.EngineConfig{
			MaxLeverage: 125,
			MinMargin:   decimal.NewFromInt(1),
		},
	)

	tradeMonitor := trading.NewMonitor(
		tradingEngine, positionCache, orderCache, hub,
		markPriceEngine, insuranceFund,
	)

	cfg := config.Config{}
	cfg.Trading.FeeRate = "0.0004"
	cfg.Trading.MaintenanceMarginRate = "0.005"
	cfg.Trading.ForceTpROI = "5"
	cfg.Trading.MaxLeverage = 125
	cfg.Trading.MinMargin = "1"
	cfg.Trading.InitialBalance = "10000"
	cfg.JWT.Secret = "test-secret"
	cfg.JWT.Expire = 3600
	cfg.JWT.RefreshExpire = 86400

	klineAggregator := pricefeed.NewKlineAggregator(nil)

	fundingScheduler := funding.NewScheduler(
		[]int{0, 8, 16}, "", priceCache, positionCache,
		tradingEngine.GetMemAccounts(), nil, nil, nil, hub, decimal.NewFromFloat(0.005),
	)

	return &svc.ServiceContext{
		Config:          cfg,
		PriceCache:      priceCache,
		PositionCache:   positionCache,
		OrderCache:      orderCache,
		TradingEngine:   tradingEngine,
		TradeMonitor:    tradeMonitor,
		Settler:         settler,
		MarkPriceEngine: markPriceEngine,
		InsuranceFund:    insuranceFund,
		FeeCalculator:   feeCalc,
		KlineAggregator:  klineAggregator,
		FundingScheduler: fundingScheduler,
		Hub:             hub,
	}
}
