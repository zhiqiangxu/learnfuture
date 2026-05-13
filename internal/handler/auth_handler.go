package handler

import (
	"encoding/json"
	"net/http"

	"learn_future/internal/logic"
	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func WxLoginHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.WxLoginReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		l := logic.NewAuthLogic(svcCtx)
		lang := tutorial.ParseLang(r.Header.Get("Accept-Language"))
		resp, tut, err := l.WxLogin(&req, lang)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.SuccessWithTutorial(w, resp, tut)
	}
}

func RefreshTokenHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RefreshTokenReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		l := logic.NewAuthLogic(svcCtx)
		resp, err := l.RefreshToken(&req)
		if err != nil {
			response.Error(w, 401, err.Error())
			return
		}
		response.Success(w, resp)
	}
}
