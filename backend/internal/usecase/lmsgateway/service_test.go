package lmsgateway

import (
	"context"
	"encoding/json"
	"testing"

	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/usecase/labcatalog"
)

func TestServiceLaunchPublishesProvisionCommand(t *testing.T) {
	t.Parallel()

	mapper, err := NewMapper(`{"course-ext":"course-linux"}`, `{"assignment-ext":"LAB-02"}`)
	if err != nil {
		t.Fatalf("NewMapper: %v", err)
	}
	repo := &fakeRepository{}
	catalog := &fakeCatalog{definition: labcatalog.Definition{
		CourseID:  "course-linux",
		LabID:     "LAB-02",
		Enabled:   true,
		Resources: commands.LabResourceProfile{VCPU: 2, RAMMiB: 4096, DiskGiB: 40},
		Instances: []commands.VMBlueprint{{
			Name:     "vm-1",
			ImageID:  "image-1",
			FlavorID: "flavor-1",
			DiskGiB:  20,
		}},
	}}
	service, err := NewService("lms-gateway-service", "moodle", mapper, repo, catalog)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, inserted, err := service.Launch(context.Background(), LaunchRequest{
		MoodleUserID:       "student-ext",
		MoodleCourseID:     "course-ext",
		MoodleAssignmentID: "assignment-ext",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !inserted {
		t.Fatal("launch was not inserted")
	}
	if result.Mapping.StudentID != "moodle:student-ext" || result.Mapping.CourseID != "course-linux" || result.Mapping.LabID != "LAB-02" {
		t.Fatalf("unexpected mapping: %+v", result.Mapping)
	}
	if repo.command.MessageType != commands.RequestProvisionV1.String() {
		t.Fatalf("command = %s", repo.command.MessageType)
	}
	var payload commands.RequestProvisionV1Payload
	if err := json.Unmarshal(repo.command.Payload, &payload); err != nil {
		t.Fatalf("decode command payload: %v", err)
	}
	if payload.StudentID != result.Mapping.StudentID || payload.CourseID != result.Mapping.CourseID || payload.LabID != result.Mapping.LabID {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Resources.VCPU != 2 || len(payload.Instances) != 1 || payload.Instances[0].ImageID != "image-1" {
		t.Fatalf("payload must include catalog configuration: %+v", payload)
	}
}

func TestServiceLaunchReportsActiveLabWithoutNewCommand(t *testing.T) {
	t.Parallel()

	mapper, err := NewMapper(`{"course-ext":"course-linux"}`, `{"assignment-ext":"LAB-02"}`)
	if err != nil {
		t.Fatalf("NewMapper: %v", err)
	}
	repo := &fakeRepository{
		saved: LaunchRecord{
			ID:             "11111111-1111-1111-1111-111111111111",
			LabRunID:       "22222222-2222-2222-2222-222222222222",
			LocalStudentID: "moodle:student-ext",
			LocalCourseID:  "course-linux",
			LocalLabID:     "LAB-02",
			Status:         LaunchStatusActiveLabExists,
		},
		inserted: false,
	}
	catalog := &fakeCatalog{definition: labcatalog.Definition{
		CourseID:  "course-linux",
		LabID:     "LAB-02",
		Enabled:   true,
		Resources: commands.LabResourceProfile{VCPU: 2, RAMMiB: 4096, DiskGiB: 40},
		Instances: []commands.VMBlueprint{{Name: "vm-1", ImageID: "image-1", FlavorID: "flavor-1", DiskGiB: 20}},
	}}
	service, err := NewService("lms-gateway-service", "moodle", mapper, repo, catalog)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, inserted, err := service.Launch(context.Background(), LaunchRequest{
		MoodleUserID:       "student-ext",
		MoodleCourseID:     "course-ext",
		MoodleAssignmentID: "assignment-ext",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if inserted {
		t.Fatal("active lab conflict must not enqueue a new launch")
	}
	if result.Status != LaunchStatusActiveLabExists || result.LabRunID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeRepository struct {
	command  contracts.Envelope
	saved    LaunchRecord
	inserted bool
}

func (r *fakeRepository) SaveLaunch(_ context.Context, launch LaunchRecord, command contracts.Envelope) (LaunchRecord, bool, error) {
	r.command = command
	if r.saved.ID != "" {
		return r.saved, r.inserted, nil
	}
	launch.CreatedAt = command.OccurredAt
	return launch, true, nil
}

func (r *fakeRepository) LoadResult(context.Context, string) (LaunchResult, bool, error) {
	return LaunchResult{}, false, nil
}

type fakeCatalog struct {
	definition labcatalog.Definition
}

func (c *fakeCatalog) Get(_ context.Context, courseID string, labID string) (labcatalog.Definition, bool, error) {
	if c.definition.CourseID == courseID && c.definition.LabID == labID {
		return c.definition, true, nil
	}
	return labcatalog.Definition{}, false, nil
}
