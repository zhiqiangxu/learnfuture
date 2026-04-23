package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"

	"learn_future/internal/config"
	"learn_future/internal/handler"
	"learn_future/internal/middleware"
	"learn_future/internal/svc"
)

var configFile = flag.String("f", "etc/config.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	svcCtx := svc.NewServiceContext(c)

	// Load caches from DB (recovery after restart)
	if err := svcCtx.LoadCachesFromDB(); err != nil {
		log.Fatalf("failed to load caches: %v", err)
	}

	// Start background engines
	svcCtx.StartEngines()
	defer svcCtx.StopEngines()

	// Register routes
	registerRoutes(server, svcCtx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}

func registerRoutes(server *rest.Server, svcCtx *svc.ServiceContext) {
	auth := middleware.AuthMiddleware(svcCtx.Config.JWT.Secret)

	// Public routes
	server.AddRoutes([]rest.Route{
		{Method: http.MethodPost, Path: "/api/v1/auth/wx-login", Handler: handler.WxLoginHandler(svcCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/refresh", Handler: handler.RefreshTokenHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/price", Handler: handler.PriceHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/klines", Handler: handler.KlinesHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/funding-rate", Handler: handler.FundingRateHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/mark-price", Handler: handler.MarkPriceHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/insurance-fund", Handler: handler.InsuranceFundHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/api/v1/market/depth", Handler: handler.DepthHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/ws", Handler: handler.WebSocketHandler(svcCtx)},
		{Method: http.MethodGet, Path: "/health", Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"status":"ok"}`))
		}},
	})

	// Authenticated routes
	server.AddRoutes([]rest.Route{
		// Account
		{Method: http.MethodGet, Path: "/api/v1/account/info", Handler: auth(handler.AccountInfoHandler(svcCtx))},
		{Method: http.MethodPost, Path: "/api/v1/account/reset", Handler: auth(handler.ResetAccountHandler(svcCtx))},

		// Trade
		{Method: http.MethodPost, Path: "/api/v1/order/place", Handler: auth(handler.PlaceOrderHandler(svcCtx))},
		{Method: http.MethodPost, Path: "/api/v1/order/cancel", Handler: auth(handler.CancelOrderHandler(svcCtx))},

		// Position
		{Method: http.MethodPost, Path: "/api/v1/position/close", Handler: auth(handler.ClosePositionHandler(svcCtx))},
		{Method: http.MethodPost, Path: "/api/v1/position/update-tpsl", Handler: auth(handler.UpdateTPSLHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/position/list", Handler: auth(handler.PositionListHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/order/pending", Handler: auth(handler.PendingOrdersHandler(svcCtx))},

		// History
		{Method: http.MethodGet, Path: "/api/v1/history/orders", Handler: auth(handler.HistoryOrdersHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/history/trades", Handler: auth(handler.HistoryTradesHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/history/funding", Handler: auth(handler.HistoryFundingHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/history/pnl", Handler: auth(handler.PnlSummaryHandler(svcCtx))},

		// Leaderboard
		{Method: http.MethodGet, Path: "/api/v1/leaderboard/pnl", Handler: auth(handler.LeaderboardPnlHandler(svcCtx))},
		{Method: http.MethodGet, Path: "/api/v1/leaderboard/roi", Handler: auth(handler.LeaderboardROIHandler(svcCtx))},

		// Tutorial
		{Method: http.MethodGet, Path: "/api/v1/tutorial/progress", Handler: auth(handler.TutorialProgressHandler(svcCtx))},
		{Method: http.MethodPost, Path: "/api/v1/tutorial/complete", Handler: auth(handler.CompleteTutorialHandler(svcCtx))},
	})
}
