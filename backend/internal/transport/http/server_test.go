package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/labcatalog"
	"cyber-deploy-hub/internal/usecase/labs"
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
	server := NewServer(labUsecase, nil, catalog, nil, nil, nil, nil)
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
	server := NewServer(labUsecase, nil, catalog, nil, nil, nil, nil)
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
	server := NewServer(&fakeLabUsecase{}, nil, catalog, nil, nil, nil, nil)
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
