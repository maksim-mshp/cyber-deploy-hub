package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleStudent = "student"
	RoleTeacher = "teacher"
)

type Config struct {
	SessionSecret            string
	SessionTTL               time.Duration
	CookieName               string
	CookieSecure             bool
	CookieSameSite           string
	CookieDomain             string
	LocalUsersJSON           string
	LocalStudentLoginEnabled bool
}

type LocalUser struct {
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
	Role         string `json:"role"`
	DisplayName  string `json:"display_name,omitempty"`
}

type Principal struct {
	Subject     string    `json:"subject"`
	Role        string    `json:"role"`
	DisplayName string    `json:"display_name,omitempty"`
	Source      string    `json:"source,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResult struct {
	User      Principal `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"-"`
}

type Service struct {
	users                    map[string]LocalUser
	sessionSecret            []byte
	sessionTTL               time.Duration
	cookieName               string
	cookieSecure             bool
	cookieSameSite           http.SameSite
	cookieDomain             string
	localStudentLoginEnabled bool
	now                      func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	secret := strings.TrimSpace(cfg.SessionSecret)
	if len(secret) < 16 {
		return nil, errors.New("AUTH_SESSION_SECRET must contain at least 16 characters")
	}
	cookieName := strings.TrimSpace(cfg.CookieName)
	if cookieName == "" {
		cookieName = "cdh_session"
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	sameSite, err := parseCookieSameSite(cfg.CookieSameSite)
	if err != nil {
		return nil, err
	}
	if sameSite == http.SameSiteNoneMode && !cfg.CookieSecure {
		return nil, errors.New("AUTH_COOKIE_SECURE must be true when AUTH_COOKIE_SAME_SITE=none")
	}
	users, err := parseLocalUsers(cfg.LocalUsersJSON)
	if err != nil {
		return nil, err
	}
	return &Service{
		users:                    users,
		sessionSecret:            []byte(secret),
		sessionTTL:               ttl,
		cookieName:               cookieName,
		cookieSecure:             cfg.CookieSecure,
		cookieSameSite:           sameSite,
		cookieDomain:             strings.TrimSpace(cfg.CookieDomain),
		localStudentLoginEnabled: cfg.LocalStudentLoginEnabled,
		now:                      time.Now,
	}, nil
}

func (s *Service) CookieName() string {
	if s == nil || s.cookieName == "" {
		return "cdh_session"
	}
	return s.cookieName
}

func (s *Service) CookieSecure() bool {
	return s != nil && s.cookieSecure
}

func (s *Service) CookieSameSite() http.SameSite {
	if s == nil || s.cookieSameSite == 0 {
		return http.SameSiteLaxMode
	}
	return s.cookieSameSite
}

func (s *Service) CookieDomain() string {
	if s == nil {
		return ""
	}
	return s.cookieDomain
}

func (s *Service) Login(_ context.Context, req LoginRequest) (LoginResult, error) {
	if s == nil {
		return LoginResult{}, errors.New("auth service is nil")
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	user, ok := s.users[username]
	if !ok || !user.passwordMatches(password) {
		return LoginResult{}, errors.New("invalid username or password")
	}
	if user.Role == RoleStudent && !s.localStudentLoginEnabled {
		return LoginResult{}, errors.New("local student login is disabled; launch labs through LTI")
	}
	principal := Principal{
		Subject:     user.Username,
		Role:        user.Role,
		DisplayName: user.displayName(),
		Source:      "local",
	}
	token, expiresAt, err := s.IssueSession(principal)
	if err != nil {
		return LoginResult{}, err
	}
	principal.ExpiresAt = expiresAt
	return LoginResult{User: principal, ExpiresAt: expiresAt, Token: token}, nil
}

func (s *Service) IssueSession(principal Principal) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, errors.New("auth service is nil")
	}
	principal.Subject = strings.TrimSpace(principal.Subject)
	principal.Role = strings.TrimSpace(principal.Role)
	principal.Source = strings.TrimSpace(principal.Source)
	if principal.Source == "" {
		principal.Source = "local"
	}
	if principal.Subject == "" {
		return "", time.Time{}, errors.New("subject is required")
	}
	if err := validateRole(principal.Role); err != nil {
		return "", time.Time{}, err
	}
	expiresAt := s.now().Add(s.sessionTTL).UTC()
	principal.ExpiresAt = expiresAt
	rawPayload, err := json.Marshal(sessionPayload{
		Subject:     principal.Subject,
		Role:        principal.Role,
		DisplayName: principal.DisplayName,
		Source:      principal.Source,
		ExpiresAt:   expiresAt.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(rawPayload)
	signature := s.sign(payload)
	return payload + "." + signature, expiresAt, nil
}

func (s *Service) AuthenticateToken(token string) (Principal, error) {
	if s == nil {
		return Principal{}, errors.New("auth service is nil")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Principal{}, errors.New("invalid session token")
	}
	expected := s.sign(parts[0])
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expected)) != 1 {
		return Principal{}, errors.New("invalid session signature")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, errors.New("invalid session payload")
	}
	var payload sessionPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return Principal{}, errors.New("invalid session payload")
	}
	if payload.Subject == "" || validateRole(payload.Role) != nil {
		return Principal{}, errors.New("invalid session principal")
	}
	expiresAt := time.Unix(payload.ExpiresAt, 0).UTC()
	if !expiresAt.After(s.now()) {
		return Principal{}, errors.New("session expired")
	}
	return Principal{
		Subject:     payload.Subject,
		Role:        payload.Role,
		DisplayName: payload.DisplayName,
		Source:      payload.Source,
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *Service) sign(payload string) string {
	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type sessionPayload struct {
	Subject     string `json:"sub"`
	Role        string `json:"role"`
	DisplayName string `json:"name,omitempty"`
	Source      string `json:"src,omitempty"`
	ExpiresAt   int64  `json:"exp"`
}

func parseLocalUsers(raw string) (map[string]LocalUser, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]LocalUser{}, nil
	}
	var users []LocalUser
	if err := json.Unmarshal([]byte(raw), &users); err != nil {
		return nil, fmt.Errorf("decode AUTH_LOCAL_USERS_JSON: %w", err)
	}
	result := make(map[string]LocalUser, len(users))
	for _, user := range users {
		user.Username = strings.TrimSpace(user.Username)
		user.Role = strings.TrimSpace(user.Role)
		user.DisplayName = strings.TrimSpace(user.DisplayName)
		if user.Username == "" {
			return nil, errors.New("auth user username is required")
		}
		if err := validateRole(user.Role); err != nil {
			return nil, fmt.Errorf("auth user %s: %w", user.Username, err)
		}
		if user.Password == "" && user.PasswordHash == "" {
			return nil, fmt.Errorf("auth user %s must include password or password_hash", user.Username)
		}
		result[user.Username] = user
	}
	return result, nil
}

func (u LocalUser) passwordMatches(password string) bool {
	if u.PasswordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) == 1
}

func (u LocalUser) displayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func parseCookieSameSite(raw string) (http.SameSite, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	case "default":
		return http.SameSiteDefaultMode, nil
	default:
		return 0, fmt.Errorf("unsupported AUTH_COOKIE_SAME_SITE %q", raw)
	}
}

func validateRole(role string) error {
	switch role {
	case RoleStudent, RoleTeacher:
		return nil
	default:
		return fmt.Errorf("unsupported role %q", role)
	}
}
