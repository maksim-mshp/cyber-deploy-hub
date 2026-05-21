package lmsgateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Authenticator struct {
	secret []byte
	skew   time.Duration
	now    func() time.Time
}

func NewAuthenticator(secret string, skew time.Duration) (*Authenticator, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("LMS_SHARED_SECRET is required")
	}
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	return &Authenticator{secret: []byte(secret), skew: skew, now: time.Now}, nil
}

func (a *Authenticator) Authenticate(r *http.Request, body []byte) error {
	if a == nil {
		return errors.New("authenticator is nil")
	}
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		if subtle.ConstantTimeCompare([]byte(token), a.secret) == 1 {
			return nil
		}
		return errors.New("invalid bearer token")
	}
	timestamp := strings.TrimSpace(r.Header.Get("X-LMS-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-LMS-Signature"))
	if timestamp == "" || signature == "" {
		return errors.New("missing LMS signature headers")
	}
	if err := a.validateTimestamp(timestamp); err != nil {
		return err
	}
	expected := a.sign(timestamp, body)
	signature = strings.TrimPrefix(signature, "sha256=")
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return errors.New("invalid LMS signature")
	}
	return nil
}

func (a *Authenticator) sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) validateTimestamp(value string) error {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid LMS timestamp: %w", err)
	}
	occurredAt := time.Unix(seconds, 0)
	now := a.now()
	if occurredAt.Before(now.Add(-a.skew)) || occurredAt.After(now.Add(a.skew)) {
		return errors.New("LMS timestamp is outside allowed skew")
	}
	return nil
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
