package lmsgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func testServer(t *testing.T, repo *testLMSRepository) (*Server, *authn.Service) {
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
	sessionAuth, err := authn.NewService(authn.Config{
		SessionSecret: "0123456789abcdef",
		SessionTTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	return NewServer(service, authenticator, sessionAuth, "/", readinessChecker{}, nil), sessionAuth
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
