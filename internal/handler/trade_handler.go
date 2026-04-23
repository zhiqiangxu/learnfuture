package handler

import (
	"encoding/json"
	"net/http"

	"learn_future/internal/logic"
	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func PlaceOrderHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.PlaceOrderReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		userID := middleware.GetUserID(r.Context())
		l := logic.NewTradeLogic(svcCtx)
		resp, tutorial, err := l.PlaceOrder(userID, &req)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.SuccessWithTutorial(w, resp, tutorial)
	}
}

func CancelOrderHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CancelOrderReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		userID := middleware.GetUserID(r.Context())
		l := logic.NewTradeLogic(svcCtx)
		err := l.CancelOrder(userID, req.OrderID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.Success(w, nil)
	}
}
