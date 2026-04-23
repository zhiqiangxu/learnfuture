package types

// --- Auth ---

type WxLoginReq struct {
	Code string `json:"code"`
}

type WxLoginResp struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	UserID       int64  `json:"user_id"`
}

type RefreshTokenReq struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshTokenResp struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// --- Account ---

type AccountInfoResp struct {
	Balance   string          `json:"balance"`
	Frozen    string          `json:"frozen"`
	TotalPnl  string          `json:"total_pnl"`
	Available string          `json:"available"`
	Progress  *TutorialStatus `json:"progress,omitempty"`
}

type ResetAccountResp struct {
	Balance string `json:"balance"`
}

// --- Trade ---

type PlaceOrderReq struct {
	Side       int    `json:"side"`        // 1=多, -1=空
	OrderType  int    `json:"order_type"`  // 1=市价, 2=限价
	Leverage   int    `json:"leverage"`
	MarginMode int    `json:"margin_mode"` // 1=逐仓(默认), 2=全仓
	Margin     string `json:"margin"`      // 投入保证金 USDT
	Price      string `json:"price,omitempty"`
	TakeProfit string `json:"take_profit,omitempty"`
	StopLoss   string `json:"stop_loss,omitempty"`
}

type PlaceOrderResp struct {
	OrderID    int64        `json:"order_id"`
	PositionID int64        `json:"position_id,omitempty"`
	Status     string       `json:"status"` // "filled" or "pending"
	Detail     *OrderDetail `json:"detail,omitempty"` // 计算详情（教学用）
}

// OrderDetail shows the full calculation breakdown for educational purposes.
// Displayed in the trade panel's "计算详情" collapsible section.
type OrderDetail struct {
	// 输入
	Margin   string `json:"margin"`   // 投入保证金 (USDT)
	Leverage int    `json:"leverage"` // 杠杆倍数
	Side     string `json:"side"`     // "多/Long" or "空/Short"

	// 计算过程
	PositionValue string `json:"position_value"` // 仓位价值 = margin × leverage
	EntryPrice    string `json:"entry_price"`    // 开仓价格
	Quantity      string `json:"quantity"`       // 开仓数量 (BTC)
	FeeRate       string `json:"fee_rate"`       // 费率 (Maker/Taker)
	Fee           string `json:"fee"`            // 手续费 = positionValue × feeRate

	// 风控价格
	LiqPrice     string `json:"liq_price"`      // 强平价格
	ForceTpPrice string `json:"force_tp_price"`  // 强盈价格

	// 公式展示
	Formulas []string `json:"formulas"` // 计算公式步骤
}

type CancelOrderReq struct {
	OrderID int64 `json:"order_id"`
}

// --- Position ---

type ClosePositionReq struct {
	PositionID int64  `json:"position_id"`
	Quantity   string `json:"quantity,omitempty"` // 平仓数量 (BTC), 空=全部平仓, 支持部分平仓
}

type ClosePositionResp struct {
	RealizedPnl string `json:"realized_pnl"`
	Fee         string `json:"fee"`
	FundingPnl  string `json:"funding_pnl"`
	NetPnl      string `json:"net_pnl"`
}

type UpdateTPSLReq struct {
	PositionID int64  `json:"position_id"`
	TakeProfit string `json:"take_profit,omitempty"`
	StopLoss   string `json:"stop_loss,omitempty"`
}

type PositionInfo struct {
	ID            int64  `json:"id"`
	Side          int    `json:"side"`
	MarginMode    int    `json:"margin_mode"` // 1=逐仓, 2=全仓
	Leverage      int    `json:"leverage"`
	EntryPrice    string `json:"entry_price"`
	Quantity      string `json:"quantity"`
	Margin        string `json:"margin"`
	LiqPrice      string `json:"liq_price"`
	ForceTpPrice  string `json:"force_tp_price"`
	TakeProfit    string `json:"take_profit,omitempty"`
	StopLoss      string `json:"stop_loss,omitempty"`
	UnrealizedPnl string `json:"unrealized_pnl"`
	ROI           string `json:"roi"`
	MarginRatio   string `json:"margin_ratio"`
	FundingPnl    string `json:"funding_pnl"`
	ADLIndicator  int    `json:"adl_indicator"` // 1-5, higher = more likely to be ADL'd
	OpenedAt      int64  `json:"opened_at"`
}

type PositionListResp struct {
	Positions []PositionInfo `json:"positions"`
}

// --- Market ---

type PriceResp struct {
	Price  string `json:"price"`
	Change string `json:"change"` // 24h涨跌幅 %
	High   string `json:"high"`
	Low    string `json:"low"`
}

type KlineReq struct {
	Interval string `form:"interval"` // 1m,5m,15m,1h,4h,1d
	Limit    int    `form:"limit,optional,default=100"`
}

type Kline struct {
	OpenTime  int64  `json:"open_time"`
	Open      string `json:"open"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Close     string `json:"close"`
	Volume    string `json:"volume"`
	CloseTime int64  `json:"close_time"`
}

type KlineResp struct {
	Klines []Kline `json:"klines"`
}

type FundingRateResp struct {
	Rate       string `json:"rate"`
	NextSettle int64  `json:"next_settle"` // Unix timestamp
}

// --- History ---

type PaginationReq struct {
	Page     int `form:"page,optional,default=1"`
	PageSize int `form:"page_size,optional,default=20"`
}

type OrderHistory struct {
	ID          int64  `json:"id"`
	Side        int    `json:"side"`
	OrderType   int    `json:"order_type"`
	Leverage    int    `json:"leverage"`
	Price       string `json:"price"`
	FilledPrice string `json:"filled_price,omitempty"`
	Quantity    string `json:"quantity"`
	MarginCost  string `json:"margin_cost"`
	Status      int    `json:"status"`
	CreatedAt   int64  `json:"created_at"`
}

type TradeHistory struct {
	ID          int64  `json:"id"`
	Side        int    `json:"side"`
	Price       string `json:"price"`
	Quantity    string `json:"quantity"`
	Fee         string `json:"fee"`
	RealizedPnl string `json:"realized_pnl"`
	IsClose     bool   `json:"is_close"`
	CloseReason int    `json:"close_reason"`
	CreatedAt   int64  `json:"created_at"`
}

type FundingHistory struct {
	Rate          string `json:"rate"`
	PositionValue string `json:"position_value"`
	Payment       string `json:"payment"`
	SettleTime    int64  `json:"settle_time"`
}

type PnlSummary struct {
	TotalPnl      string `json:"total_pnl"`
	TotalFee      string `json:"total_fee"`
	TotalFunding  string `json:"total_funding"`
	WinCount      int    `json:"win_count"`
	LossCount     int    `json:"loss_count"`
	WinRate       string `json:"win_rate"`
	BestTrade     string `json:"best_trade"`
	WorstTrade    string `json:"worst_trade"`
	LiquidatedCnt int    `json:"liquidated_count"`
	ForceTpCnt    int    `json:"force_tp_count"`
}

// --- Leaderboard ---

type LeaderboardEntry struct {
	Rank     int    `json:"rank"`
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Value    string `json:"value"` // PnL or ROI
}

type LeaderboardResp struct {
	List []LeaderboardEntry `json:"list"`
	My   *LeaderboardEntry  `json:"my,omitempty"`
}

// --- Tutorial ---

type TutorialCard struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Formula  string `json:"formula,omitempty"`
	Example  string `json:"example,omitempty"`
	ShowOnce bool   `json:"show_once"`
}

type TutorialStatus struct {
	Completed int      `json:"completed"`
	Total     int      `json:"total"`
	Topics    []string `json:"topics"` // completed topic IDs
}

type CompleteTutorialReq struct {
	TopicID string `json:"topic_id"`
}

// --- WebSocket Messages ---

type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type WSTicker struct {
	Price  string `json:"p"`
	Change string `json:"c"`
	High   string `json:"h"`
	Low    string `json:"l"`
}

type WSPositionPnl struct {
	ID          int64  `json:"id"`
	Upnl        string `json:"upnl"`
	MarginRatio string `json:"margin_ratio"`
	ROI         string `json:"roi"`
}

type WSOrderFilled struct {
	OrderID int64  `json:"order_id"`
	Price   string `json:"price"`
	Qty     string `json:"qty"`
}

type WSLiquidated struct {
	PositionID int64  `json:"position_id"`
	LiqPrice   string `json:"liq_price"`
	Loss       string `json:"loss"`
}

type WSForceTP struct {
	PositionID int64  `json:"position_id"`
	Price      string `json:"price"`
	Profit     string `json:"profit"`
}

type WSFundingSettled struct {
	Rate    string `json:"rate"`
	Amount  string `json:"amount"`
}

// --- API Response Wrapper ---

type Response struct {
	Code     int          `json:"code"`
	Message  string       `json:"message"`
	Data     interface{}  `json:"data,omitempty"`
	Tutorial *TutorialCard `json:"tutorial,omitempty"`
}
