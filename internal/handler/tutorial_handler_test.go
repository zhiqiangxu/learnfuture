package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"learn_future/internal/middleware"
	"learn_future/internal/types"
	"learn_future/testutil"
)

func withUserID(r *http.Request, userID int64) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	return r.WithContext(ctx)
}

func TestCompleteTutorialHandler_InvalidTopicID(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	body := `{"topic_id":"nonexistent_topic"}`
	req := httptest.NewRequest("POST", "/api/v1/tutorial/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, 1)
	rr := httptest.NewRecorder()

	CompleteTutorialHandler(svcCtx)(rr, req)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code == 0 {
		t.Error("expected error for invalid topic_id")
	}
}

func TestCompleteTutorialHandler_InvalidJSON(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	req := httptest.NewRequest("POST", "/api/v1/tutorial/complete", strings.NewReader("bad"))
	req = withUserID(req, 1)
	rr := httptest.NewRecorder()

	CompleteTutorialHandler(svcCtx)(rr, req)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 400 {
		t.Errorf("expected code=400, got %d", resp.Code)
	}
}
