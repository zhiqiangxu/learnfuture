package logic

import (
	"errors"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/adl"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
)

type PositionLogic struct {
	svcCtx *svc.ServiceContext
}

func NewPositionLogic(svcCtx *svc.ServiceContext) *PositionLogic {
	return &PositionLogic{svcCtx: svcCtx}
}

func (l *PositionLogic) Close(userID, positionID int64, qtyStr string, lang string) (*types.ClosePositionResp, *types.TutorialCard, error) {
	var closeQty decimal.Decimal
	if qtyStr != "" {
		var err error
		closeQty, err = decimal.NewFromString(qtyStr)
		if err != nil {
			return nil, nil, err
		}
	}
	result, err := l.svcCtx.TradingEngine.ClosePosition(positionID, userID, model.CloseReasonManual, closeQty)
	if err != nil {
		return nil, nil, err
	}

	resp := &types.ClosePositionResp{
		RealizedPnl: result.RealizedPnl.StringFixed(2),
		Fee:         result.Fee.StringFixed(2),
		FundingPnl:  result.FundingPnl.StringFixed(2),
		NetPnl:      result.NetPnl.StringFixed(2),
	}

	completed, _ := l.svcCtx.TutorialModel.GetCompleted(userID)
	card := tutorial.ShouldShow(tutorial.TopicRealizedPnl, completed, lang)
	if card != nil {
		card = tutorial.ForRealizedPnl(tutorial.TriggerContext{
			RawPnl:  result.RawPnl.StringFixed(2),
			Pnl:     result.RealizedPnl.StringFixed(2),
			Fee:     result.Fee.StringFixed(2),
			Funding: result.FundingPnl.StringFixed(2),
			NetPnl:  result.NetPnl.StringFixed(2),
			Lang:    lang,
		})
		l.svcCtx.TutorialModel.MarkComplete(userID, tutorial.TopicRealizedPnl)
	}

	return resp, card, nil
}

func (l *PositionLogic) UpdateTPSL(userID int64, req *types.UpdateTPSLReq, lang string) (*types.TutorialCard, error) {
	pos, err := l.svcCtx.PositionModel.FindByID(req.PositionID)
	if err != nil {
		return nil, err
	}
	if pos == nil || pos.UserID != userID || pos.Status != model.PositionStatusActive {
		return nil, errors.New("position not found or not active")
	}

	var tp, sl *decimal.Decimal
	if req.TakeProfit != "" {
		d, err := decimal.NewFromString(req.TakeProfit)
		if err != nil {
			return nil, err
		}
		if pos.Side == 1 && d.LessThanOrEqual(pos.EntryPrice) {
			return nil, errors.New("take profit must be above entry price for long positions")
		}
		if pos.Side == -1 && d.GreaterThanOrEqual(pos.EntryPrice) {
			return nil, errors.New("take profit must be below entry price for short positions")
		}
		tp = &d
	}
	if req.StopLoss != "" {
		d, err := decimal.NewFromString(req.StopLoss)
		if err != nil {
			return nil, err
		}
		if pos.Side == 1 && d.GreaterThanOrEqual(pos.EntryPrice) {
			return nil, errors.New("stop loss must be below entry price for long positions")
		}
		if pos.Side == -1 && d.LessThanOrEqual(pos.EntryPrice) {
			return nil, errors.New("stop loss must be above entry price for short positions")
		}
		sl = &d
	}

	if err := l.svcCtx.PositionModel.UpdateTPSL(req.PositionID, tp, sl); err != nil {
		return nil, err
	}

	// Update cache
	l.svcCtx.PositionCache.Update(req.PositionID, func(cp *cache.CachedPosition) {
		if tp != nil {
			cp.TakeProfit = tp.String()
		}
		if sl != nil {
			cp.StopLoss = sl.String()
		}
	})

	completed, _ := l.svcCtx.TutorialModel.GetCompleted(userID)
	card := tutorial.ShouldShow(tutorial.TopicTPSL, completed, lang)
	if card != nil {
		l.svcCtx.TutorialModel.MarkComplete(userID, tutorial.TopicTPSL)
	}
	return card, nil
}

func (l *PositionLogic) List(userID int64) (*types.PositionListResp, error) {
	// Read from memory cache (consistent with ClosePosition which also reads from cache)
	cached := l.svcCtx.PositionCache.GetByUser(userID)
	currentPrice := l.svcCtx.PriceCache.GetPrice()
	list := make([]types.PositionInfo, 0, len(cached))

	for _, cp := range cached {
		entryPrice, _ := decimal.NewFromString(cp.EntryPrice)
		quantity, _ := decimal.NewFromString(cp.Quantity)
		margin, _ := decimal.NewFromString(cp.Margin)
		fundingPnl, _ := decimal.NewFromString(cp.FundingPnl)

		upnl := position.CalcUnrealizedPnL(entryPrice, currentPrice, quantity, cp.Side)
		roi := position.CalcROI(upnl, margin)
		marginRatio := position.CalcMarginRatio(margin, upnl, quantity, currentPrice)
		adlLevel := adl.GetADLIndicator(roi, len(cached))

		info := types.PositionInfo{
			ID:            cp.ID,
			Side:          cp.Side,
			MarginMode:    cp.MarginMode,
			Leverage:      cp.Leverage,
			EntryPrice:    cp.EntryPrice,
			Quantity:      cp.Quantity,
			Margin:        margin.StringFixed(2),
			LiqPrice:      cp.LiqPrice,
			ForceTpPrice:  cp.ForceTpPrice,
			UnrealizedPnl: upnl.StringFixed(2),
			ROI:           roi.StringFixed(2),
			MarginRatio:   marginRatio.StringFixed(4),
			FundingPnl:    fundingPnl.StringFixed(2),
			ADLIndicator:  adlLevel,
		}
		if cp.TakeProfit != "" {
			info.TakeProfit = cp.TakeProfit
		}
		if cp.StopLoss != "" {
			info.StopLoss = cp.StopLoss
		}
		list = append(list, info)
	}

	return &types.PositionListResp{Positions: list}, nil
}
