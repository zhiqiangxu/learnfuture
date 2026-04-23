package handler

import (
	"net/http"
	"strconv"

	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func HistoryOrdersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		page, pageSize := parsePagination(r)
		offset := (page - 1) * pageSize

		orders, err := svcCtx.OrderModel.FindByUserPaged(userID, offset, pageSize)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		list := make([]types.OrderHistory, 0, len(orders))
		for _, o := range orders {
			oh := types.OrderHistory{
				ID:         o.ID,
				Side:       o.Side,
				OrderType:  o.OrderType,
				Leverage:   o.Leverage,
				Quantity:   o.Quantity.String(),
				MarginCost: o.MarginCost.String(),
				Status:     o.Status,
				CreatedAt:  o.CreatedAt.Unix(),
			}
			if o.Price != nil {
				oh.Price = o.Price.String()
			}
			if o.FilledPrice != nil {
				oh.FilledPrice = o.FilledPrice.String()
			}
			list = append(list, oh)
		}
		response.Success(w, map[string]interface{}{"orders": list})
	}
}

func HistoryTradesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		page, pageSize := parsePagination(r)
		offset := (page - 1) * pageSize

		trades, err := svcCtx.TradeModel.FindByUserPaged(userID, offset, pageSize)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		list := make([]types.TradeHistory, 0, len(trades))
		for _, t := range trades {
			list = append(list, types.TradeHistory{
				ID:          t.ID,
				Side:        t.Side,
				Price:       t.Price.String(),
				Quantity:    t.Quantity.String(),
				Fee:         t.Fee.String(),
				RealizedPnl: t.RealizedPnl.String(),
				IsClose:     t.IsClose,
				CloseReason: t.CloseReason,
				CreatedAt:   t.CreatedAt.Unix(),
			})
		}
		response.Success(w, map[string]interface{}{"trades": list})
	}
}

func HistoryFundingHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		page, pageSize := parsePagination(r)
		offset := (page - 1) * pageSize

		settlements, err := svcCtx.FundingModel.FindSettlementsByUser(userID, offset, pageSize)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		list := make([]types.FundingHistory, 0, len(settlements))
		for _, s := range settlements {
			list = append(list, types.FundingHistory{
				Rate:          s.Rate.String(),
				PositionValue: s.PositionValue.String(),
				Payment:       s.Payment.String(),
				SettleTime:    s.SettleTime.Unix(),
			})
		}
		response.Success(w, map[string]interface{}{"fundings": list})
	}
}

func PnlSummaryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		summary, err := svcCtx.TradeModel.GetPnlSummary(userID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		totalFunding, _ := svcCtx.FundingModel.GetTotalFunding(userID)

		total := summary.WinCount + summary.LossCount
		winRate := "0"
		if total > 0 {
			winRate = strconv.FormatFloat(float64(summary.WinCount)/float64(total)*100, 'f', 1, 64)
		}

		response.Success(w, &types.PnlSummary{
			TotalPnl:      summary.TotalPnl.String(),
			TotalFee:       summary.TotalFee.String(),
			TotalFunding:   totalFunding.String(),
			WinCount:       summary.WinCount,
			LossCount:      summary.LossCount,
			WinRate:        winRate,
			BestTrade:      summary.BestTrade.String(),
			WorstTrade:     summary.WorstTrade.String(),
			LiquidatedCnt:  summary.LiquidatedCount,
			ForceTpCnt:     summary.ForceTpCount,
		})
	}
}

func parsePagination(r *http.Request) (int, int) {
	page := 1
	pageSize := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && v > 0 && v <= 100 {
		pageSize = v
	}
	return page, pageSize
}
