package vdigateway

import (
	"errors"
	"strings"
	"testing"
)

func TestTokenHashRejectsEmptyToken(t *testing.T) {
	_, err := TokenHash(" ")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("TokenHash err = %v, want ErrInvalidToken", err)
	}
}

func TestSecureTokenGeneratorReturnsURLSafeToken(t *testing.T) {
	token, err := SecureTokenGenerator{}.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if strings.ContainsAny(token, "/+=") {
		t.Fatalf("token is not raw-url-safe: %q", token)
	}
	hash, err := TokenHash(token)
	if err != nil {
		t.Fatalf("TokenHash: %v", err)
	}
	if strings.Contains(hash, token) {
		t.Fatalf("hash contains raw token: %q", hash)
	}
}
