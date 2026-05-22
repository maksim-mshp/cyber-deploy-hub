package lmsgateway

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/authn"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	lmsusecase "cyber-deploy-hub/internal/usecase/lmsgateway"
)

func TestMoodleSSOIssuesStudentSessionCookieAndRedirects(t *testing.T) {
	server, sessionAuth := testServer(t, &testLMSRepository{})

	req := httptest.NewRequest(http.MethodPost, "/lms/moodle/sso", strings.NewReader(`{
		"moodle_user_id":"student-ext",
		"moodle_course_id":"course-ext",
		"moodle_assignment_id":"assignment-ext",
		"user_login":"Student One"
	}`))
	req.Header.Set("Authorization", "Bearer shared-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "launch_status=ACCEPTED") {
		t.Fatalf("location = %q", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	principal, err := sessionAuth.AuthenticateToken(cookies[0].Value)
	if err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}
	if principal.Subject != "moodle:student-ext" || principal.Role != authn.RoleStudent || principal.Source != "moodle" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestMoodleSSORedirectsToExistingActiveLabWithoutNewProvision(t *testing.T) {
	repo := &testLMSRepository{
		saved: lmsusecase.LaunchRecord{
			ID:             "11111111-1111-1111-1111-111111111111",
			LabRunID:       "22222222-2222-2222-2222-222222222222",
			LocalStudentID: "moodle:student-ext",
			LocalCourseID:  "course-3",
			LocalLabID:     "lab-3",
			Status:         lmsusecase.LaunchStatusActiveLabExists,
			CreatedAt:      time.Unix(100, 0).UTC(),
		},
		inserted: false,
	}
	server, sessionAuth := testServer(t, repo)

	req := httptest.NewRequest(http.MethodPost, "/lms/moodle/sso", strings.NewReader(`{
		"moodle_user_id":"student-ext",
		"moodle_course_id":"course-ext",
		"moodle_assignment_id":"assignment-ext"
	}`))
	req.Header.Set("Authorization", "Bearer shared-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "lab_run_id=22222222-2222-2222-2222-222222222222") ||
		!strings.Contains(location, "launch_status=ACTIVE_LAB_EXISTS") {
		t.Fatalf("location = %q", location)
	}
	if repo.command.MessageType != commands.RequestProvisionV1.String() {
		t.Fatalf("service must still build a provision command before repository guard, got %q", repo.command.MessageType)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	principal, err := sessionAuth.AuthenticateToken(cookies[0].Value)
	if err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}
	if principal.Subject != "moodle:student-ext" || principal.Role != authn.RoleStudent {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestLTILoginInitiationRedirectsToPlatform(t *testing.T) {
	lti := testLTIService(t, "https://moodle.example/mod/lti/certs.php")
	server, _ := testServerWithLTI(t, &testLMSRepository{}, lti)
	form := url.Values{
		"iss":              {"https://moodle.example"},
		"client_id":        {"client-1"},
		"login_hint":       {"login-hint-1"},
		"target_link_uri":  {"https://tool.example/lti/1p3/launch"},
		"lti_message_hint": {"message-hint-1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/lti/1p3/login", strings.NewReader(form.Encode()))
	req.Host = "tool.example"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if location.Scheme != "https" || location.Host != "moodle.example" || location.Path != "/mod/lti/auth.php" {
		t.Fatalf("location = %q", location.String())
	}
	query := location.Query()
	if query.Get("response_type") != "id_token" ||
		query.Get("response_mode") != "form_post" ||
		query.Get("prompt") != "none" ||
		query.Get("client_id") != "client-1" ||
		query.Get("login_hint") != "login-hint-1" ||
		query.Get("lti_message_hint") != "message-hint-1" ||
		query.Get("redirect_uri") != "https://tool.example/lti/1p3/launch" ||
		query.Get("state") == "" ||
		query.Get("nonce") == "" {
		t.Fatalf("unexpected login query: %s", location.RawQuery)
	}
}

func TestLTILaunchIssuesStudentSessionCookieAndRedirects(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"keys": []any{jwkFromPublicKey("kid-1", &privateKey.PublicKey)}})
	}))
	t.Cleanup(jwksServer.Close)

	lti := testLTIService(t, jwksServer.URL)
	now := time.Unix(1_700_000_000, 0).UTC()
	lti.now = func() time.Time { return now }
	server, sessionAuth := testServerWithLTI(t, &testLMSRepository{}, lti)
	state, err := lti.signState(ltiState{
		Nonce:         "nonce-1",
		Issuer:        "https://moodle.example",
		TargetLinkURI: "https://tool.example/lti/1p3/launch",
		ExpiresAt:     now.Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	idToken := signedIDToken(t, privateKey, "kid-1", map[string]any{
		"iss":                "https://moodle.example",
		"sub":                "student-ext",
		"aud":                "client-1",
		"exp":                now.Add(time.Minute).Unix(),
		"iat":                now.Unix(),
		"nonce":              "nonce-1",
		"jti":                "jwt-1",
		"name":               "Student One",
		ltiClaimDeploymentID: "deployment-1",
		ltiClaimMessageType:  ltiMessageTypeResourceLinkRequest,
		ltiClaimVersion:      "1.3.0",
		ltiClaimContext: map[string]any{
			"id":    "course-ext",
			"title": "Course",
		},
		ltiClaimResourceLink: map[string]any{
			"id":    "assignment-ext",
			"title": "Lab 3",
		},
	})
	form := url.Values{"id_token": {idToken}, "state": {state}}
	req := httptest.NewRequest(http.MethodPost, "/lti/1p3/launch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "launch_status=ACCEPTED") {
		t.Fatalf("location = %q", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	principal, err := sessionAuth.AuthenticateToken(cookies[0].Value)
	if err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}
	if principal.Subject != "moodle:student-ext" || principal.Role != authn.RoleStudent || principal.Source != "lti" || principal.DisplayName != "Student One" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestLTISessionCookieUsesConfiguredBrowserPolicy(t *testing.T) {
	server, _ := testServerWithAuthConfig(t, &testLMSRepository{}, nil, authn.Config{
		SessionSecret:  "0123456789abcdef",
		SessionTTL:     time.Hour,
		CookieSecure:   true,
		CookieSameSite: "none",
		CookieDomain:   ".example.com",
	})

	cookie := server.sessionCookie("token", time.Now().Add(time.Hour))

	if cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("same_site = %v", cookie.SameSite)
	}
	if !cookie.Secure {
		t.Fatal("cookie must be secure")
	}
	if cookie.Domain != ".example.com" {
		t.Fatalf("domain = %q", cookie.Domain)
	}
}

func TestLTIToolConfigurationUsesPublicBaseURL(t *testing.T) {
	lti, err := NewLTIService(LTIConfig{
		PlatformIssuer:   "https://moodle.example",
		ClientID:         "client-1",
		AuthLoginURL:     "https://moodle.example/mod/lti/auth.php",
		JWKSURL:          "https://moodle.example/mod/lti/certs.php",
		PublicBaseURL:    "https://hub.example/lms",
		DeploymentIDs:    []string{"deployment-1"},
		StateSecret:      "0123456789abcdef",
		AllowedClockSkew: time.Minute,
	}, nil)
	if err != nil {
		t.Fatalf("NewLTIService: %v", err)
	}
	server, _ := testServerWithLTI(t, &testLMSRepository{}, lti)
	req := httptest.NewRequest(http.MethodGet, "http://internal/lti/1p3/tool-configuration", nil)
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		OIDCLoginURL  string   `json:"oidc_login_url"`
		TargetLinkURI string   `json:"target_link_uri"`
		RedirectURIs  []string `json:"redirect_uris"`
		LaunchURL     string   `json:"launch_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantLaunch := "https://hub.example/lms/lti/1p3/launch"
	if payload.TargetLinkURI != wantLaunch || payload.LaunchURL != wantLaunch {
		t.Fatalf("launch urls = %#v", payload)
	}
	if len(payload.RedirectURIs) != 1 || payload.RedirectURIs[0] != wantLaunch {
		t.Fatalf("redirect_uris = %#v", payload.RedirectURIs)
	}
	if payload.OIDCLoginURL != "https://hub.example/lms/lti/1p3/login" {
		t.Fatalf("oidc_login_url = %q", payload.OIDCLoginURL)
	}
}

func TestLTIDiagnosticsReportsPublicURLAndCookieWarnings(t *testing.T) {
	lti := testLTIService(t, "https://moodle.example/mod/lti/certs.php")
	server, _ := testServerWithLTI(t, &testLMSRepository{}, lti)
	req := httptest.NewRequest(http.MethodGet, "http://internal/lti/1p3/diagnostics", nil)
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Status string `json:"status"`
		URLs   struct {
			LaunchURL string `json:"launch_url"`
		} `json:"urls"`
		Platform struct {
			ClientID          string `json:"client_id"`
			DeploymentIDCount int    `json:"deployment_id_count"`
		} `json:"platform"`
		BrowserSession struct {
			CookieSameSite string `json:"cookie_same_site"`
			CookieSecure   bool   `json:"cookie_secure"`
			IFrameReady    bool   `json:"iframe_ready"`
		} `json:"browser_session"`
		Warnings []string `json:"warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "configured" || payload.Platform.ClientID != "client-1" || payload.Platform.DeploymentIDCount != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.URLs.LaunchURL != "http://internal/lti/1p3/launch" {
		t.Fatalf("launch_url = %q", payload.URLs.LaunchURL)
	}
	if payload.BrowserSession.CookieSameSite != "lax" || payload.BrowserSession.CookieSecure || payload.BrowserSession.IFrameReady {
		t.Fatalf("browser_session = %#v", payload.BrowserSession)
	}
	if !containsWarning(payload.Warnings, "public HTTPS") || !containsWarning(payload.Warnings, "AUTH_COOKIE_SAME_SITE=none") {
		t.Fatalf("warnings = %#v", payload.Warnings)
	}
}

func TestLTIClaimsKeepMoodleCourseAndUseCustomLocalLab(t *testing.T) {
	req, err := ltiClaims{
		Issuer:  "https://moodle.example",
		Subject: "student-ext",
		Name:    "Student One",
		Context: ltiContext{
			ID:    "moodle-course-1",
			Title: "Moodle Course",
		},
		ResourceLink: ltiResourceLink{
			ID:    "assignment-ext",
			Title: "Lab 1",
		},
		Custom: map[string]any{
			"course_id": "course-3",
			"lab_id":    "lab-1-debian",
		},
	}.launchRequest()
	if err != nil {
		t.Fatalf("launchRequest: %v", err)
	}
	if req.MoodleCourseID != "moodle-course-1" {
		t.Fatalf("moodle_course_id = %s", req.MoodleCourseID)
	}
	if req.CourseID != "course-3" {
		t.Fatalf("course_id = %s", req.CourseID)
	}
	if req.LabID != "lab-1-debian" {
		t.Fatalf("lab_id = %s", req.LabID)
	}
}

func containsWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func testServer(t *testing.T, repo *testLMSRepository) (*Server, *authn.Service) {
	return testServerWithLTI(t, repo, nil)
}

func testServerWithLTI(t *testing.T, repo *testLMSRepository, lti *LTIService) (*Server, *authn.Service) {
	return testServerWithAuthConfig(t, repo, lti, authn.Config{
		SessionSecret: "0123456789abcdef",
		SessionTTL:    time.Hour,
	})
}

func testServerWithAuthConfig(t *testing.T, repo *testLMSRepository, lti *LTIService, authConfig authn.Config) (*Server, *authn.Service) {
	t.Helper()

	mapper, err := lmsusecase.NewMapper(`{"course-ext":"course-3"}`, `{"assignment-ext":"lab-3"}`)
	if err != nil {
		t.Fatalf("NewMapper: %v", err)
	}
	catalog := &testLabCatalog{definition: labcatalog.Definition{
		CourseID:  "course-3",
		LabID:     "lab-3",
		Enabled:   true,
		Resources: commands.LabResourceProfile{VCPU: 1, RAMMiB: 2048, DiskGiB: 20},
		Instances: []commands.VMBlueprint{{Name: "vm-1", ImageID: "image-1", FlavorID: "flavor-1", DiskGiB: 20}},
	}}
	service, err := lmsusecase.NewService("lms-gateway-service", "moodle", mapper, repo, catalog)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	authenticator, err := lmsusecase.NewAuthenticator("shared-secret", time.Minute)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	sessionAuth, err := authn.NewService(authConfig)
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	return NewServer(service, authenticator, lti, sessionAuth, "/", readinessChecker{}, nil), sessionAuth
}

func testLTIService(t *testing.T, jwksURL string) *LTIService {
	t.Helper()
	lti, err := NewLTIService(LTIConfig{
		PlatformIssuer:   "https://moodle.example",
		ClientID:         "client-1",
		AuthLoginURL:     "https://moodle.example/mod/lti/auth.php",
		JWKSURL:          jwksURL,
		DeploymentIDs:    []string{"deployment-1"},
		StateSecret:      "0123456789abcdef",
		AllowedClockSkew: time.Minute,
	}, nil)
	if err != nil {
		t.Fatalf("NewLTIService: %v", err)
	}
	return lti
}

func signedIDToken(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}
	signingInput := encodeJWTPart(t, header) + "." + encodeJWTPart(t, claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func encodeJWTPart(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func jwkFromPublicKey(kid string, key *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

type testLMSRepository struct {
	command  contracts.Envelope
	saved    lmsusecase.LaunchRecord
	inserted bool
}

func (r *testLMSRepository) SaveLaunch(_ context.Context, launch lmsusecase.LaunchRecord, command contracts.Envelope) (lmsusecase.LaunchRecord, bool, error) {
	r.command = command
	if r.saved.ID != "" {
		return r.saved, r.inserted, nil
	}
	launch.CreatedAt = command.OccurredAt
	return launch, true, nil
}

func (r *testLMSRepository) LoadResult(context.Context, string) (lmsusecase.LaunchResult, bool, error) {
	return lmsusecase.LaunchResult{}, false, nil
}

type testLabCatalog struct {
	definition labcatalog.Definition
}

func (c *testLabCatalog) Get(_ context.Context, courseID string, labID string) (labcatalog.Definition, bool, error) {
	if c.definition.CourseID == courseID && c.definition.LabID == labID {
		return c.definition, true, nil
	}
	return labcatalog.Definition{}, false, nil
}
