package logic

import (
	"github.com/shopspring/decimal"

	"learn_future/internal/engine/trading"
	"learn_future/internal/model"
	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
)

type TradeLogic struct {
	svcCtx *svc.ServiceContext
}

func NewTradeLogic(svcCtx *svc.ServiceContext) *TradeLogic {
	return &TradeLogic{svcCtx: svcCtx}
}

func (l *TradeLogic) PlaceOrder(userID int64, req *types.PlaceOrderReq, lang string) (*types.PlaceOrderResp, *types.TutorialCard, error) {
	margin, err := decimal.NewFromString(req.Margin)
	if err != nil {
		return nil, nil, err
	}

	var tp, sl *decimal.Decimal
	if req.TakeProfit != "" {
		d, err := decimal.NewFromString(req.TakeProfit)
		if err != nil {
			return nil, nil, err
		}
		tp = &d
	}
	if req.StopLoss != "" {
		d, err := decimal.NewFromString(req.StopLoss)
		if err != nil {
			return nil, nil, err
		}
		sl = &d
	}

	var result *trading.PlaceOrderResult

	if req.OrderType == model.OrderTypeLimit {
		limitPrice, err := decimal.NewFromString(req.Price)
		if err != nil {
			return nil, nil, err
		}
		result, err = l.svcCtx.TradingEngine.PlaceLimitOrder(userID, req.Side, req.Leverage, margin, limitPrice, tp, sl)
		if err != nil {
			return nil, nil, err
		}
	} else {
		result, err = l.svcCtx.TradingEngine.PlaceMarketOrder(userID, req.Side, req.Leverage, req.MarginMode, margin, tp, sl)
		if err != nil {
			return nil, nil, err
		}
	}

	resp := &types.PlaceOrderResp{
		Status: result.Status,
	}
	if result.Order != nil {
		resp.OrderID = result.Order.ID
	}
	if result.Position != nil {
		resp.PositionID = result.Position.ID
		resp.Detail = l.buildOrderDetail(req, result)
	}

	card := l.getTutorialCard(userID, req, lang)
	return resp, card, nil
}

func (l *TradeLogic) CancelOrder(userID, orderID int64) error {
	return l.svcCtx.TradingEngine.CancelOrder(orderID, userID)
}

func (l *TradeLogic) buildOrderDetail(req *types.PlaceOrderReq, result *trading.PlaceOrderResult) *types.OrderDetail {
	if result.Position == nil {
		return nil
	}
	pos := result.Position
	posValue := pos.Margin.Mul(decimal.NewFromInt(int64(pos.Leverage)))

	sideStr := "多/Long"
	if pos.Side == -1 {
		sideStr = "空/Short"
	}

	feeRateStr := "0.04% (Taker)"
	if req.OrderType == model.OrderTypeLimit {
		feeRateStr = "0.02% (Maker)"
	}

	fee := decimal.Zero
	if result.Trade != nil {
		fee = result.Trade.Fee
	}

	liqFormula := "开仓价 × (1 - 1/杠杆 + 0.5%)"
	if pos.Side == -1 {
		liqFormula = "开仓价 × (1 + 1/杠杆 - 0.5%)"
	}
	ftpFormula := "开仓价 × (1 + 5/杠杆)"
	if pos.Side == -1 {
		ftpFormula = "开仓价 × (1 - 5/杠杆)"
	}

	return &types.OrderDetail{
		Margin:        pos.Margin.String(),
		Leverage:      pos.Leverage,
		Side:          sideStr,
		PositionValue: posValue.String(),
		EntryPrice:    pos.EntryPrice.String(),
		Quantity:      pos.Quantity.StringFixed(8),
		FeeRate:       feeRateStr,
		Fee:           fee.StringFixed(4),
		LiqPrice:      pos.LiqPrice.StringFixed(2),
		ForceTpPrice:  pos.ForceTpPrice.StringFixed(2),
		Formulas: []string{
			"仓位价值 = 保证金 × 杠杆 = " + pos.Margin.String() + " × " + decimal.NewFromInt(int64(pos.Leverage)).String() + " = " + posValue.String() + " USDT",
			"开仓数量 = 仓位价值 / 开仓价 = " + posValue.String() + " / " + pos.EntryPrice.String() + " = " + pos.Quantity.StringFixed(8) + " BTC",
			"手续费 = 仓位价值 × 费率 = " + posValue.String() + " × " + feeRateStr + " = " + fee.StringFixed(4) + " USDT",
			"强平价 = " + liqFormula + " = " + pos.LiqPrice.StringFixed(2),
			"强盈价 = " + ftpFormula + " = " + pos.ForceTpPrice.StringFixed(2),
		},
	}
}

func (l *TradeLogic) getTutorialCard(userID int64, req *types.PlaceOrderReq, lang string) *types.TutorialCard {
	if l.svcCtx.TutorialModel == nil {
		return nil
	}
	completed, _ := l.svcCtx.TutorialModel.GetCompleted(userID)

	var topicID string
	switch {
	case !completed[tutorial.TopicPerpetualIntro]:
		topicID = tutorial.TopicPerpetualIntro
	case !completed[tutorial.TopicLeverage] && req.Leverage > 1:
		topicID = tutorial.TopicLeverage
	case !completed[tutorial.TopicLongShort]:
		topicID = tutorial.TopicLongShort
	case !completed[tutorial.TopicMarketOrder] && req.OrderType == model.OrderTypeMarket:
		topicID = tutorial.TopicMarketOrder
	case !completed[tutorial.TopicLimitOrder] && req.OrderType == model.OrderTypeLimit:
		topicID = tutorial.TopicLimitOrder
	case !completed[tutorial.TopicTradingFee]:
		topicID = tutorial.TopicTradingFee
	}

	if topicID == "" {
		return nil
	}

	card := tutorial.ShouldShow(topicID, completed, lang)
	if card != nil {
		l.svcCtx.TutorialModel.MarkComplete(userID, topicID)
	}
	return card
}
