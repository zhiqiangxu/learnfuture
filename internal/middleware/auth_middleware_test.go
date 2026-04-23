package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"learn_future/pkg/jwt"
)

const testSecret = "test-secret-for-middleware"

func TestAuthMiddleware_ValidToken(t *testing.T) {
	token, _ := jwt.GenerateToken(123, testSecret, 3600)

	handler := AuthMiddleware(testSecret)(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserID(r.Context())
		if userID != 123 {
			t.Errorf("expected userID=123, got %d", userID)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	handler := AuthMiddleware(testSecret)(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	handler := AuthMiddleware(testSecret)(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_TokenInQuery(t *testing.T) {
	token, _ := jwt.GenerateToken(456, testSecret, 3600)

	handler := AuthMiddleware(testSecret)(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserID(r.Context())
		if userID != 456 {
			t.Errorf("expected userID=456, got %d", userID)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test?token="+token, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	token, _ := jwt.GenerateToken(1, testSecret, -1)

	handler := AuthMiddleware(testSecret)(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}
