package tutorial

import (
	"fmt"
	"learn_future/internal/types"
)

// TriggerContext holds info for dynamic tutorial content.
type TriggerContext struct {
	LiqPrice string
	Margin   string
	Pnl      string
	Fee      string
	Funding  string
	NetPnl   string
	Price    string
	Profit   string
}

// ShouldShow checks if a tutorial should be shown for this topic.
// completedTopics is the set of topic IDs already completed by the user.
func ShouldShow(topicID string, completedTopics map[string]bool) *types.TutorialCard {
	card, ok := Topics[topicID]
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
	card := *Topics[TopicPostLiquidation] // copy
	card.Content = fmt.Sprintf(
		"你的仓位在 %s 被强制平仓，损失全部保证金 %s USDT。"+
			"建议：1) 降低杠杆倍数 2) 始终设置止损 3) 不要把所有资金投入单笔交易。",
		ctx.LiqPrice, ctx.Margin,
	)
	return &card
}

// ForForceTakeProfit returns a customized tutorial card for force TP.
func ForForceTakeProfit(ctx TriggerContext) *types.TutorialCard {
	card := *Topics[TopicForceTpADL] // copy
	card.Content = fmt.Sprintf(
		"你的仓位在价格 %s 时被强制止盈，获利 %s USDT。"+
			"在实盘中，做市商/流动性提供者(LP)会设定最大敞口上限，"+
			"当某方向持仓盈利过大导致LP风险过高时，会触发自动减仓(ADL)机制。",
		ctx.Price, ctx.Profit,
	)
	return &card
}

// ForRealizedPnl returns a customized tutorial card for realized PnL.
func ForRealizedPnl(ctx TriggerContext) *types.TutorialCard {
	card := *Topics[TopicRealizedPnl] // copy
	card.Example = fmt.Sprintf(
		"本次盈亏=%sU，手续费=%sU，资金费用=%sU，净盈亏=%sU",
		ctx.Pnl, ctx.Fee, ctx.Funding, ctx.NetPnl,
	)
	return &card
}
