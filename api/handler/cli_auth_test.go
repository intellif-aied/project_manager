package handler

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCLIAuthSkipsOnlyExpiration(t *testing.T) {
	secret := "cli-auth-test-secret"
	expired := signedTestToken(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
		"uid": 42, "iat": time.Now().Add(-2 * time.Hour).Unix(), "exp": time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := extractAIHubUID(expired, secret); err == nil {
		t.Fatal("web auth accepted an expired token")
	}
	uid, err := extractAIHubUIDWithPolicy(expired, secret, true)
	if err != nil || uid != 42 {
		t.Fatalf("cli uid=%d err=%v", uid, err)
	}

	future := signedTestToken(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
		"uid": 42, "nbf": time.Now().Add(time.Hour).Unix(), "exp": time.Now().Add(2 * time.Hour).Unix(),
	})
	if _, err := extractAIHubUIDWithPolicy(future, secret, true); err == nil {
		t.Fatal("cli auth accepted a token before nbf")
	}
	wrongAlgorithm := signedTestToken(t, jwt.SigningMethodHS512, secret, jwt.MapClaims{
		"uid": 42, "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := extractAIHubUIDWithPolicy(wrongAlgorithm, secret, true); err == nil {
		t.Fatal("cli auth accepted a non-HS256 token")
	}
}

func signedTestToken(t *testing.T, method jwt.SigningMethod, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
