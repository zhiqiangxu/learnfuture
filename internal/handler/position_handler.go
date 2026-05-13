package handler

import (
	"encoding/json"
	"net/http"

	"learn_future/internal/logic"
	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func ClosePositionHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ClosePositionReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		userID := middleware.GetUserID(r.Context())
		l := logic.NewPositionLogic(svcCtx)
		lang := tutorial.ParseLang(r.Header.Get("Accept-Language"))
		resp, tut, err := l.Close(userID, req.PositionID, req.Quantity, lang)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.SuccessWithTutorial(w, resp, tut)
	}
}

func UpdateTPSLHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UpdateTPSLReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		userID := middleware.GetUserID(r.Context())
		l := logic.NewPositionLogic(svcCtx)
		lang := tutorial.ParseLang(r.Header.Get("Accept-Language"))
		tut, err := l.UpdateTPSL(userID, &req, lang)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.SuccessWithTutorial(w, nil, tut)
	}
}

func PositionListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		l := logic.NewPositionLogic(svcCtx)
		resp, err := l.List(userID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.Success(w, resp)
	}
}

func PendingOrdersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		orders := svcCtx.OrderCache.GetByUser(userID)
		var list []map[string]interface{}
		for _, o := range orders {
			list = append(list, map[string]interface{}{
				"id":          o.ID,
				"side":        o.Side,
				"price":       o.Price.String(),
				"quantity":    o.Quantity.String(),
				"margin_cost": o.MarginCost.String(),
				"leverage":    o.Leverage,
			})
		}
		if list == nil {
			list = []map[string]interface{}{}
		}
		response.Success(w, map[string]interface{}{"orders": list})
	}
}
