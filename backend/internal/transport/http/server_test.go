package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/authn"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	"cyber-deploy-hub/internal/usecase/labs"
	"cyber-deploy-hub/internal/usecase/readmodel"
)

func TestHandleRequestLabUsesCatalogDefinition(t *testing.T) {
	labUsecase := &fakeLabUsecase{}
	catalog := &fakeLabCatalog{
		definition: labcatalog.Definition{
			CourseID: "course-3",
			LabID:    "lab-3",
			Title:    "Lab 3",
			Enabled:  true,
			Resources: commands.LabResourceProfile{
				VCPU:    4,
				RAMMiB:  8192,
				DiskGiB: 80,
			},
			Instances: []commands.VMBlueprint{{
				Name:     "vm-1",
				ImageID:  "image-1",
				FlavorID: "flavor-1",
				DiskGiB:  40,
			}},
		},
	}
	server := NewServer(labUsecase, nil, catalog, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/labs", strings.NewReader(`{
		"student_id":"student-1",
		"course_id":"course-3",
		"lab_id":"lab-3",
		"source":"student-ui",
		"idempotency_key":"key-1"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if labUsecase.request.CourseID != "course-3" || labUsecase.request.LabID != "lab-3" {
		t.Fatalf("request identifiers = %#v", labUsecase.request)
	}
	if labUsecase.request.Resources.VCPU != 4 || labUsecase.request.Resources.RAMMiB != 8192 || labUsecase.request.Resources.DiskGiB != 80 {
		t.Fatalf("request resources = %#v", labUsecase.request.Resources)
	}
	if len(labUsecase.request.Instances) != 1 || labUsecase.request.Instances[0].ImageID != "image-1" {
		t.Fatalf("request instances = %#v", labUsecase.request.Instances)
	}
}

func TestHandleRequestLabRejectsStudentSuppliedConfiguration(t *testing.T) {
	labUsecase := &fakeLabUsecase{}
	catalog := &fakeLabCatalog{}
	server := NewServer(labUsecase, nil, catalog, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/labs", strings.NewReader(`{
		"student_id":"student-1",
		"course_id":"course-3",
		"lab_id":"lab-3",
		"resources":{"vcpu":99,"ram_mib":999999,"disk_gib":999}
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if labUsecase.called {
		t.Fatal("lab usecase must not be called when student sends deploy configuration")
	}
}

func TestHandleUpdateTeacherLabDefinitionSavesCatalogDefinition(t *testing.T) {
	catalog := &fakeLabCatalog{}
	server := NewServer(&fakeLabUsecase{}, nil, catalog, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/lab-definitions", strings.NewReader(`{
		"course_id":"course-3",
		"lab_id":"lab-3",
		"title":"Lab 3",
		"description":"storage lab",
		"enabled":true,
		"resources":{"vcpu":2,"ram_mib":4096,"disk_gib":40},
		"instances":[{"name":"vm-1","image_id":"image-1","flavor_id":"flavor-1","disk_gib":20}],
		"changed_by":"teacher-console"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if catalog.update.ChangedBy != "teacher-console" {
		t.Fatalf("changed_by = %q", catalog.update.ChangedBy)
	}
	if catalog.update.Definition.Resources.VCPU != 2 || len(catalog.update.Definition.Instances) != 1 {
		t.Fatalf("saved definition = %#v", catalog.update.Definition)
	}
}

func TestHandleListOpenStackCatalog(t *testing.T) {
	cloud := &fakeOpenStack{
		images: []openstack.ImageOption{{
			ID:         "image-1",
			Name:       "Debian 12",
			Status:     "active",
			Visibility: "public",
			DiskFormat: "qcow2",
			MinDiskGiB: 20,
			SizeGiB:    4,
		}},
		flavors: []openstack.FlavorOption{{
			ID:      "flavor-1",
			Name:    "small",
			VCPUs:   1,
			RAMMiB:  2048,
			DiskGiB: 0,
		}},
	}
	server := NewServer(&fakeLabUsecase{}, nil, &fakeLabCatalog{}, nil, nil, nil, cloud, nil)

	imageReq := httptest.NewRequest(http.MethodGet, "/api/teacher/openstack/images", nil)
	imageRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(imageRec, imageReq)
	if imageRec.Code != http.StatusOK {
		t.Fatalf("image status = %d, body = %s", imageRec.Code, imageRec.Body.String())
	}
	var imagePayload struct {
		Images []openstack.ImageOption `json:"images"`
	}
	if err := json.NewDecoder(imageRec.Body).Decode(&imagePayload); err != nil {
		t.Fatalf("decode images: %v", err)
	}
	if len(imagePayload.Images) != 1 || imagePayload.Images[0].Name != "Debian 12" {
		t.Fatalf("images = %#v", imagePayload.Images)
	}

	flavorReq := httptest.NewRequest(http.MethodGet, "/api/teacher/openstack/flavors", nil)
	flavorRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(flavorRec, flavorReq)
	if flavorRec.Code != http.StatusOK {
		t.Fatalf("flavor status = %d, body = %s", flavorRec.Code, flavorRec.Body.String())
	}
	var flavorPayload struct {
		Flavors []openstack.FlavorOption `json:"flavors"`
	}
	if err := json.NewDecoder(flavorRec.Body).Decode(&flavorPayload); err != nil {
		t.Fatalf("decode flavors: %v", err)
	}
	if len(flavorPayload.Flavors) != 1 || flavorPayload.Flavors[0].Name != "small" {
		t.Fatalf("flavors = %#v", flavorPayload.Flavors)
	}
}

func TestAuthLoginAndMe(t *testing.T) {
	authService := testAuthService(t)
	server := NewServer(&fakeLabUsecase{}, nil, &fakeLabCatalog{}, nil, authService, nil, nil, nil)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"student","password":"student-pass"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRec.Code, loginRec.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(loginRec.Result().Cookies()[0])
	meRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meRec.Code, meRec.Body.String())
	}
	if !strings.Contains(meRec.Body.String(), `"role":"student"`) {
		t.Fatalf("me body = %s", meRec.Body.String())
	}
}

func TestStudentCannotStartSecondActiveLab(t *testing.T) {
	authService := testAuthService(t)
	reader := &fakeReadModel{active: true}
	catalog := &fakeLabCatalog{definition: labcatalog.Definition{
		CourseID:  "course-3",
		LabID:     "lab-3",
		Title:     "Lab 3",
		Enabled:   true,
		Resources: commands.LabResourceProfile{VCPU: 1, RAMMiB: 1024, DiskGiB: 20},
		Instances: []commands.VMBlueprint{{Name: "vm", ImageID: "image", FlavorID: "flavor", DiskGiB: 20}},
	}}
	labUsecase := &fakeLabUsecase{}
	server := NewServer(labUsecase, nil, catalog, reader, authService, nil, nil, nil)
	token, _, err := authService.IssueSession(authn.Principal{Subject: "student", Role: authn.RoleStudent})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/labs", strings.NewReader(`{"course_id":"course-3","lab_id":"lab-3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authService.CookieName(), Value: token})
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if labUsecase.called {
		t.Fatal("lab usecase must not be called when student has active lab")
	}
}

func TestStudentCannotAccessTeacherCatalog(t *testing.T) {
	authService := testAuthService(t)
	server := NewServer(&fakeLabUsecase{}, nil, &fakeLabCatalog{}, &fakeReadModel{}, authService, nil, nil, nil)
	token, _, err := authService.IssueSession(authn.Principal{Subject: "student", Role: authn.RoleStudent})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/teacher/lab-definitions", nil)
	req.AddCookie(&http.Cookie{Name: authService.CookieName(), Value: token})
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestStudentRequestLabUsesAuthenticatedSubject(t *testing.T) {
	authService := testAuthService(t)
	reader := &fakeReadModel{}
	catalog := &fakeLabCatalog{definition: labcatalog.Definition{
		CourseID:  "course-3",
		LabID:     "lab-3",
		Title:     "Lab 3",
		Enabled:   true,
		Resources: commands.LabResourceProfile{VCPU: 1, RAMMiB: 1024, DiskGiB: 20},
		Instances: []commands.VMBlueprint{{Name: "vm", ImageID: "image", FlavorID: "flavor", DiskGiB: 20}},
	}}
	labUsecase := &fakeLabUsecase{}
	server := NewServer(labUsecase, nil, catalog, reader, authService, nil, nil, nil)
	token, _, err := authService.IssueSession(authn.Principal{Subject: "student", Role: authn.RoleStudent})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/labs", strings.NewReader(`{
		"student_id":"other-student",
		"course_id":"course-3",
		"lab_id":"lab-3",
		"source":"external"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authService.CookieName(), Value: token})
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if labUsecase.request.StudentID != "student" || labUsecase.request.Source != "student-ui" {
		t.Fatalf("request identity = %#v", labUsecase.request)
	}
}

func TestStudentCannotReadAnotherStudentsLab(t *testing.T) {
	authService := testAuthService(t)
	reader := &fakeReadModel{labs: readmodel.LabRunsView{Labs: []readmodel.LabRunView{{
		ID:        "lab-run-1",
		StudentID: "other-student",
		CourseID:  "course-3",
		LabID:     "lab-3",
		State:     "READY",
	}}}}
	server := NewServer(&fakeLabUsecase{}, nil, &fakeLabCatalog{}, reader, authService, nil, nil, nil)
	token, _, err := authService.IssueSession(authn.Principal{Subject: "student", Role: authn.RoleStudent})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/labs/lab-run-1", nil)
	req.AddCookie(&http.Cookie{Name: authService.CookieName(), Value: token})
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestTeacherCanStartDisabledLabDefinition(t *testing.T) {
	authService := testAuthService(t)
	catalog := &fakeLabCatalog{definition: labcatalog.Definition{
		CourseID:  "course-3",
		LabID:     "lab-3",
		Title:     "Lab 3",
		Enabled:   false,
		Resources: commands.LabResourceProfile{VCPU: 1, RAMMiB: 1024, DiskGiB: 20},
		Instances: []commands.VMBlueprint{{Name: "vm", ImageID: "image", FlavorID: "flavor", DiskGiB: 20}},
	}}
	labUsecase := &fakeLabUsecase{}
	server := NewServer(labUsecase, nil, catalog, &fakeReadModel{}, authService, nil, nil, nil)
	token, _, err := authService.IssueSession(authn.Principal{Subject: "teacher", Role: authn.RoleTeacher})
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/labs", strings.NewReader(`{"course_id":"course-3","lab_id":"lab-3"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: authService.CookieName(), Value: token})
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if labUsecase.request.StudentID != "teacher:teacher" || labUsecase.request.Source != "teacher-ui" {
		t.Fatalf("request identity = %#v", labUsecase.request)
	}
}

func testAuthService(t *testing.T) *authn.Service {
	t.Helper()
	service, err := authn.NewService(authn.Config{
		SessionSecret:  "0123456789abcdef",
		SessionTTL:     time.Hour,
		LocalUsersJSON: `[{"username":"student","password":"student-pass","role":"student"}]`,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

type fakeLabUsecase struct {
	called  bool
	request labs.RequestProvision
}

func (u *fakeLabUsecase) RequestProvision(_ context.Context, req labs.RequestProvision) (labs.ProvisionAccepted, error) {
	u.called = true
	u.request = req
	return labs.ProvisionAccepted{LabRunID: "lab-run-1", SagaID: "saga-1", CommandID: "cmd-1", Status: "REQUESTED"}, nil
}

func (u *fakeLabUsecase) RequestFreeze(context.Context, labs.LabCommand) (labs.CommandAccepted, error) {
	return labs.CommandAccepted{}, nil
}

func (u *fakeLabUsecase) RequestCleanup(context.Context, labs.LabCommand) (labs.CommandAccepted, error) {
	return labs.CommandAccepted{}, nil
}

func (u *fakeLabUsecase) RequestCheck(context.Context, labs.CheckCommand) (labs.CommandAccepted, error) {
	return labs.CommandAccepted{}, nil
}

type fakeLabCatalog struct {
	definition labcatalog.Definition
	update     labcatalog.UpdateRequest
}

func (c *fakeLabCatalog) List(_ context.Context, includeDisabled bool) (labcatalog.ListResult, error) {
	if c.definition.CourseID == "" {
		return labcatalog.ListResult{Labs: []labcatalog.Definition{}}, nil
	}
	if !includeDisabled && !c.definition.Enabled {
		return labcatalog.ListResult{Labs: []labcatalog.Definition{}}, nil
	}
	return labcatalog.ListResult{Labs: []labcatalog.Definition{c.definition}}, nil
}

func (c *fakeLabCatalog) Get(_ context.Context, courseID string, labID string) (labcatalog.Definition, bool, error) {
	if c.definition.CourseID == courseID && c.definition.LabID == labID {
		return c.definition, true, nil
	}
	return labcatalog.Definition{}, false, nil
}

func (c *fakeLabCatalog) Update(_ context.Context, req labcatalog.UpdateRequest) (labcatalog.UpdateResult, error) {
	c.update = req
	return labcatalog.UpdateResult{Lab: req.Definition}, nil
}

type fakeReadModel struct {
	active        bool
	labs          readmodel.LabRunsView
	lastStudentID string
}

func (r *fakeReadModel) ListLabRuns(context.Context, int) (readmodel.LabRunsView, error) {
	return r.labs, nil
}

func (r *fakeReadModel) ListLabRunsByStudent(_ context.Context, studentID string, _ int) (readmodel.LabRunsView, error) {
	r.lastStudentID = studentID
	filtered := readmodel.LabRunsView{Labs: []readmodel.LabRunView{}}
	for _, lab := range r.labs.Labs {
		if lab.StudentID == studentID {
			filtered.Labs = append(filtered.Labs, lab)
		}
	}
	return filtered, nil
}

func (r *fakeReadModel) HasActiveLabRun(context.Context, string) (bool, error) {
	return r.active, nil
}

func (r *fakeReadModel) GetLabRun(_ context.Context, labRunID string) (readmodel.LabRunView, bool, error) {
	for _, lab := range r.labs.Labs {
		if lab.ID == labRunID {
			return lab, true, nil
		}
	}
	return readmodel.LabRunView{}, false, nil
}

func (r *fakeReadModel) GetVDIAccess(context.Context, string) (readmodel.VDIAccessView, bool, error) {
	return readmodel.VDIAccessView{}, false, nil
}

func (r *fakeReadModel) ListLabInstances(context.Context, string) (readmodel.LabInstancesView, bool, error) {
	return readmodel.LabInstancesView{}, false, nil
}

func (r *fakeReadModel) ListLabRunEvents(context.Context, string, int64, int) ([]readmodel.LabRunEvent, error) {
	return nil, nil
}

func (r *fakeReadModel) ListAuditEvents(context.Context, int) (readmodel.AuditView, error) {
	return readmodel.AuditView{}, nil
}

func (r *fakeReadModel) GetSettings(context.Context) (readmodel.SettingsView, error) {
	return readmodel.SettingsView{}, nil
}

func (r *fakeReadModel) GetProjectPool(context.Context) (readmodel.ProjectPoolView, error) {
	return readmodel.ProjectPoolView{}, nil
}

func (r *fakeReadModel) ListCheckRuns(context.Context, string, int) (readmodel.CheckRunsView, error) {
	return readmodel.CheckRunsView{}, nil
}

type fakeOpenStack struct {
	images  []openstack.ImageOption
	flavors []openstack.FlavorOption
}

func (c *fakeOpenStack) Configured() bool {
	return true
}

func (c *fakeOpenStack) Check(context.Context) error {
	return nil
}

func (c *fakeOpenStack) ListImages(context.Context) ([]openstack.ImageOption, error) {
	return c.images, nil
}

func (c *fakeOpenStack) ListFlavors(context.Context) ([]openstack.FlavorOption, error) {
	return c.flavors, nil
}
