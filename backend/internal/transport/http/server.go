package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cyber-deploy-hub/internal/usecase/labs"
	"cyber-deploy-hub/internal/usecase/settings"
)

type LabUsecase interface {
	RequestProvision(ctx context.Context, req labs.RequestProvision) (labs.ProvisionAccepted, error)
}

type SettingsUsecase interface {
	Update(ctx context.Context, req settings.UpdateRequest) (settings.UpdateAccepted, error)
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

type OpenStackChecker interface {
	Configured() bool
	Check(ctx context.Context) error
}

type Server struct {
	labs      LabUsecase
	settings  SettingsUsecase
	ready     ReadinessChecker
	openstack OpenStackChecker
	logger    *slog.Logger
}

func NewServer(labUsecase LabUsecase, settingsUsecase SettingsUsecase, ready ReadinessChecker, openstack OpenStackChecker, logger *slog.Logger) *Server {
	return &Server{
		labs:      labUsecase,
		settings:  settingsUsecase,
		ready:     ready,
		openstack: openstack,
		logger:    logger,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/admin/openstack/ping", s.handleOpenStackPing)
	mux.HandleFunc("POST /api/labs", s.handleRequestLab)
	mux.HandleFunc("POST /api/admin/settings", s.handleUpdateSettings)
	return s.withRequestLog(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleOpenStackPing(w http.ResponseWriter, r *http.Request) {
	if s.openstack == nil || !s.openstack.Configured() {
		writeError(w, http.StatusServiceUnavailable, "openstack_not_configured", "OpenStack credentials are not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if err := s.openstack.Check(ctx); err != nil {
		writeError(w, http.StatusBadGateway, "openstack_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRequestLab(w http.ResponseWriter, r *http.Request) {
	var req requestLabRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	result, err := s.labs.RequestProvision(r.Context(), labs.RequestProvision{
		StudentID:      req.StudentID,
		CourseID:       req.CourseID,
		LabID:          req.LabID,
		Source:         req.Source,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "request_rejected", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	result, err := s.settings.Update(r.Context(), settings.UpdateRequest{
		ChangedBy:      req.ChangedBy,
		Values:         req.Values,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "settings_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

type requestLabRequest struct {
	StudentID      string `json:"student_id"`
	CourseID       string `json:"course_id"`
	LabID          string `json:"lab_id"`
	Source         string `json:"source"`
	IdempotencyKey string `json:"idempotency_key"`
}

type updateSettingsRequest struct {
	ChangedBy      string         `json:"changed_by"`
	Values         map[string]any `json:"values"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func readJSON(r *http.Request, dst any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
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
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	return r.RemoteAddr
}
