package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"learn_future/internal/types"
	"learn_future/testutil"
)

func TestWxLoginHandler_EmptyCode(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	body := `{"code":""}`
	req := httptest.NewRequest("POST", "/api/v1/auth/wx-login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	WxLoginHandler(svcCtx)(rr, req)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code == 0 {
		t.Error("expected error for empty code")
	}
}

func TestWxLoginHandler_InvalidJSON(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	req := httptest.NewRequest("POST", "/api/v1/auth/wx-login", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	WxLoginHandler(svcCtx)(rr, req)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code == 0 {
		t.Error("expected error for invalid JSON")
	}
}

func TestRefreshTokenHandler_InvalidToken(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	body := `{"refresh_token":"invalid.token"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	RefreshTokenHandler(svcCtx)(rr, req)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code == 0 {
		t.Error("expected error for invalid refresh token")
	}
}
