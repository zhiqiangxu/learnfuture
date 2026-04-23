package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	rest.RestConf

	Postgres struct {
		DataSource string
	}

	JWT struct {
		Secret        string
		Expire        int64
		RefreshExpire int64
	}

	WeChat struct {
		AppID     string
		AppSecret string
	}

	PriceFeed struct {
		WsURL              string // Futures WebSocket (合约成交流)
		SpotURL            string // Spot REST API (现货价格, 作为指数价格)
		ReconnectBaseDelay int
		ReconnectMaxDelay  int
		PingInterval       int
		PongTimeout        int
		PublishInterval    int
	}

	Trading struct {
		FeeRate               string
		MaintenanceMarginRate string
		MaxLeverage           int
		MinMargin             string
		InitialBalance        string
		ForceTpROI            string
		WALPath               string `json:",optional"`
	}

	Funding struct {
		SettleHours []int
		FetchURL    string
	}
}
