package vdigateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

const accessTokenBytes = 32

type TokenGenerator interface {
	Generate() (string, error)
}

type SecureTokenGenerator struct{}

func (SecureTokenGenerator) Generate() (string, error) {
	raw := make([]byte, accessTokenBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func TokenHash(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

var (
	ErrInvalidToken     = errors.New("invalid vdi token")
	ErrTokenExpired     = errors.New("vdi token expired")
	ErrTokenRevoked     = errors.New("vdi token revoked")
	ErrTokenNotFound    = errors.New("vdi token not found")
	ErrInstanceNotFound = errors.New("vdi instance not found")
)
