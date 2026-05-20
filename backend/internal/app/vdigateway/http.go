package vdigateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	vdigatewayusecase "cyber-deploy-hub/internal/usecase/vdigateway"
)

type sessionService interface {
	OpenSession(ctx context.Context, req vdigatewayusecase.OpenSessionRequest) (vdigatewayusecase.SessionLaunch, error)
}

type readiness interface {
	Ready(ctx context.Context) error
}

type httpServer struct {
	service sessionService
	ready   readiness
	logger  *slog.Logger
}

func newHTTPServer(service sessionService, ready readiness, logger *slog.Logger) *httpServer {
	return &httpServer{service: service, ready: ready, logger: logger}
}

func (s *httpServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /vdi/session/{token}", s.handleOpenSession)
	return s.withRequestLog(mux)
}

func (s *httpServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *httpServer) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.ready.Ready(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *httpServer) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	launch, err := s.service.OpenSession(r.Context(), vdigatewayusecase.OpenSessionRequest{
		Token:      r.PathValue("token"),
		RemoteAddr: remoteAddr(r),
		UserAgent:  r.UserAgent(),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, launch)
		return
	}
	http.Redirect(w, r, launch.LaunchURL, http.StatusFound)
}

func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, vdigatewayusecase.ErrInvalidToken):
		writeError(w, http.StatusBadRequest, "invalid_vdi_token", "VDI access token is invalid")
	case errors.Is(err, vdigatewayusecase.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "vdi_token_not_found", "VDI access token was not found")
	case errors.Is(err, vdigatewayusecase.ErrTokenExpired):
		writeError(w, http.StatusGone, "vdi_token_expired", "VDI access token has expired")
	case errors.Is(err, vdigatewayusecase.ErrTokenRevoked):
		writeError(w, http.StatusGone, "vdi_token_revoked", "VDI access token has been revoked")
	default:
		writeError(w, http.StatusInternalServerError, "vdi_session_failed", err.Error())
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

func (s *httpServer) withRequestLog(next http.Handler) http.Handler {
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
