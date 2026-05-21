package lmsgateway

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatorAcceptsHMACSignature(t *testing.T) {
	t.Parallel()

	auth, err := NewAuthenticator("secret", time.Minute)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	auth.now = func() time.Time { return time.Unix(100, 0) }

	body := []byte(`{"moodle_user_id":"u1"}`)
	req, err := http.NewRequest(http.MethodPost, "/lms/moodle/launch", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-LMS-Timestamp", "100")
	req.Header.Set("X-LMS-Signature", auth.sign("100", body))

	if err := auth.Authenticate(req, body); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticatorRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()

	auth, err := NewAuthenticator("secret", time.Minute)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	auth.now = func() time.Time { return time.Unix(200, 0) }

	body := []byte(`{}`)
	req, err := http.NewRequest(http.MethodPost, "/lms/moodle/launch", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-LMS-Timestamp", "100")
	req.Header.Set("X-LMS-Signature", auth.sign("100", body))

	if err := auth.Authenticate(req, body); err == nil {
		t.Fatal("Authenticate succeeded for stale timestamp")
	}
}
