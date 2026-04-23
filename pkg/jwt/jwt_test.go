package jwt

import (
	"testing"
)

const testSecret = "test-secret-key-for-unit-tests"

func TestGenerateAndParseToken(t *testing.T) {
	token, err := GenerateToken(12345, testSecret, 3600)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := ParseToken(token, testSecret)
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	if claims.UserID != 12345 {
		t.Errorf("expected userID 12345, got %d", claims.UserID)
	}
}

func TestParseToken_Expired(t *testing.T) {
	// Generate with 0 seconds expiry (already expired)
	token, err := GenerateToken(1, testSecret, -1)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = ParseToken(token, testSecret)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestParseToken_Invalid(t *testing.T) {
	_, err := ParseToken("invalid.token.here", testSecret)
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestParseToken_WrongSecret(t *testing.T) {
	token, _ := GenerateToken(1, testSecret, 3600)
	_, err := ParseToken(token, "wrong-secret")
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}
