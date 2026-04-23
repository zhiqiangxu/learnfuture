package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"learn_future/internal/types"
)

func TestSuccess(t *testing.T) {
	rr := httptest.NewRecorder()
	Success(rr, map[string]string{"hello": "world"})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Errorf("expected code=0, got %d", resp.Code)
	}
	if resp.Message != "ok" {
		t.Errorf("expected message=ok, got %s", resp.Message)
	}
}

func TestSuccessWithTutorial(t *testing.T) {
	rr := httptest.NewRecorder()
	card := &types.TutorialCard{
		ID:    "test",
		Title: "Test",
	}
	SuccessWithTutorial(rr, nil, card)

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Tutorial == nil {
		t.Error("expected tutorial in response")
	}
}

func TestError(t *testing.T) {
	rr := httptest.NewRecorder()
	Error(rr, 400, "bad request")

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 400 {
		t.Errorf("expected code=400, got %d", resp.Code)
	}
	if resp.Message != "bad request" {
		t.Errorf("expected 'bad request', got %s", resp.Message)
	}
}

func TestUnauthorized(t *testing.T) {
	rr := httptest.NewRecorder()
	Unauthorized(rr, "no token")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	Success(rr, nil)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}
