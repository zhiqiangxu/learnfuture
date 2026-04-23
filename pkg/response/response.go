package response

import (
	"encoding/json"
	"net/http"

	"learn_future/internal/types"
)

func Success(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, &types.Response{
		Code:    0,
		Message: "ok",
		Data:    data,
	})
}

func SuccessWithTutorial(w http.ResponseWriter, data interface{}, tutorial *types.TutorialCard) {
	writeJSON(w, http.StatusOK, &types.Response{
		Code:     0,
		Message:  "ok",
		Data:     data,
		Tutorial: tutorial,
	})
}

func Error(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, http.StatusOK, &types.Response{
		Code:    code,
		Message: msg,
	})
}

func Unauthorized(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusUnauthorized, &types.Response{
		Code:    401,
		Message: msg,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, resp *types.Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	data, err := json.Marshal(resp)
	if err != nil {
		w.Write([]byte(`{"code":500,"message":"internal error"}`))
		return
	}
	w.Write(data)
}
