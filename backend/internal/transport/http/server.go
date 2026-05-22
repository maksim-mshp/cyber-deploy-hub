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
	"cyber-deploy-hub/internal/usecase/authn"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	"cyber-deploy-hub/internal/usecase/labs"
	"cyber-deploy-hub/internal/usecase/projectpool"
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

type ProjectPoolUsecase interface {
	RequestImport(ctx context.Context, req projectpool.ImportRequest) (projectpool.ImportAccepted, error)
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
	ListLabRunsByStudent(ctx context.Context, studentID string, limit int) (readmodel.LabRunsView, error)
	HasActiveLabRun(ctx context.Context, studentID string) (bool, error)
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
	labs        LabUsecase
	settings    SettingsUsecase
	projectPool ProjectPoolUsecase
	catalog     LabCatalogUsecase
	read        ReadModel
	auth        *authn.Service
	ready       ReadinessChecker
	openstack   OpenStackChecker
	logger      *slog.Logger
}

func NewServer(labUsecase LabUsecase, settingsUsecase SettingsUsecase, catalog LabCatalogUsecase, read ReadModel, authService *authn.Service, ready ReadinessChecker, openstack OpenStackChecker, logger *slog.Logger, projectPoolUsecases ...ProjectPoolUsecase) *Server {
	server := &Server{
		labs:      labUsecase,
		settings:  settingsUsecase,
		catalog:   catalog,
		read:      read,
		auth:      authService,
		ready:     ready,
		openstack: openstack,
		logger:    logger,
	}
	if len(projectPoolUsecases) > 0 {
		server.projectPool = projectPoolUsecases[0]
	}
	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
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
	mux.HandleFunc("POST /api/admin/project-pool/import", s.handleImportProjectPool)
	mux.HandleFunc("GET /api/teacher/lab-definitions", s.handleListTeacherLabDefinitions)
	mux.HandleFunc("POST /api/teacher/lab-definitions", s.handleUpdateTeacherLabDefinition)
	return s.withRequestLog(s.withAuth(mux))
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

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": principal})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is not configured")
		return
	}
	var req authn.LoginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.auth.Login(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
		return
	}
	http.SetCookie(w, s.sessionCookie(result.Token, result.ExpiresAt))
	writeJSON(w, http.StatusOK, map[string]any{"user": result.User, "expires_at": result.ExpiresAt})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, _ *http.Request) {
	if s.auth != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     s.auth.CookieName(),
			Value:    "",
			Domain:   s.auth.CookieDomain(),
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: s.auth.CookieSameSite(),
			Secure:   s.auth.CookieSecure(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	principal, ok := principalFromContext(r.Context())
	if !ok {
		if s.auth != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required")
			return
		}
		principal = authn.Principal{Subject: strings.TrimSpace(req.StudentID), Role: authn.RoleStudent}
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
	if !found || (principal.Role == authn.RoleStudent && !definition.Enabled) {
		writeError(w, http.StatusNotFound, "lab_not_available", "Lab definition is not available")
		return
	}

	studentID := principal.Subject
	source := "student-ui"
	if principal.Role == authn.RoleTeacher {
		studentID = strings.TrimSpace(req.StudentID)
		if studentID == "" {
			studentID = "teacher:" + principal.Subject
		}
		source = "teacher-ui"
	} else if s.auth != nil {
		if s.read == nil {
			writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
			return
		}
		active, err := s.read.HasActiveLabRun(r.Context(), principal.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
			return
		}
		if active {
			writeError(w, http.StatusConflict, "active_lab_exists", "Finish the active lab before starting another one")
			return
		}
	}
	if principal.Role == authn.RoleTeacher && strings.TrimSpace(req.Source) != "" {
		source = strings.TrimSpace(req.Source)
	}

	result, err := s.labs.RequestProvision(r.Context(), labs.RequestProvision{
		StudentID:      studentID,
		CourseID:       definition.CourseID,
		LabID:          definition.LabID,
		Source:         source,
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
	var (
		view readmodel.LabRunsView
		err  error
	)
	if principal, ok := principalFromContext(r.Context()); ok && principal.Role == authn.RoleStudent {
		view, err = s.read.ListLabRunsByStudent(r.Context(), principal.Subject, limit)
	} else {
		view, err = s.read.ListLabRuns(r.Context(), limit)
	}
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
	if !authorizeLabView(w, r, view) {
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetLabVDI(w http.ResponseWriter, r *http.Request) {
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	if !s.authorizeLabRunID(w, r, r.PathValue("labRunID")) {
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
	if !s.authorizeLabRunID(w, r, r.PathValue("labRunID")) {
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
	if !s.authorizeTeacherOrOwner(w, r, r.PathValue("labRunID")) {
		return
	}
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
	if !s.authorizeTeacherOrOwner(w, r, r.PathValue("labRunID")) {
		return
	}
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return
	}
	if s.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "lab_catalog_unavailable", "Lab catalog is not configured")
		return
	}
	var req checkLabRequest
	if err := readJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	labRun, found, err := s.read.GetLabRun(r.Context(), r.PathValue("labRunID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "lab_not_found", "Lab run was not found")
		return
	}
	definition, found, err := s.catalog.Get(r.Context(), labRun.CourseID, labRun.LabID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lab_catalog_failed", err.Error())
		return
	}
	if !found || definition.CheckProfile == nil || len(definition.CheckProfile.Steps) == 0 {
		writeError(w, http.StatusConflict, "check_not_configured", "SSH check is not configured for this lab")
		return
	}
	result, err := s.labs.RequestCheck(r.Context(), labs.CheckCommand{
		LabRunID:       r.PathValue("labRunID"),
		ProfileID:      definition.CheckProfile.ID,
		Profile:        definition.CheckProfile,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "check_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) handleCleanupLab(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTeacherOrOwner(w, r, r.PathValue("labRunID")) {
		return
	}
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
	if !s.authorizeLabRunID(w, r, r.PathValue("labRunID")) {
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
	if !s.authorizeLabRunID(w, r, r.PathValue("labRunID")) {
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
		ChangedBy:      changedBy(r, req.ChangedBy),
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

func (s *Server) handleImportProjectPool(w http.ResponseWriter, r *http.Request) {
	if s.projectPool == nil {
		writeError(w, http.StatusServiceUnavailable, "project_pool_unavailable", "Project pool import is not configured")
		return
	}
	var req importProjectPoolRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.projectPool.RequestImport(r.Context(), projectpool.ImportRequest{
		Seed: projectpool.Seed{
			Domains:  req.Domains,
			Projects: req.Projects,
		},
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "project_pool_import_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, result)
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
		ChangedBy: changedBy(r, req.ChangedBy),
		Definition: labcatalog.Definition{
			CourseID:     req.CourseID,
			LabID:        req.LabID,
			Title:        req.Title,
			Description:  req.Description,
			Enabled:      req.Enabled,
			Resources:    req.Resources,
			Instances:    req.Instances,
			CheckProfile: req.CheckProfile,
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
	CourseID     string                      `json:"course_id"`
	LabID        string                      `json:"lab_id"`
	Title        string                      `json:"title"`
	Description  string                      `json:"description"`
	Enabled      bool                        `json:"enabled"`
	Resources    commands.LabResourceProfile `json:"resources"`
	Instances    []commands.VMBlueprint      `json:"instances"`
	CheckProfile *commands.CheckerProfileV1  `json:"check_profile"`
	ChangedBy    string                      `json:"changed_by"`
}

type labActionRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type checkLabRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

type updateSettingsRequest struct {
	ChangedBy      string         `json:"changed_by"`
	Values         map[string]any `json:"values"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type importProjectPoolRequest struct {
	Domains        []projectpool.SeedDomain  `json:"domains"`
	Projects       []projectpool.SeedProject `json:"projects"`
	IdempotencyKey string                    `json:"idempotency_key"`
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

type principalContextKey struct{}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil || !strings.HasPrefix(r.URL.Path, "/api/") || isPublicAuthPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		principal, err := s.authenticateRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		if requiresTeacherRole(r.URL.Path) && principal.Role != authn.RoleTeacher {
			writeError(w, http.StatusForbidden, "forbidden", "Teacher role is required")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authenticateRequest(r *http.Request) (authn.Principal, error) {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return s.auth.AuthenticateToken(token)
	}
	cookie, err := r.Cookie(s.auth.CookieName())
	if err != nil {
		return authn.Principal{}, errors.New("authentication is required")
	}
	return s.auth.AuthenticateToken(cookie.Value)
}

func (s *Server) sessionCookie(token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	return &http.Cookie{
		Name:     s.auth.CookieName(),
		Value:    token,
		Domain:   s.auth.CookieDomain(),
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: s.auth.CookieSameSite(),
		Secure:   s.auth.CookieSecure(),
	}
}

func isPublicAuthPath(r *http.Request) bool {
	if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
		return true
	}
	if r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost {
		return true
	}
	return false
}

func requiresTeacherRole(path string) bool {
	return strings.HasPrefix(path, "/api/teacher/") || strings.HasPrefix(path, "/api/admin/")
}

func principalFromContext(ctx context.Context) (authn.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(authn.Principal)
	return principal, ok
}

func (s *Server) requireTeacher(w http.ResponseWriter, r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required")
		return false
	}
	if principal.Role != authn.RoleTeacher {
		writeError(w, http.StatusForbidden, "forbidden", "Teacher role is required")
		return false
	}
	return true
}

func (s *Server) authorizeTeacherOrOwner(w http.ResponseWriter, r *http.Request, labRunID string) bool {
	if s.auth == nil {
		return true
	}
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required")
		return false
	}
	if principal.Role == authn.RoleTeacher {
		return true
	}
	return s.authorizeLabRunID(w, r, labRunID)
}

func (s *Server) authorizeLabRunID(w http.ResponseWriter, r *http.Request, labRunID string) bool {
	if s.auth == nil {
		return true
	}
	if s.read == nil {
		writeError(w, http.StatusServiceUnavailable, "read_model_unavailable", "Read model is not configured")
		return false
	}
	view, found, err := s.read.GetLabRun(r.Context(), labRunID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_model_failed", err.Error())
		return false
	}
	if !found {
		writeError(w, http.StatusNotFound, "lab_not_found", "Lab run was not found")
		return false
	}
	return authorizeLabView(w, r, view)
}

func authorizeLabView(w http.ResponseWriter, r *http.Request, view readmodel.LabRunView) bool {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		return true
	}
	if principal.Role == authn.RoleTeacher || view.StudentID == principal.Subject {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden", "Lab run belongs to another user")
	return false
}

func changedBy(r *http.Request, fallback string) string {
	if principal, ok := principalFromContext(r.Context()); ok {
		return principal.Subject
	}
	return fallback
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
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
