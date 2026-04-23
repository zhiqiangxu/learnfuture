package handler

import (
	"net/http"
	"strconv"

	"learn_future/internal/svc"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func PriceHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ticker := svcCtx.PriceCache.GetTicker()
		response.Success(w, &types.PriceResp{
			Price:  ticker.Price.String(),
			Change: ticker.Change.StringFixed(2),
			High:   ticker.High.String(),
			Low:    ticker.Low.String(),
		})
	}
}

func KlinesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		interval := r.URL.Query().Get("interval")
		if interval == "" {
			interval = "1m"
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 1500 {
				limit = v
			}
		}

		// Try cache first
		bars := svcCtx.KlineAggregator.GetCachedKlines(interval, limit)
		if len(bars) >= limit || svcCtx.KlineModel == nil {
			klines := make([]types.Kline, 0, len(bars))
			for _, b := range bars {
				klines = append(klines, types.Kline{
					OpenTime:  b.OpenTime.UnixMilli(),
					Open:      b.Open.String(),
					High:      b.High.String(),
					Low:       b.Low.String(),
					Close:     b.Close.String(),
					Volume:    b.Volume.String(),
					CloseTime: b.CloseTime.UnixMilli(),
				})
			}
			response.Success(w, &types.KlineResp{Klines: klines})
			return
		}

		// Fallback to DB
		dbKlines, err := svcCtx.KlineModel.Find(interval, limit)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		klines := make([]types.Kline, 0, len(dbKlines))
		for _, k := range dbKlines {
			klines = append(klines, types.Kline{
				OpenTime:  k.OpenTime.UnixMilli(),
				Open:      k.Open.String(),
				High:      k.High.String(),
				Low:       k.Low.String(),
				Close:     k.Close.String(),
				Volume:    k.Volume.String(),
				CloseTime: k.CloseTime.UnixMilli(),
			})
		}
		response.Success(w, &types.KlineResp{Klines: klines})
	}
}

func MarkPriceHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		last, mark, index := svcCtx.MarkPriceEngine.GetPrices()
		basis := svcCtx.MarkPriceEngine.GetBasis()
		response.Success(w, map[string]interface{}{
			"last_price":  last.StringFixed(2),
			"mark_price":  mark.StringFixed(2),
			"index_price": index.StringFixed(2),
			"basis":       basis.StringFixed(6),
		})
	}
}

func InsuranceFundHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		balance := svcCtx.InsuranceFund.GetBalance()
		response.Success(w, map[string]interface{}{
			"balance": balance.StringFixed(2),
		})
	}
}

func DepthHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 20
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
				limit = v
			}
		}
		depth := svcCtx.OrderBook.Depth(limit)
		response.Success(w, depth)
	}
}

func FundingRateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rate := svcCtx.FundingScheduler.GetCurrentRate()
		nextSettle := svcCtx.FundingScheduler.GetNextSettleTime()
		response.Success(w, &types.FundingRateResp{
			Rate:       rate.String(),
			NextSettle: nextSettle.Unix(),
		})
	}
}
