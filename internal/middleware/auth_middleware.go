package middleware

import (
	"context"
	"net/http"
	"strings"

	"learn_future/pkg/jwt"
	"learn_future/pkg/response"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func AuthMiddleware(secret string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				response.Unauthorized(w, "missing token")
				return
			}

			claims, err := jwt.ParseToken(token, secret)
			if err != nil {
				response.Unauthorized(w, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			next(w, r.WithContext(ctx))
		}
	}
}

func GetUserID(ctx context.Context) int64 {
	userID, _ := ctx.Value(UserIDKey).(int64)
	return userID
}

func extractToken(r *http.Request) string {
	// Try Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Try query parameter (for WebSocket)
	return r.URL.Query().Get("token")
}
