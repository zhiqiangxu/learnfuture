package handler

import (
	"net/http"

	"learn_future/internal/logic"
	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/pkg/response"
)

func AccountInfoHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		l := logic.NewAccountLogic(svcCtx)
		resp, err := l.GetInfo(userID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.Success(w, resp)
	}
}

func ResetAccountHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())
		l := logic.NewAccountLogic(svcCtx)
		resp, err := l.Reset(userID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.Success(w, resp)
	}
}
