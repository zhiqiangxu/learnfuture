package jwt

import (
	"errors"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

type Claims struct {
	UserID int64 `json:"user_id"`
	jwtv5.RegisteredClaims
}

func GenerateToken(userID int64, secret string, expireSeconds int64) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwtv5.RegisteredClaims{
			ExpiresAt: jwtv5.NewNumericDate(now.Add(time.Duration(expireSeconds) * time.Second)),
			IssuedAt:  jwtv5.NewNumericDate(now),
			NotBefore: jwtv5.NewNumericDate(now),
		},
	}
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ParseToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwtv5.ParseWithClaims(tokenString, &Claims{}, func(t *jwtv5.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwtv5.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, ErrTokenInvalid
}
