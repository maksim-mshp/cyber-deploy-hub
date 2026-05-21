package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	"cyber-deploy-hub/internal/usecase/labs"
	"cyber-deploy-hub/internal/usecase/readmodel"
	"cyber-deploy-hub/internal/usecase/settings"
)

type LabUsecase interface {
	RequestProvision(ctx context.Context, req labs.RequestProvision) (labs.ProvisionAccepted, error)
	RequestFreeze(ctx context.Context, req labs.LabCommand) (labs.CommandAccepted, error)
	RequestCleanup(ctx context.Context, req labs.LabCommand) (labs.CommandAccepted, error)
	RequestCheck(ctx context.Context, req labs.CheckCommand) (labs.CommandAccepted, error)
}

type SettingsUsecase interface {
	Update(ctx context.Context, req settings.UpdateRequest) (settings.UpdateAccepted, error)
}

type LabCatalogUsecase interface {
	List(ctx context.Context, includeDisabled bool) (labcatalog.ListResult, error)
	Get(ctx context.Context, courseID string, labID string) (labcatalog.Definition, bool, error)
	Update(ctx context.Context, req labcatalog.UpdateRequest) (labcatalog.UpdateResult, error)
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

type OpenStackChecker interface {
	Configured() bool
	Check(ctx context.Context) error
	ListImages(ctx context.Context) ([]openstack.ImageOption, error)
	ListFlavors(ctx context.Context) ([]openstack.FlavorOption, error)
}

type ReadModel interface {
	ListLabRuns(ctx context.Context, limit int) (readmodel.LabRunsView, error)
	GetLabRun(ctx context.Context, labRunID string) (readmodel.LabRunView, bool, error)
	GetVDIAccess(ctx context.Context, labRunID string) (readmodel.VDIAccessView, bool, error)
	ListLabInstances(ctx context.Context, labRunID string) (readmodel.LabInstancesView, bool, error)
	ListLabRunEvents(ctx context.Context, labRunID string, afterID int64, limit int) ([]readmodel.LabRunEvent, error)
	ListAuditEvents(ctx context.Context, limit int) (readmodel.AuditView, error)
	GetSettings(ctx context.Context) (readmodel.SettingsView, error)
	GetProjectPool(ctx context.Context) (readmodel.ProjectPoolView, error)
	ListCheckRuns(ctx context.Context, labRunID string, limit int) (readmodel.CheckRunsView, error)
}

type Server struct {
	labs      LabUsecase
	settings  SettingsUsecase
	catalog   LabCatalogUsecase
	read      ReadModel
	ready     ReadinessChecker
	openstack OpenStackChecker
	logger    *slog.Logger
}

func NewServer(labUsecase LabUsecase, settingsUsecase SettingsUsecase, catalog LabCatalogUsecase, read ReadModel, ready ReadinessChecker, openstack OpenStackChecker, logger *slog.Logger) *Server {
	return &Server{
		labs:      labUsecase,
		settings:  settingsUsecase,
		catalog:   catalog,
		read:      read,
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
	mux.HandleFunc("GET /api/teacher/openstack/images", s.handleListOpenStackImages)
	mux.HandleFunc("GET /api/teacher/openstack/flavors", s.handleListOpenStackFlavors)
	mux.HandleFunc("GET /api/lab-definitions", s.handleListAvailableLabDefinitions)
	mux.HandleFunc("GET /api/labs", s.handleListLabs)
	mux.HandleFunc("POST /api/labs", s.handleRequestLab)
	mux.HandleFunc("GET /api/labs/{labRunID}", s.handleGetLab)
	mux.HandleFunc("GET /api/labs/{labRunID}/vdi", s.handleGetLabVDI)
	mux.HandleFunc("GET /api/labs/{labRunID}/instances", s.handleLabInstances)
	mux.HandleFunc("GET /api/labs/{labRunID}/events", s.handleLabEvents)
	mux.HandleFunc("GET /api/labs/{labRunID}/checks", s.handleLabChecks)
	mux.HandleFunc("POST /api/labs/{labRunID}/freeze", s.handleFreezeLab)
	mux.HandleFunc("POST /api/labs/{labRunID}/check", s.handleCheckLab)
	mux.HandleFunc("POST /api/labs/{labRunID}/cleanup", s.handleCleanupLab)
	mux.HandleFunc("GET /api/admin/audit", s.handleAdminAudit)
	mux.HandleFunc("GET /api/admin/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/admin/settings", s.handleUpdateSettings)
	mux.HandleFunc("GET /api/admin/project-pool", s.handleProjectPool)
	mux.HandleFunc("GET /api/teacher/lab-definitions", s.handleListTeacherLabDefinitions)
	mux.HandleFunc("POST /api/teacher/lab-definitions", s.handleUpdateTeacherLabDefinition)
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

func (s *Server) handleListOpenStackImages(w http.ResponseWriter, r *http.Request) {
	if s.openstack == nil || !s.openstack.Configured() {
		writeError(w, http.StatusServiceUnavailable, "openstack_not_configured", "OpenStack credentials are not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, err := s.openstack.ListImages(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "openstack_images_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": items})
}

func (s *Server) handleListOpenStackFlavors(w http.ResponseWriter, r *http.Request) {
	if s.openstack == nil || !s.openstack.Configured() {
		writeError(w, http.StatusServiceUnavailable, "openstack_not_configured", "OpenStack credentials are not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, err := s.openstack.ListFlavors(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "openstack_flavors_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flavors": items})
}

func (s *Server) handleListAvailableLabDefinitions(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "lab_catalog_unavailable", "Lab catalog is not configured")
		return
	}
	view, err := s.catalog.List(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lab_catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleRequestLab(w http.ResponseWriter, r *http.Request) {
	var req requestLabRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if s.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "lab_catalog_unavailable", "Lab catalog is not configured")
		return
	}
	definition, found, err := s.catalog.Get(r.Context(), req.CourseID, req.LabID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lab_catalog_failed", err.Error())
		return
	}
	if !found || !definition.Enabled {
		writeError(w, http.StatusNotFound, "lab_not_available", "Lab definition is not available")
		return
	}

	result, err := s.labs.RequestProvision(r.Context(), labs.RequestProvision{
		StudentID:      req.StudentID,
		CourseID:       definition.CourseID,
		LabID:          definition.LabID,
		Source:         req.Source,
		Resources:      definition.Resources,
		Instances:      definition.Instances,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "request_rejected", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleListLabs(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	limit := int(parseInt64(r.URL.Query().Get("limit"), 50))
	view, err := s.read.ListLabRuns(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetLab(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, found, err := s.read.GetLabRun(r.Context(), r.PathValue("labRunID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "lab_not_found", "Lab run was not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetLabVDI(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, found, err := s.read.GetVDIAccess(r.Context(), r.PathValue("labRunID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "lab_not_found", "Lab run was not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleLabInstances(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, found, err := s.read.ListLabInstances(r.Context(), r.PathValue("labRunID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "lab_not_found", "Lab run was not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleFreezeLab(w http.ResponseWriter, r *http.Request) {
	var req labActionRequest
	if err := readJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.labs.RequestFreeze(r.Context(), labs.LabCommand{
		LabRunID:       r.PathValue("labRunID"),
		Reason:         req.Reason,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "freeze_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleCheckLab(w http.ResponseWriter, r *http.Request) {
	var req checkLabRequest
	if err := readJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.labs.RequestCheck(r.Context(), labs.CheckCommand{
		LabRunID:       r.PathValue("labRunID"),
		ProfileID:      req.ProfileID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "check_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleCleanupLab(w http.ResponseWriter, r *http.Request) {
	var req labActionRequest
	if err := readJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.labs.RequestCleanup(r.Context(), labs.LabCommand{
		LabRunID:       r.PathValue("labRunID"),
		Reason:         req.Reason,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "cleanup_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleLabEvents(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable", "HTTP streaming is not supported")
		return
	}
	afterID := parseInt64(r.URL.Query().Get("after_id"), 0)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		events, err := s.read.ListLabRunEvents(r.Context(), r.PathValue("labRunID"), afterID, 100)
		if err != nil {
			writeSSE(w, "error", map[string]string{"error": err.Error()}, 0)
			flusher.Flush()
			return
		}
		for _, event := range events {
			writeSSE(w, "lab_run", event, event.ID)
			afterID = event.ID
		}
		if len(events) == 0 {
			_, _ = fmt.Fprint(w, ": ping\n\n")
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) handleLabChecks(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, err := s.read.ListCheckRuns(r.Context(), r.PathValue("labRunID"), int(parseInt64(r.URL.Query().Get("limit"), 10)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, err := s.read.ListAuditEvents(r.Context(), int(parseInt64(r.URL.Query().Get("limit"), 100)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, err := s.read.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
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

func (s *Server) handleProjectPool(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	view, err := s.read.GetProjectPool(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleListTeacherLabDefinitions(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "lab_catalog_unavailable", "Lab catalog is not configured")
		return
	}
	view, err := s.catalog.List(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lab_catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleUpdateTeacherLabDefinition(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "lab_catalog_unavailable", "Lab catalog is not configured")
		return
	}
	var req updateLabDefinitionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.catalog.Update(r.Context(), labcatalog.UpdateRequest{
		ChangedBy: req.ChangedBy,
		Definition: labcatalog.Definition{
			CourseID:    req.CourseID,
			LabID:       req.LabID,
			Title:       req.Title,
			Description: req.Description,
			Enabled:     req.Enabled,
			Resources:   req.Resources,
			Instances:   req.Instances,
		},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "lab_definition_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type requestLabRequest struct {
	StudentID      string `json:"student_id"`
	CourseID       string `json:"course_id"`
	LabID          string `json:"lab_id"`
	Source         string `json:"source"`
	IdempotencyKey string `json:"idempotency_key"`
}

type updateLabDefinitionRequest struct {
	CourseID    string                      `json:"course_id"`
	LabID       string                      `json:"lab_id"`
	Title       string                      `json:"title"`
	Description string                      `json:"description"`
	Enabled     bool                        `json:"enabled"`
	Resources   commands.LabResourceProfile `json:"resources"`
	Instances   []commands.VMBlueprint      `json:"instances"`
	ChangedBy   string                      `json:"changed_by"`
}

type labActionRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type checkLabRequest struct {
	ProfileID      string `json:"profile_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type updateSettingsRequest struct {
	ChangedBy      string         `json:"changed_by"`
	Values         map[string]any `json:"values"`
	IdempotencyKey string         `json:"idempotency_key"`
}

var errEmptyBody = errors.New("empty body")

func readJSON(r *http.Request, dst any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errEmptyBody
		}
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

func writeSSE(w http.ResponseWriter, eventName string, body any, id int64) {
	if id > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", id)
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", eventName)
	raw, err := json.Marshal(body)
	if err != nil {
		raw = []byte(`{"error":"failed to encode event"}`)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}

func parseInt64(value string, fallback int64) int64 {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
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
