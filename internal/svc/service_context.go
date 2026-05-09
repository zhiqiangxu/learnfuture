package svc

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/config"
	"learn_future/internal/engine/clearing"
	"learn_future/internal/engine/fee"
	"learn_future/internal/engine/funding"
	"learn_future/internal/engine/marketmaker"
	"learn_future/internal/engine/orderbook"
	"learn_future/internal/engine/insurance"
	"learn_future/internal/engine/markprice"
	"learn_future/internal/engine/pricefeed"
	"learn_future/internal/engine/trading"
	"learn_future/internal/model"
	"learn_future/internal/ws"
)

type ServiceContext struct {
	Config config.Config
	DB     *sql.DB

	// Models
	UserModel        *model.UserModel
	AccountModel     *model.AccountModel
	PositionModel    *model.PositionModel
	OrderModel       *model.OrderModel
	TradeModel       *model.TradeModel
	FundingModel     *model.FundingModel
	KlineModel       *model.KlineModel
	TutorialModel    *model.TutorialModel
	LeaderboardModel *model.LeaderboardModel

	// Caches
	PriceCache    *cache.PriceCache
	PositionCache *cache.PositionCache
	OrderCache    *cache.OrderCache

	// Engines
	TradingEngine    *trading.Engine
	TradeMonitor     *trading.Monitor
	Settler          *clearing.Settler
	PriceFeed        *pricefeed.Manager
	KlineAggregator  *pricefeed.KlineAggregator
	FundingScheduler *funding.Scheduler
	MarkPriceEngine  *markprice.Engine
	InsuranceFund    *insurance.Fund
	FeeCalculator    *fee.Calculator
	OrderBook        *orderbook.Book
	MarketMakerBot   *marketmaker.Bot

	// WebSocket
	Hub *ws.Hub
}

func NewServiceContext(c config.Config) *ServiceContext {
	// Connect to PostgreSQL
	db, err := sql.Open("postgres", c.Postgres.DataSource)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	db.SetMaxOpenConns(c.Postgres.MaxOpenConns)
	db.SetMaxIdleConns(c.Postgres.MaxIdleConns)

	// Initialize models
	userModel := model.NewUserModel(db)
	accountModel := model.NewAccountModel(db)
	positionModel := model.NewPositionModel(db)
	orderModel := model.NewOrderModel(db)
	tradeModel := model.NewTradeModel(db)
	fundingModel := model.NewFundingModel(db)
	klineModel := model.NewKlineModel(db)
	tutorialModel := model.NewTutorialModel(db)
	leaderboardModel := model.NewLeaderboardModel(db)

	// Initialize caches
	priceCache := cache.NewPriceCache()
	positionCache := cache.NewPositionCache()
	orderCache := cache.NewOrderCache()

	// WebSocket hub
	hub := ws.NewHub()
	go hub.Run()

	// Parse trading config — fail fast on invalid values
	feeRate, err := decimal.NewFromString(c.Trading.FeeRate)
	if err != nil {
		log.Fatalf("invalid FeeRate %q: %v", c.Trading.FeeRate, err)
	}
	maintRate, err := decimal.NewFromString(c.Trading.MaintenanceMarginRate)
	if err != nil {
		log.Fatalf("invalid MaintenanceMarginRate %q: %v", c.Trading.MaintenanceMarginRate, err)
	}
	forceTpROI, err := decimal.NewFromString(c.Trading.ForceTpROI)
	if err != nil {
		log.Fatalf("invalid ForceTpROI %q: %v", c.Trading.ForceTpROI, err)
	}
	minMargin, err := decimal.NewFromString(c.Trading.MinMargin)
	if err != nil {
		log.Fatalf("invalid MinMargin %q: %v", c.Trading.MinMargin, err)
	}

	// Fee calculator with Maker/Taker tiers
	feeCalculator := fee.NewCalculator(nil)

	// Order book (real limit order book with price-time priority)
	ob := orderbook.NewBook()

	// Clearance (pure calculation layer: margin/fee/PnL/liq price)
	clearance := clearing.NewClearance(feeCalculator, feeRate, maintRate, forceTpROI)

	// Settlement (DB persistence layer)
	settler := clearing.NewSettler(db, orderModel, positionModel, tradeModel, accountModel, fundingModel, 10000)

	// Matching engine (orderbook matching + memory state)
	tradingEngine := trading.NewEngine(
		db, priceCache, positionCache, orderCache, ob,
		accountModel, orderModel, positionModel, tradeModel,
		clearance, settler,
		trading.EngineConfig{
			MaxLeverage: c.Trading.MaxLeverage,
			MinMargin:   minMargin,
		},
	)

	// Market maker bot — onTrades callback processes limit orders matched by MM quotes
	mmBot := marketmaker.NewBot(ob, priceCache, marketmaker.DefaultConfig,
		func(trades []*orderbook.Trade, makerUserID int64, makerSide int) {
			tradingEngine.ProcessTrades(trades, makerUserID, makerSide, 1)
		},
	)

	// Mark price engine: spot (REST) as index, futures (WS) as last
	markPriceEngine := markprice.NewEngine(markprice.EngineConfig{
		BasisAlpha:   0.1,
		SpotFetchURL: c.PriceFeed.SpotURL,
	})

	// Insurance fund (start with 1M virtual USDT)
	insuranceFund := insurance.NewFund(decimal.NewFromInt(1000000))

	// Trade monitor (with mark price + insurance fund + ADL)
	tradeMonitor := trading.NewMonitor(
		tradingEngine, positionCache, orderCache, hub,
		markPriceEngine, insuranceFund,
	)

	// K-line aggregator
	klineAggregator := pricefeed.NewKlineAggregator(func(bar *pricefeed.KlineBar) {
		if err := klineModel.Upsert(bar.ToModelKline()); err != nil {
			log.Printf("[Kline] failed to persist kline: %v", err)
		}
	})

	// Price feed
	priceFeed := pricefeed.NewManager(
		c.PriceFeed.WsURL,
		priceCache,
		c.PriceFeed.ReconnectBaseDelay,
		c.PriceFeed.ReconnectMaxDelay,
		c.PriceFeed.PingInterval,
		c.PriceFeed.PongTimeout,
	)

	// Register price callbacks
	publishIntervalMs := c.PriceFeed.PublishInterval
	if publishIntervalMs <= 0 {
		publishIntervalMs = 200
	}

	// Batch price updates to WS clients
	var lastBroadcast time.Time
	priceFeed.OnPrice(func(price decimal.Decimal, ts time.Time) {
		// Update mark price: futures last price from WS
		// (spot/index price is fetched separately via REST every 5s)
		markPriceEngine.UpdateLastPrice(price)

		// Aggregate klines
		klineAggregator.OnTrade(price, ts)

		// Check orders/positions using mark price for liquidation
		tradeMonitor.OnPriceUpdate(price)

		// Throttle WS broadcast
		now := time.Now()
		if now.Sub(lastBroadcast) >= time.Duration(publishIntervalMs)*time.Millisecond {
			lastBroadcast = now
			ticker := priceCache.GetTicker()

			// Build kline snapshots for all intervals
			klineSnap := make(map[string]interface{})
			for _, cfg := range pricefeed.Intervals {
				bar := klineAggregator.GetCurrentBar(cfg.Name)
				if bar != nil {
					klineSnap[cfg.Name] = map[string]interface{}{
						"t": bar.OpenTime.UnixMilli(),
						"o": bar.Open.String(),
						"h": bar.High.String(),
						"l": bar.Low.String(),
						"c": bar.Close.String(),
					}
				}
			}

			msg, _ := ws.NewMessage("ticker", map[string]interface{}{
				"p": ticker.Price.String(),
				"c": ticker.Change.StringFixed(2),
				"h": ticker.High.String(),
				"l": ticker.Low.String(),
				"k": klineSnap,
			})
			hub.Broadcast(msg)
		}
	})

	// Funding scheduler
	fundingScheduler := funding.NewScheduler(
		c.Funding.SettleHours,
		c.Funding.FetchURL,
		priceCache,
		positionCache,
		tradingEngine.GetMemAccounts(),
		positionModel,
		accountModel,
		fundingModel,
		hub,
		maintRate,
	)

	return &ServiceContext{
		Config:           c,
		DB:               db,
		UserModel:        userModel,
		AccountModel:     accountModel,
		PositionModel:    positionModel,
		OrderModel:       orderModel,
		TradeModel:       tradeModel,
		FundingModel:     fundingModel,
		KlineModel:       klineModel,
		TutorialModel:    tutorialModel,
		LeaderboardModel: leaderboardModel,
		PriceCache:       priceCache,
		PositionCache:    positionCache,
		OrderCache:       orderCache,
		TradingEngine:    tradingEngine,
		TradeMonitor:     tradeMonitor,
		Settler:          settler,
		PriceFeed:        priceFeed,
		KlineAggregator:  klineAggregator,
		FundingScheduler: fundingScheduler,
		MarkPriceEngine:  markPriceEngine,
		InsuranceFund:    insuranceFund,
		FeeCalculator:    feeCalculator,
		OrderBook:        ob,
		MarketMakerBot:   mmBot,
		Hub:              hub,
	}
}

// LoadCachesFromDB loads active positions and pending orders from DB into caches.
func (svc *ServiceContext) LoadCachesFromDB() error {
	positions, err := svc.PositionModel.FindAllActive()
	if err != nil {
		return err
	}
	for _, p := range positions {
		var tp, sl string
		if p.TakeProfit != nil {
			tp = p.TakeProfit.String()
		}
		if p.StopLoss != nil {
			sl = p.StopLoss.String()
		}
		svc.PositionCache.Add(&cache.CachedPosition{
			ID:           p.ID,
			UserID:       p.UserID,
			Side:         p.Side,
			Leverage:     p.Leverage,
			EntryPrice:   p.EntryPrice.String(),
			Quantity:     p.Quantity.String(),
			Margin:       p.Margin.String(),
			LiqPrice:     p.LiqPrice.String(),
			ForceTpPrice: p.ForceTpPrice.String(),
			TakeProfit:   tp,
			StopLoss:     sl,
		})
	}
	log.Printf("[Cache] loaded %d active positions", len(positions))

	orders, err := svc.OrderModel.FindAllPending()
	if err != nil {
		return err
	}
	for _, o := range orders {
		if o.Price == nil {
			continue
		}
		var tp, sl string
		if o.TakeProfit != nil {
			tp = o.TakeProfit.String()
		}
		if o.StopLoss != nil {
			sl = o.StopLoss.String()
		}
		svc.OrderCache.Add(&cache.CachedOrder{
			ID:         o.ID,
			UserID:     o.UserID,
			Side:       o.Side,
			Price:      *o.Price,
			Quantity:   o.Quantity,
			MarginCost: o.MarginCost,
			Leverage:   o.Leverage,
			TakeProfit: tp,
			StopLoss:   sl,
		})
	}
	log.Printf("[Cache] loaded %d pending orders", len(orders))

	// Load all account balances into memory (source of truth for matching)
	accountCount, err := svc.loadAccountsToMemory()
	if err != nil {
		return err
	}
	log.Printf("[Cache] loaded %d account balances to memory", accountCount)

	return nil
}

func (svc *ServiceContext) loadAccountsToMemory() (int, error) {
	rows, err := svc.DB.Query(`SELECT user_id, balance, frozen FROM accounts`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	memAccounts := svc.TradingEngine.GetMemAccounts()
	count := 0
	for rows.Next() {
		var userID int64
		var balance, frozen decimal.Decimal
		if err := rows.Scan(&userID, &balance, &frozen); err != nil {
			return count, err
		}
		memAccounts.Load(userID, balance, frozen)
		count++
	}
	// Ensure market maker (SystemUserID=0) has a large account for zero-sum trading
	memAccounts.Load(0, decimal.NewFromInt(100_000_000), decimal.Zero)

	return count, rows.Err()
}

func (svc *ServiceContext) StartEngines() {
	svc.Settler.Start()          // async clearing & settlement worker
	svc.PriceFeed.Start()        // price feed WS connection
	svc.FundingScheduler.Start() // 8h funding rate settlement
	svc.MarkPriceEngine.Start()  // periodic spot price fetch
	svc.MarketMakerBot.Start()   // market maker bot (orderbook liquidity)
}

func (svc *ServiceContext) StopEngines() {
	svc.MarketMakerBot.Stop()
	svc.PriceFeed.Stop()
	svc.FundingScheduler.Stop()
	svc.MarkPriceEngine.Stop()
	svc.Settler.Stop() // drain remaining events before shutdown
}
