package tutorial

import "learn_future/internal/types"

const TotalTopics = 14

// Topic IDs
const (
	TopicPerpetualIntro  = "perpetual_intro"
	TopicLeverage        = "leverage_explained"
	TopicLongShort       = "long_short"
	TopicUnrealizedPnl   = "unrealized_pnl"
	TopicTPSL            = "tp_sl_risk"
	TopicFundingRate     = "funding_rate"
	TopicLiquidation     = "liquidation"
	TopicPostLiquidation = "post_liquidation"
	TopicForceTpADL      = "force_tp_adl"
	TopicRealizedPnl     = "realized_pnl"
	TopicMarketOrder     = "market_order"
	TopicLimitOrder      = "limit_order"
	TopicTradingFee      = "trading_fee"
	TopicMarginMode      = "margin_mode"
)

// ParseLang extracts language from Accept-Language header. Returns "en" or "".
func ParseLang(acceptLang string) string {
	if len(acceptLang) >= 2 && (acceptLang[:2] == "en" || acceptLang[:2] == "EN") {
		return "en"
	}
	return ""
}

// GetTopics returns the topic map for the given language.
func GetTopics(lang string) map[string]*types.TutorialCard {
	if len(lang) >= 2 && (lang[:2] == "en" || lang[:2] == "EN") {
		return TopicsEN
	}
	return Topics
}

var AllTopicIDs = []string{
	TopicPerpetualIntro,
	TopicLeverage,
	TopicLongShort,
	TopicUnrealizedPnl,
	TopicTPSL,
	TopicFundingRate,
	TopicLiquidation,
	TopicPostLiquidation,
	TopicForceTpADL,
	TopicRealizedPnl,
	TopicMarketOrder,
	TopicLimitOrder,
	TopicTradingFee,
	TopicMarginMode,
}

var Topics = map[string]*types.TutorialCard{
	TopicPerpetualIntro: {
		ID:       TopicPerpetualIntro,
		Title:    "什么是永续合约？",
		Content:  "永续合约是一种没有到期日的衍生品合约，可以无限期持有。它通过「资金费率」机制使合约价格锚定现货价格。与交割合约不同，永续合约不需要交割，更适合灵活交易。\n\n简单说：你不需要真的买BTC，只需要猜BTC涨还是跌。猜对了赚差价，猜错了亏差价。",
		Formula:  "",
		Example:  "BTC现在60000U，你觉得会涨→开多合约→BTC涨到65000→你赚5000U的差价（不需要真的持有BTC）",
		ShowOnce: true,
	},
	TopicLeverage: {
		ID:       TopicLeverage,
		Title:    "什么是杠杆？",
		Content:  "杠杆允许你用较少的保证金控制更大价值的仓位。10x杠杆意味着用100U保证金可以控制1000U的仓位。杠杆放大收益的同时也放大亏损，杠杆越高，离强平越近。",
		Formula:  "保证金 = 仓位价值 / 杠杆倍数",
		Example:  "100 USDT × 10倍杠杆 = 控制1000 USDT仓位",
		ShowOnce: true,
	},
	TopicLongShort: {
		ID:       TopicLongShort,
		Title:    "多空机制",
		Content:  "开多（做多）= 看涨，价格上涨时盈利，下跌时亏损。开空（做空）= 看跌，价格下跌时盈利，上涨时亏损。永续合约的核心优势就是可以双向交易。",
		Formula:  "",
		Example:  "BTC当前60000U，你认为会涨→开多；你认为会跌→开空",
		ShowOnce: true,
	},
	TopicUnrealizedPnl: {
		ID:       TopicUnrealizedPnl,
		Title:    "未实现盈亏",
		Content:  "未实现盈亏是你持仓中的浮动盈亏，只有平仓后才会变成真实的盈亏（已实现盈亏）。它会随着价格波动实时变化。",
		Formula:  "未实现盈亏 = (当前价 - 开仓价) × 数量 × 方向",
		Example:  "开多 0.01BTC，开仓价60000，当前价65000 → 盈亏 = (65000-60000)×0.01×1 = +50U",
		ShowOnce: true,
	},
	TopicTPSL: {
		ID:       TopicTPSL,
		Title:    "止盈止损",
		Content:  "止盈（Take Profit）和止损（Stop Loss）是预设的自动平仓价格，是风险控制的核心工具。设置止盈锁定利润，设置止损限制亏损。建议每笔交易都设置止损。",
		Formula:  "",
		Example:  "开多仓60000，设止盈65000（赚5000就走），止损58000（最多亏2000）",
		ShowOnce: true,
	},
	TopicFundingRate: {
		ID:       TopicFundingRate,
		Title:    "资金费率",
		Content:  "资金费率每8小时结算一次（00:00/08:00/16:00 UTC），用于让合约价格锚定现货价格。费率为正时，多头向空头支付；费率为负时，空头向多头支付。",
		Formula:  "资金费 = 仓位价值 × 资金费率",
		Example:  "持仓价值10000U，费率0.01% → 资金费 = 10000 × 0.0001 = 1U",
		ShowOnce: true,
	},
	TopicLiquidation: {
		ID:       TopicLiquidation,
		Title:    "强制平仓（爆仓）",
		Content:  "当你的保证金率降到0.5%以下时，系统会强制平仓（俗称爆仓）。杠杆越高，强平价离开仓价越近。合理使用杠杆和设置止损是避免爆仓的关键。",
		Formula:  "强平价(多) = 开仓价 × (1 - 1/杠杆 + 0.5%)\n保证金率 = (保证金 + 未实现盈亏) / 仓位价值",
		Example:  "60000开多，10x杠杆 → 强平价≈54300，价格跌9.5%就爆仓",
		ShowOnce: true,
	},
	TopicPostLiquidation: {
		ID:       TopicPostLiquidation,
		Title:    "爆仓复盘",
		Content:  "你的仓位被强制平仓了，损失了全部保证金。这是交易中最痛苦的经历，但也是最好的教训。建议：1) 降低杠杆倍数 2) 始终设置止损 3) 不要把所有资金投入单笔交易。",
		Formula:  "",
		Example:  "",
		ShowOnce: false, // 每次爆仓都显示
	},
	TopicForceTpADL: {
		ID:       TopicForceTpADL,
		Title:    "强盈与ADL机制",
		Content:  "你的仓位收益触发了对手方LP的风控上限，被强制平仓获利了结。在实盘中，这对应「自动减仓」(ADL)机制。做市商/流动性提供者(LP)会设定最大敞口上限，当某方向持仓盈利过大导致LP风险过高时，会触发ADL。ADL优先减仓对手方中盈利最高的仓位。",
		Formula:  "强盈触发: 收益率 ≥ 500%（仓位价值达保证金6倍）",
		Example:  "10x杠杆开多，价格涨50%时收益率=500%，触发强盈",
		ShowOnce: false, // 每次强盈都显示
	},
	TopicRealizedPnl: {
		ID:       TopicRealizedPnl,
		Title:    "已实现盈亏",
		Content:  "平仓后的实际盈亏，需要扣除手续费和累计的资金费用。这才是你真正赚到或亏掉的钱。",
		Formula:  "价格盈亏 = (平仓价 - 开仓价) × 数量 × 方向\n已实现盈亏 = 价格盈亏 - 平仓手续费\n净盈亏 = 已实现盈亏 + 累计资金费",
		Example:  "价格盈亏+500U - 平仓手续费0.4U = 已实现盈亏499.6U + 资金费-1U = 净盈亏498.6U",
		ShowOnce: true,
	},
	TopicMarketOrder: {
		ID:       TopicMarketOrder,
		Title:    "市价单",
		Content:  "市价单以当前市场价格立即成交。优点是保证成交，缺点是在剧烈波动时可能出现滑点（实际成交价与预期有偏差）。适合需要快速进场的情况。",
		Formula:  "",
		Example:  "BTC当前60000，下市价多单 → 立即以60000（附近）成交",
		ShowOnce: true,
	},
	TopicLimitOrder: {
		ID:       TopicLimitOrder,
		Title:    "限价单",
		Content:  "限价单在价格达到指定价位时才触发成交。优点是可以获得更好的成交价格，缺点是可能不会成交。挂单期间保证金会被冻结。",
		Formula:  "",
		Example:  "BTC当前60000，你想在58000买入→下限价多单58000，价格跌到58000时自动成交",
		ShowOnce: true,
	},
	TopicTradingFee: {
		ID:       TopicTradingFee,
		Title:    "手续费",
		Content:  "每次开仓和平仓都会收取手续费，按仓位价值的百分比计算。频繁交易会导致手续费累积，侵蚀利润。",
		Formula:  "手续费 = 仓位价值 × 费率(0.04%)",
		Example:  "仓位价值1000U → 手续费 = 1000 × 0.04% = 0.4U（开仓+平仓共0.8U）",
		ShowOnce: true,
	},
	TopicMarginMode: {
		ID:       TopicMarginMode,
		Title:    "逐仓 vs 全仓",
		Content:  "逐仓模式(Isolated)：每个仓位独立锁定保证金，爆仓只亏该仓位的保证金，其他仓位不受影响。适合新手，风险可控。\n\n全仓模式(Cross)：所有仓位共享账户可用余额作为保证金。优点是不容易爆仓（余额自动补充保证金），缺点是一旦爆仓会失去全部余额。适合有经验的交易者。",
		Formula:  "逐仓爆仓条件: 仓位保证金率 ≤ 0.5%\n全仓爆仓条件: 账户净值 ≤ 所有仓位维持保证金之和",
		Example:  "账户1000U，开两个仓各用100U。逐仓模式爆一个最多亏100U；全仓模式爆了可能亏掉全部1000U",
		ShowOnce: true,
	},
}
