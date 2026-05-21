package labcatalog

import (
	"context"
	"strings"
	"testing"

	"cyber-deploy-hub/internal/contracts/commands"
)

func TestServiceUpdatePersistsTeacherLabDefinition(t *testing.T) {
	repo := &fakeRepository{}
	service, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.Update(context.Background(), UpdateRequest{
		ChangedBy: " teacher-1 ",
		Definition: Definition{
			CourseID: " course-3 ",
			LabID:    " lab-3 ",
			Title:    " Lab 3 ",
			Enabled:  true,
			Resources: commands.LabResourceProfile{
				VCPU:    2,
				RAMMiB:  4096,
				DiskGiB: 40,
			},
			Instances: []commands.VMBlueprint{{
				Name:     "vm-1",
				ImageID:  "image-1",
				FlavorID: "flavor-1",
				DiskGiB:  20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if result.Lab.CourseID != "course-3" || result.Lab.LabID != "lab-3" {
		t.Fatalf("lab identifiers were not normalized: %#v", result.Lab)
	}
	if repo.changedBy != "teacher-1" {
		t.Fatalf("changed_by = %q", repo.changedBy)
	}
	if repo.definition.Resources.VCPU != 2 || len(repo.definition.Instances) != 1 {
		t.Fatalf("saved definition = %#v", repo.definition)
	}
}

func TestServiceUpdateRejectsInvalidDefinition(t *testing.T) {
	service, err := NewService(&fakeRepository{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = service.Update(context.Background(), UpdateRequest{
		ChangedBy: "teacher-1",
		Definition: Definition{
			CourseID: "course-3",
			LabID:    "lab-3",
			Title:    "Lab 3",
			Enabled:  true,
			Resources: commands.LabResourceProfile{
				VCPU:    1,
				RAMMiB:  1024,
				DiskGiB: 10,
			},
			Instances: []commands.VMBlueprint{
				{Name: "vm-1", ImageID: "image-1", FlavorID: "flavor-1", DiskGiB: 8},
				{Name: "vm-2", ImageID: "image-2", FlavorID: "flavor-2", DiskGiB: 8},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disk_gib") {
		t.Fatalf("expected disk validation error, got %v", err)
	}

	_, err = service.Update(context.Background(), UpdateRequest{
		ChangedBy: "teacher-1",
		Definition: Definition{
			CourseID: "course-3",
			LabID:    "lab-3",
			Title:    "Lab 3",
			Enabled:  true,
			Resources: commands.LabResourceProfile{
				VCPU:    2,
				RAMMiB:  2048,
				DiskGiB: 40,
			},
			Instances: []commands.VMBlueprint{
				{Name: "vm-1", ImageID: "image-1", FlavorID: "flavor-1", DiskGiB: 10},
				{Name: "vm-1", ImageID: "image-2", FlavorID: "flavor-2", DiskGiB: 10},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("expected duplicate name validation error, got %v", err)
	}
}

type fakeRepository struct {
	definition Definition
	changedBy  string
}

func (r *fakeRepository) List(_ context.Context, _ bool) ([]Definition, error) {
	if r.definition.CourseID == "" {
		return []Definition{}, nil
	}
	return []Definition{r.definition}, nil
}

func (r *fakeRepository) Get(_ context.Context, courseID string, labID string) (Definition, bool, error) {
	if r.definition.CourseID == courseID && r.definition.LabID == labID {
		return r.definition, true, nil
	}
	return Definition{}, false, nil
}

func (r *fakeRepository) Upsert(_ context.Context, definition Definition, changedBy string) (Definition, error) {
	r.definition = definition
	r.changedBy = changedBy
	return definition, nil
}
