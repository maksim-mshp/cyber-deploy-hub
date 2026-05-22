package authn

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestServiceLoginAndAuthenticateToken(t *testing.T) {
	service, err := NewService(Config{
		SessionSecret:  "0123456789abcdef",
		SessionTTL:     time.Hour,
		LocalUsersJSON: `[{"username":"student-1","password":"pass","role":"student","display_name":"Student One"}]`,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	now := time.Unix(100, 0)
	service.now = func() time.Time { return now }

	result, err := service.Login(context.Background(), LoginRequest{Username: "student-1", Password: "pass"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.User.Role != RoleStudent || result.User.Subject != "student-1" {
		t.Fatalf("user = %#v", result.User)
	}

	principal, err := service.AuthenticateToken(result.Token)
	if err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}
	if principal.Subject != "student-1" || principal.DisplayName != "Student One" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestServiceRejectsExpiredToken(t *testing.T) {
	service, err := NewService(Config{
		SessionSecret: "0123456789abcdef",
		SessionTTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.now = func() time.Time { return time.Unix(100, 0) }
	token, _, err := service.IssueSession(Principal{Subject: "teacher-1", Role: RoleTeacher})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	service.now = func() time.Time { return time.Unix(200, 0) }
	if _, err := service.AuthenticateToken(token); err == nil {
		t.Fatal("expected expired token error")
	}
}

func TestServiceLoginWithBcryptPasswordHash(t *testing.T) {
	service, err := NewService(Config{
		SessionSecret:  "0123456789abcdef",
		SessionTTL:     time.Hour,
		LocalUsersJSON: `[{"username":"teacher","password_hash":"$2y$12$QRNGWmEbCQcqAJtaYKV/EeAOsmvFeWkeihqxzZwa8EtNXSw5WvO4O","role":"teacher"}]`,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.Login(context.Background(), LoginRequest{Username: "teacher", Password: "teacher"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.User.Role != RoleTeacher || result.User.Subject != "teacher" {
		t.Fatalf("user = %#v", result.User)
	}
}

func TestServiceCookiePolicy(t *testing.T) {
	service, err := NewService(Config{
		SessionSecret:  "0123456789abcdef",
		CookieSecure:   true,
		CookieSameSite: "none",
		CookieDomain:   ".example.com",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if !service.CookieSecure() {
		t.Fatal("cookie must be secure")
	}
	if service.CookieSameSite() != http.SameSiteNoneMode {
		t.Fatalf("same_site = %v", service.CookieSameSite())
	}
	if service.CookieDomain() != ".example.com" {
		t.Fatalf("domain = %q", service.CookieDomain())
	}
}

func TestServiceRejectsSameSiteNoneWithoutSecureCookie(t *testing.T) {
	_, err := NewService(Config{
		SessionSecret:  "0123456789abcdef",
		CookieSameSite: "none",
	})
	if err == nil {
		t.Fatal("expected SameSite=None without Secure to be rejected")
	}
}
