package tutorial

import (
	"fmt"
	"learn_future/internal/types"
)

// TriggerContext holds info for dynamic tutorial content.
type TriggerContext struct {
	LiqPrice string
	Margin   string
	RawPnl   string
	Pnl      string
	Fee      string
	Funding  string
	NetPnl   string
	Price    string
	Profit   string
	Lang     string // "en" or ""
}

// ShouldShow checks if a tutorial should be shown for this topic.
func ShouldShow(topicID string, completedTopics map[string]bool, lang string) *types.TutorialCard {
	topics := GetTopics(lang)
	card, ok := topics[topicID]
	if !ok {
		return nil
	}
	if card.ShowOnce && completedTopics[topicID] {
		return nil
	}
	return card
}

// ForPostLiquidation returns a customized tutorial card for post-liquidation.
func ForPostLiquidation(ctx TriggerContext) *types.TutorialCard {
	topics := GetTopics(ctx.Lang)
	card := *topics[TopicPostLiquidation]
	if ctx.Lang == "en" {
		card.Content = fmt.Sprintf(
			"Your position was liquidated at %s, losing all margin %s USDT. "+
				"Tips: 1) Lower leverage 2) Always set stop loss 3) Don't put all funds in one trade.",
			ctx.LiqPrice, ctx.Margin,
		)
	} else {
		card.Content = fmt.Sprintf(
			"你的仓位在 %s 被强制平仓，损失全部保证金 %s USDT。"+
				"建议：1) 降低杠杆倍数 2) 始终设置止损 3) 不要把所有资金投入单笔交易。",
			ctx.LiqPrice, ctx.Margin,
		)
	}
	return &card
}

// ForForceTakeProfit returns a customized tutorial card for force TP.
func ForForceTakeProfit(ctx TriggerContext) *types.TutorialCard {
	topics := GetTopics(ctx.Lang)
	card := *topics[TopicForceTpADL]
	if ctx.Lang == "en" {
		card.Content = fmt.Sprintf(
			"Your position was force-closed at price %s, profit %s USDT. "+
				"In live trading, market makers set max exposure limits. When profit gets too large, ADL is triggered.",
			ctx.Price, ctx.Profit,
		)
	} else {
		card.Content = fmt.Sprintf(
			"你的仓位在价格 %s 时被强制止盈，获利 %s USDT。"+
				"在实盘中，做市商/流动性提供者(LP)会设定最大敞口上限，"+
				"当某方向持仓盈利过大导致LP风险过高时，会触发自动减仓(ADL)机制。",
			ctx.Price, ctx.Profit,
		)
	}
	return &card
}

// ForRealizedPnl returns a customized tutorial card for realized PnL.
func ForRealizedPnl(ctx TriggerContext) *types.TutorialCard {
	topics := GetTopics(ctx.Lang)
	card := *topics[TopicRealizedPnl]
	if ctx.Lang == "en" {
		card.Example = fmt.Sprintf(
			"Price PnL (before fee)=%s USDT\n"+
				"Close fee=%s USDT\n"+
				"Realized PnL (after fee)=%s USDT\n"+
				"Cumulative funding=%s USDT\n"+
				"Net PnL=%s USDT",
			ctx.RawPnl, ctx.Fee, ctx.Pnl, ctx.Funding, ctx.NetPnl,
		)
	} else {
		card.Example = fmt.Sprintf(
			"价格变动盈亏（未扣费）=%sU\n"+
				"平仓手续费=%sU\n"+
				"已实现盈亏（扣费后）=%sU\n"+
				"累计资金费=%sU\n"+
				"净盈亏=%sU",
			ctx.RawPnl, ctx.Fee, ctx.Pnl, ctx.Funding, ctx.NetPnl,
		)
	}
	return &card
}
