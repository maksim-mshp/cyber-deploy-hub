package lmsgateway

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cyber-deploy-hub/internal/usecase/authn"
	lmsusecase "cyber-deploy-hub/internal/usecase/lmsgateway"
)

const (
	ltiMessageTypeResourceLinkRequest = "LtiResourceLinkRequest"

	ltiClaimDeploymentID = "https://purl.imsglobal.org/spec/lti/claim/deployment_id"
	ltiClaimMessageType  = "https://purl.imsglobal.org/spec/lti/claim/message_type"
	ltiClaimVersion      = "https://purl.imsglobal.org/spec/lti/claim/version"
	ltiClaimContext      = "https://purl.imsglobal.org/spec/lti/claim/context"
	ltiClaimResourceLink = "https://purl.imsglobal.org/spec/lti/claim/resource_link"
	ltiClaimCustom       = "https://purl.imsglobal.org/spec/lti/claim/custom"
)

type LTIConfig struct {
	PlatformIssuer   string
	ClientID         string
	AuthLoginURL     string
	JWKSURL          string
	PublicBaseURL    string
	RedirectURL      string
	DeploymentIDs    []string
	StateSecret      string
	AllowedClockSkew time.Duration
}

type LTIService struct {
	platformIssuer string
	clientID       string
	authLoginURL   string
	jwksURL        string
	publicBaseURL  string
	redirectURL    string
	deployments    map[string]struct{}
	stateSecret    []byte
	skew           time.Duration
	client         *http.Client
	now            func() time.Time

	mu          sync.Mutex
	cachedJWKS  jwksDocument
	jwksExpires time.Time
}

type ltiDiagnostics struct {
	Status         string                      `json:"status"`
	URLs           ltiDiagnosticsURLs          `json:"urls"`
	Platform       ltiPlatformDiagnostics      `json:"platform"`
	BrowserSession ltiBrowserSessionDiagnostic `json:"browser_session"`
	Warnings       []string                    `json:"warnings,omitempty"`
}

type ltiDiagnosticsURLs struct {
	OIDCLoginURL  string `json:"oidc_login_url"`
	LaunchURL     string `json:"launch_url"`
	TargetLinkURI string `json:"target_link_uri"`
	FrontendURL   string `json:"frontend_url"`
}

type ltiPlatformDiagnostics struct {
	Configured                   bool     `json:"configured"`
	Issuer                       string   `json:"issuer,omitempty"`
	ClientID                     string   `json:"client_id,omitempty"`
	AuthLoginURL                 string   `json:"auth_login_url,omitempty"`
	JWKSURL                      string   `json:"jwks_url,omitempty"`
	PublicBaseURL                string   `json:"public_base_url,omitempty"`
	RedirectURL                  string   `json:"redirect_url,omitempty"`
	DeploymentIDCount            int      `json:"deployment_id_count"`
	DeploymentRestrictionEnabled bool     `json:"deployment_restriction_enabled"`
	SupportedMessageTypes        []string `json:"supported_message_types,omitempty"`
}

type ltiBrowserSessionDiagnostic struct {
	Configured     bool   `json:"configured"`
	CookieName     string `json:"cookie_name,omitempty"`
	CookieSecure   bool   `json:"cookie_secure"`
	CookieSameSite string `json:"cookie_same_site,omitempty"`
	CookieDomain   string `json:"cookie_domain,omitempty"`
	IFrameReady    bool   `json:"iframe_ready"`
}

func NewLTIService(cfg LTIConfig, client *http.Client) (*LTIService, error) {
	cfg.PlatformIssuer = strings.TrimSpace(cfg.PlatformIssuer)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.AuthLoginURL = strings.TrimSpace(cfg.AuthLoginURL)
	cfg.JWKSURL = strings.TrimSpace(cfg.JWKSURL)
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	cfg.StateSecret = strings.TrimSpace(cfg.StateSecret)

	configured := cfg.PlatformIssuer != "" ||
		cfg.ClientID != "" ||
		cfg.AuthLoginURL != "" ||
		cfg.JWKSURL != "" ||
		cfg.PublicBaseURL != "" ||
		cfg.RedirectURL != "" ||
		len(cfg.DeploymentIDs) > 0
	if !configured {
		return nil, nil
	}
	if cfg.PlatformIssuer == "" {
		return nil, errors.New("LTI_PLATFORM_ISSUER is required when LTI is configured")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("LTI_CLIENT_ID is required when LTI is configured")
	}
	if cfg.AuthLoginURL == "" {
		return nil, errors.New("LTI_AUTH_LOGIN_URL is required when LTI is configured")
	}
	if cfg.JWKSURL == "" {
		return nil, errors.New("LTI_JWKS_URL is required when LTI is configured")
	}
	if cfg.PublicBaseURL != "" {
		parsed, err := url.Parse(cfg.PublicBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, errors.New("LTI_PUBLIC_BASE_URL must be an absolute URL")
		}
	}
	if len(cfg.StateSecret) < 16 {
		return nil, errors.New("AUTH_SESSION_SECRET must contain at least 16 characters for LTI state signing")
	}
	if cfg.AllowedClockSkew <= 0 {
		cfg.AllowedClockSkew = 5 * time.Minute
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	deployments := make(map[string]struct{}, len(cfg.DeploymentIDs))
	for _, deploymentID := range cfg.DeploymentIDs {
		deploymentID = strings.TrimSpace(deploymentID)
		if deploymentID != "" {
			deployments[deploymentID] = struct{}{}
		}
	}
	return &LTIService{
		platformIssuer: cfg.PlatformIssuer,
		clientID:       cfg.ClientID,
		authLoginURL:   cfg.AuthLoginURL,
		jwksURL:        cfg.JWKSURL,
		publicBaseURL:  cfg.PublicBaseURL,
		redirectURL:    cfg.RedirectURL,
		deployments:    deployments,
		stateSecret:    []byte(cfg.StateSecret),
		skew:           cfg.AllowedClockSkew,
		client:         client,
		now:            time.Now,
	}, nil
}

func splitDeploymentIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (s *Server) handleLTILogin(w http.ResponseWriter, r *http.Request) {
	if s.lti == nil {
		writeError(w, http.StatusServiceUnavailable, "lti_not_configured", "LTI 1.3 is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	iss := strings.TrimSpace(r.Form.Get("iss"))
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	loginHint := strings.TrimSpace(r.Form.Get("login_hint"))
	targetLinkURI := strings.TrimSpace(r.Form.Get("target_link_uri"))
	messageHint := strings.TrimSpace(r.Form.Get("lti_message_hint"))
	if iss == "" || clientID == "" || loginHint == "" || targetLinkURI == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "iss, client_id, login_hint and target_link_uri are required")
		return
	}
	redirectURL, err := s.lti.loginRedirectURL(r, iss, clientID, loginHint, targetLinkURI, messageHint)
	if err != nil {
		writeError(w, http.StatusBadRequest, "lti_login_rejected", err.Error())
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *Server) handleLTILaunch(w http.ResponseWriter, r *http.Request) {
	if s.lti == nil {
		writeError(w, http.StatusServiceUnavailable, "lti_not_configured", "LTI 1.3 is not configured")
		return
	}
	if s.sessionAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	idToken := strings.TrimSpace(r.Form.Get("id_token"))
	stateToken := strings.TrimSpace(r.Form.Get("state"))
	if idToken == "" || stateToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id_token and state are required")
		return
	}
	claims, err := s.lti.validateLaunch(r.Context(), idToken, stateToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "lti_launch_rejected", err.Error())
		return
	}
	launchReq, err := claims.launchRequest()
	if err != nil {
		writeError(w, http.StatusBadRequest, "lti_launch_rejected", err.Error())
		return
	}
	result, _, err := s.service.Launch(r.Context(), launchReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, "launch_rejected", err.Error())
		return
	}
	displayName := launchReq.UserLogin
	if displayName == "" {
		displayName = result.Mapping.StudentID
	}
	token, expiresAt, err := s.sessionAuth.IssueSession(authn.Principal{
		Subject:     result.Mapping.StudentID,
		Role:        authn.RoleStudent,
		DisplayName: displayName,
		Source:      "lti",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	http.SetCookie(w, s.sessionCookie(token, expiresAt))
	http.Redirect(w, r, s.redirectURL(result), http.StatusSeeOther)
}

func (s *Server) handleLTIToolConfiguration(w http.ResponseWriter, r *http.Request) {
	launchURL := absoluteURL(r, "/lti/1p3/launch")
	loginURL := absoluteURL(r, "/lti/1p3/login")
	status := "configured"
	if s.lti == nil {
		status = "disabled"
	} else {
		launchURL = s.lti.launchURL(r)
		loginURL = s.lti.loginURL(r)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                  "cyber-deploy-hub",
		"status":                  status,
		"oidc_login_url":          loginURL,
		"target_link_uri":         launchURL,
		"redirect_uris":           []string{launchURL},
		"launch_url":              launchURL,
		"deep_linking_url":        "",
		"public_jwks_url":         "",
		"supported_message_types": []string{ltiMessageTypeResourceLinkRequest},
	})
}

func (s *Server) handleLTIDiagnostics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ltiDiagnostics(r))
}

func (s *Server) ltiDiagnostics(r *http.Request) ltiDiagnostics {
	launchURL := absoluteURL(r, "/lti/1p3/launch")
	loginURL := absoluteURL(r, "/lti/1p3/login")
	status := "disabled"
	platform := ltiPlatformDiagnostics{Configured: false}
	if s.lti != nil {
		status = "configured"
		launchURL = s.lti.launchURL(r)
		loginURL = s.lti.loginURL(r)
		platform = ltiPlatformDiagnostics{
			Configured:                   true,
			Issuer:                       s.lti.platformIssuer,
			ClientID:                     s.lti.clientID,
			AuthLoginURL:                 s.lti.authLoginURL,
			JWKSURL:                      s.lti.jwksURL,
			PublicBaseURL:                s.lti.publicBaseURL,
			RedirectURL:                  s.lti.redirectURL,
			DeploymentIDCount:            len(s.lti.deployments),
			DeploymentRestrictionEnabled: len(s.lti.deployments) > 0,
			SupportedMessageTypes:        []string{ltiMessageTypeResourceLinkRequest},
		}
	}

	diagnostics := ltiDiagnostics{
		Status: status,
		URLs: ltiDiagnosticsURLs{
			OIDCLoginURL:  loginURL,
			LaunchURL:     launchURL,
			TargetLinkURI: launchURL,
			FrontendURL:   s.frontendURL,
		},
		Platform:       platform,
		BrowserSession: s.browserSessionDiagnostics(),
	}
	diagnostics.Warnings = ltiDiagnosticsWarnings(diagnostics)
	return diagnostics
}

func (s *Server) browserSessionDiagnostics() ltiBrowserSessionDiagnostic {
	if s.sessionAuth == nil {
		return ltiBrowserSessionDiagnostic{}
	}
	sameSite := sameSiteModeName(s.sessionAuth.CookieSameSite())
	secure := s.sessionAuth.CookieSecure()
	return ltiBrowserSessionDiagnostic{
		Configured:     true,
		CookieName:     s.sessionAuth.CookieName(),
		CookieSecure:   secure,
		CookieSameSite: sameSite,
		CookieDomain:   s.sessionAuth.CookieDomain(),
		IFrameReady:    secure && sameSite == "none",
	}
}

func ltiDiagnosticsWarnings(diagnostics ltiDiagnostics) []string {
	warnings := []string{}
	if !diagnostics.Platform.Configured {
		return append(warnings, "LTI 1.3 is not configured")
	}
	if !isPublicHTTPSURL(diagnostics.URLs.LaunchURL) {
		warnings = append(warnings, "LTI launch URL must be public HTTPS for Moodle production")
	}
	if !isPublicHTTPSURL(diagnostics.URLs.OIDCLoginURL) {
		warnings = append(warnings, "LTI login URL must be public HTTPS for Moodle production")
	}
	if !isPublicHTTPSURL(diagnostics.URLs.FrontendURL) {
		warnings = append(warnings, "AUTH_FRONTEND_URL must be a public HTTPS URL for real Moodle users")
	}
	if !diagnostics.BrowserSession.IFrameReady {
		warnings = append(warnings, "Embedded iframe sessions require AUTH_COOKIE_SAME_SITE=none and AUTH_COOKIE_SECURE=true")
	}
	if !diagnostics.Platform.DeploymentRestrictionEnabled {
		warnings = append(warnings, "LTI_DEPLOYMENT_IDS is empty; deployment_id is not restricted")
	}
	return warnings
}

func sameSiteModeName(mode http.SameSite) string {
	switch mode {
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	default:
		return "default"
	}
}

func isPublicHTTPSURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

func (l *LTIService) loginRedirectURL(r *http.Request, iss string, clientID string, loginHint string, targetLinkURI string, messageHint string) (string, error) {
	if iss != l.platformIssuer {
		return "", errors.New("unexpected LTI issuer")
	}
	if clientID != l.clientID {
		return "", errors.New("unexpected LTI client_id")
	}
	redirectURI := l.redirectURL
	if redirectURI == "" {
		redirectURI = l.launchURL(r)
	}
	nonce, err := randomToken(24)
	if err != nil {
		return "", err
	}
	state, err := l.signState(ltiState{
		Nonce:         nonce,
		Issuer:        iss,
		TargetLinkURI: targetLinkURI,
		ExpiresAt:     l.now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(l.authLoginURL)
	if err != nil {
		return "", fmt.Errorf("parse LTI_AUTH_LOGIN_URL: %w", err)
	}
	query := parsed.Query()
	query.Set("scope", "openid")
	query.Set("response_type", "id_token")
	query.Set("response_mode", "form_post")
	query.Set("prompt", "none")
	query.Set("client_id", l.clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("login_hint", loginHint)
	query.Set("state", state)
	query.Set("nonce", nonce)
	if messageHint != "" {
		query.Set("lti_message_hint", messageHint)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (l *LTIService) validateLaunch(ctx context.Context, idToken string, stateToken string) (ltiClaims, error) {
	state, err := l.verifyState(stateToken)
	if err != nil {
		return ltiClaims{}, err
	}
	claims, err := l.verifyIDToken(ctx, idToken)
	if err != nil {
		return ltiClaims{}, err
	}
	if claims.Nonce != state.Nonce {
		return ltiClaims{}, errors.New("LTI nonce mismatch")
	}
	if claims.Issuer != state.Issuer {
		return ltiClaims{}, errors.New("LTI issuer does not match state")
	}
	return claims, nil
}

func (l *LTIService) verifyIDToken(ctx context.Context, token string) (ltiClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ltiClaims{}, errors.New("invalid id_token")
	}
	var header jwtHeader
	if err := decodeSegment(parts[0], &header); err != nil {
		return ltiClaims{}, fmt.Errorf("decode JWT header: %w", err)
	}
	if header.Algorithm != "RS256" {
		return ltiClaims{}, fmt.Errorf("unsupported JWT alg %q", header.Algorithm)
	}
	key, err := l.publicKey(ctx, header.KeyID)
	if err != nil {
		return ltiClaims{}, err
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ltiClaims{}, errors.New("invalid JWT signature encoding")
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return ltiClaims{}, errors.New("invalid JWT signature")
	}
	var claims ltiClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return ltiClaims{}, fmt.Errorf("decode JWT claims: %w", err)
	}
	if err := l.validateClaims(claims); err != nil {
		return ltiClaims{}, err
	}
	return claims, nil
}

func (l *LTIService) validateClaims(claims ltiClaims) error {
	now := l.now()
	if claims.Issuer != l.platformIssuer {
		return errors.New("unexpected LTI issuer")
	}
	if !audienceContains(claims.Audience, l.clientID) {
		return errors.New("LTI audience does not include client_id")
	}
	if claims.Subject == "" {
		return errors.New("LTI subject is required")
	}
	if claims.ExpiresAt <= now.Add(-l.skew).Unix() {
		return errors.New("LTI id_token is expired")
	}
	if claims.NotBefore > 0 && claims.NotBefore > now.Add(l.skew).Unix() {
		return errors.New("LTI id_token is not valid yet")
	}
	if claims.IssuedAt > now.Add(l.skew).Unix() {
		return errors.New("LTI id_token issued_at is in the future")
	}
	if claims.MessageType != ltiMessageTypeResourceLinkRequest {
		return errors.New("LTI message_type must be LtiResourceLinkRequest")
	}
	if claims.Version != "" && claims.Version != "1.3.0" {
		return errors.New("LTI version must be 1.3.0")
	}
	if len(l.deployments) > 0 {
		if _, ok := l.deployments[claims.DeploymentID]; !ok {
			return errors.New("LTI deployment_id is not allowed")
		}
	}
	return nil
}

func (l *LTIService) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	kid = strings.TrimSpace(kid)
	if kid == "" {
		return nil, errors.New("JWT kid is required")
	}
	keys, err := l.jwks(ctx)
	if err != nil {
		return nil, err
	}
	for _, key := range keys.Keys {
		if key.KeyID == kid {
			return key.rsaPublicKey()
		}
	}
	return nil, errors.New("JWT kid was not found in JWKS")
}

func (l *LTIService) jwks(ctx context.Context) (jwksDocument, error) {
	l.mu.Lock()
	if len(l.cachedJWKS.Keys) > 0 && l.now().Before(l.jwksExpires) {
		keys := l.cachedJWKS
		l.mu.Unlock()
		return keys, nil
	}
	l.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.jwksURL, nil)
	if err != nil {
		return jwksDocument{}, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return jwksDocument{}, fmt.Errorf("fetch LTI JWKS: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jwksDocument{}, fmt.Errorf("fetch LTI JWKS: HTTP %d", resp.StatusCode)
	}
	var keys jwksDocument
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&keys); err != nil {
		return jwksDocument{}, fmt.Errorf("decode LTI JWKS: %w", err)
	}
	if len(keys.Keys) == 0 {
		return jwksDocument{}, errors.New("LTI JWKS contains no keys")
	}
	l.mu.Lock()
	l.cachedJWKS = keys
	l.jwksExpires = l.now().Add(5 * time.Minute)
	l.mu.Unlock()
	return keys, nil
}

func (l *LTIService) signState(state ltiState) (string, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signature := l.sign(payload)
	return payload + "." + signature, nil
}

func (l *LTIService) verifyState(token string) (ltiState, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ltiState{}, errors.New("invalid LTI state")
	}
	expected := l.sign(parts[0])
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expected)) != 1 {
		return ltiState{}, errors.New("invalid LTI state signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ltiState{}, errors.New("invalid LTI state payload")
	}
	var state ltiState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ltiState{}, errors.New("invalid LTI state payload")
	}
	if state.Nonce == "" || state.Issuer == "" {
		return ltiState{}, errors.New("invalid LTI state")
	}
	if state.ExpiresAt <= l.now().Unix() {
		return ltiState{}, errors.New("LTI state expired")
	}
	return state, nil
}

func (l *LTIService) sign(payload string) string {
	mac := hmac.New(sha256.New, l.stateSecret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c ltiClaims) launchRequest() (lmsusecase.LaunchRequest, error) {
	courseID := c.Context.ID
	localCourseID := customString(c.Custom, "course_id")
	assignmentID := customString(c.Custom, "assignment_id")
	if assignmentID == "" {
		assignmentID = c.ResourceLink.ID
	}
	labID := customString(c.Custom, "lab_id")
	idempotencyKey := customString(c.Custom, "idempotency_key")
	if idempotencyKey == "" && c.JWTID != "" {
		idempotencyKey = "lti:" + c.Issuer + ":" + c.JWTID
	}
	if strings.TrimSpace(courseID) == "" {
		return lmsusecase.LaunchRequest{}, errors.New("LTI context id is required")
	}
	if strings.TrimSpace(assignmentID) == "" {
		return lmsusecase.LaunchRequest{}, errors.New("LTI resource_link id is required")
	}
	displayName := strings.TrimSpace(c.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(c.Email)
	}
	return lmsusecase.LaunchRequest{
		MoodleUserID:       c.Subject,
		MoodleCourseID:     courseID,
		MoodleAssignmentID: assignmentID,
		UserLogin:          displayName,
		CourseName:         c.Context.Title,
		CourseID:           localCourseID,
		LabID:              labID,
		IdempotencyKey:     idempotencyKey,
	}, nil
}

func (l *LTIService) launchURL(r *http.Request) string {
	if l == nil {
		return absoluteURL(r, "/lti/1p3/launch")
	}
	if l.redirectURL != "" {
		return l.redirectURL
	}
	return l.publicURL(r, "/lti/1p3/launch")
}

func (l *LTIService) loginURL(r *http.Request) string {
	if l == nil {
		return absoluteURL(r, "/lti/1p3/login")
	}
	if l.publicBaseURL != "" {
		return publicURLFromBase(l.publicBaseURL, "/lti/1p3/login")
	}
	if l.redirectURL != "" {
		return loginURLFromLaunchURL(l.redirectURL)
	}
	return absoluteURL(r, "/lti/1p3/login")
}

func (l *LTIService) publicURL(r *http.Request, path string) string {
	if l != nil && l.publicBaseURL != "" {
		return publicURLFromBase(l.publicBaseURL, path)
	}
	return absoluteURL(r, path)
}

func publicURLFromBase(rawBase string, path string) string {
	parsed, err := url.Parse(rawBase)
	if err != nil {
		return path
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	suffix := "/" + strings.TrimLeft(path, "/")
	if basePath == "" {
		parsed.Path = suffix
	} else {
		parsed.Path = basePath + suffix
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func loginURLFromLaunchURL(rawLaunchURL string) string {
	parsed, err := url.Parse(rawLaunchURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "/lti/1p3/login"
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/launch") {
		parsed.Path = strings.TrimSuffix(path, "/launch") + "/login"
	} else {
		parsed.Path = "/lti/1p3/login"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func absoluteURL(r *http.Request, path string) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host + path
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSegment(segment string, target any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func audienceContains(raw json.RawMessage, clientID string) bool {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == clientID
	}
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err != nil {
		return false
	}
	for _, value := range multiple {
		if value == clientID {
			return true
		}
	}
	return false
}

func customString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

type ltiState struct {
	Nonce         string `json:"nonce"`
	Issuer        string `json:"iss"`
	TargetLinkURI string `json:"target_link_uri"`
	ExpiresAt     int64  `json:"exp"`
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

type ltiClaims struct {
	Issuer       string          `json:"iss"`
	Subject      string          `json:"sub"`
	Audience     json.RawMessage `json:"aud"`
	ExpiresAt    int64           `json:"exp"`
	IssuedAt     int64           `json:"iat"`
	NotBefore    int64           `json:"nbf,omitempty"`
	Nonce        string          `json:"nonce"`
	JWTID        string          `json:"jti,omitempty"`
	Name         string          `json:"name,omitempty"`
	Email        string          `json:"email,omitempty"`
	DeploymentID string          `json:"https://purl.imsglobal.org/spec/lti/claim/deployment_id"`
	MessageType  string          `json:"https://purl.imsglobal.org/spec/lti/claim/message_type"`
	Version      string          `json:"https://purl.imsglobal.org/spec/lti/claim/version"`
	Context      ltiContext      `json:"https://purl.imsglobal.org/spec/lti/claim/context"`
	ResourceLink ltiResourceLink `json:"https://purl.imsglobal.org/spec/lti/claim/resource_link"`
	Custom       map[string]any  `json:"https://purl.imsglobal.org/spec/lti/claim/custom,omitempty"`
}

type ltiContext struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Title string `json:"title,omitempty"`
}

type ltiResourceLink struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg,omitempty"`
	Use       string `json:"use,omitempty"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

func (k jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	if k.KeyType != "RSA" {
		return nil, fmt.Errorf("unsupported JWK kty %q", k.KeyType)
	}
	modulus, err := base64.RawURLEncoding.DecodeString(k.Modulus)
	if err != nil {
		return nil, errors.New("invalid JWK modulus")
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(k.Exponent)
	if err != nil {
		return nil, errors.New("invalid JWK exponent")
	}
	if len(exponentBytes) > 8 {
		return nil, errors.New("JWK exponent is too large")
	}
	var padded [8]byte
	copy(padded[8-len(exponentBytes):], exponentBytes)
	exponent := binary.BigEndian.Uint64(padded[:])
	if exponent == 0 || exponent > uint64(int(^uint(0)>>1)) {
		return nil, errors.New("invalid JWK exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(exponent),
	}, nil
}
