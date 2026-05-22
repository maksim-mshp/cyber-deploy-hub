package lmsgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"cyber-deploy-hub/internal/usecase/authn"
	lmsusecase "cyber-deploy-hub/internal/usecase/lmsgateway"
)

type Server struct {
	service     *lmsusecase.Service
	auth        *lmsusecase.Authenticator
	lti         *LTIService
	sessionAuth *authn.Service
	frontendURL string
	ready       readinessChecker
	logger      *slog.Logger
}

func NewServer(service *lmsusecase.Service, auth *lmsusecase.Authenticator, lti *LTIService, sessionAuth *authn.Service, frontendURL string, ready readinessChecker, logger *slog.Logger) *Server {
	if frontendURL == "" {
		frontendURL = "/"
	}
	return &Server{service: service, auth: auth, lti: lti, sessionAuth: sessionAuth, frontendURL: frontendURL, ready: ready, logger: logger}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /lms/moodle/mapping", s.handleMapping)
	mux.HandleFunc("POST /lms/moodle/launch", s.handleMoodleLaunch)
	mux.HandleFunc("POST /lms/moodle/sso", s.handleMoodleSSO)
	mux.HandleFunc("GET /lms/moodle/launch/{launchID}/result", s.handleMoodleResult)
	mux.HandleFunc("GET /lti/1p3/login", s.handleLTILogin)
	mux.HandleFunc("POST /lti/1p3/login", s.handleLTILogin)
	mux.HandleFunc("POST /lti/1p3/launch", s.handleLTILaunch)
	mux.HandleFunc("GET /lti/1p3/tool-configuration", s.handleLTIToolConfiguration)
	return s.withRequestLog(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ready.Ready(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMapping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.service.MappingDescription())
}

func (s *Server) handleMoodleLaunch(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()
	if err := s.auth.Authenticate(r, body); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req lmsusecase.LaunchRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, inserted, err := s.service.Launch(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "launch_rejected", err.Error())
		return
	}
	status := http.StatusAccepted
	if !inserted {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (s *Server) handleMoodleSSO(w http.ResponseWriter, r *http.Request) {
	if s.sessionAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()
	if err := s.auth.Authenticate(r, body); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req lmsusecase.LaunchRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, _, err := s.service.Launch(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "launch_rejected", err.Error())
		return
	}
	displayName := req.UserLogin
	if displayName == "" {
		displayName = result.Mapping.StudentID
	}
	token, expiresAt, err := s.sessionAuth.IssueSession(authn.Principal{
		Subject:     result.Mapping.StudentID,
		Role:        authn.RoleStudent,
		DisplayName: displayName,
		Source:      "moodle",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	http.SetCookie(w, s.sessionCookie(token, expiresAt))
	http.Redirect(w, r, s.redirectURL(result), http.StatusSeeOther)
}

func (s *Server) handleMoodleResult(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Authenticate(r, nil); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	result, found, err := s.service.Result(r.Context(), r.PathValue("launchID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "result_rejected", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "launch_not_found", "launch was not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) redirectURL(result lmsusecase.LaunchAccepted) string {
	parsed, err := url.Parse(s.frontendURL)
	if err != nil {
		return "/"
	}
	query := parsed.Query()
	query.Set("lti_launch_id", result.LaunchID)
	query.Set("lab_run_id", result.LabRunID)
	query.Set("launch_status", result.Status)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *Server) sessionCookie(token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	return &http.Cookie{
		Name:     s.sessionAuth.CookieName(),
		Value:    token,
		Domain:   s.sessionAuth.CookieDomain(),
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: s.sessionAuth.CookieSameSite(),
		Secure:   s.sessionAuth.CookieSecure(),
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		if s.logger != nil {
			s.logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", remoteAddr(r)),
				slog.Duration("duration", time.Since(startedAt)),
			)
		}
	})
}

func remoteAddr(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return forwarded
	}
	return r.RemoteAddr
}

var errServerClosed = errors.New("server closed")

func normalizeListenError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return errServerClosed
	}
	return fmt.Errorf("lms gateway listen: %w", err)
}
