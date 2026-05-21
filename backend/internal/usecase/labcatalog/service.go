package labcatalog

import (
	"context"
	"errors"
	"strings"
)

type Repository interface {
	List(ctx context.Context, includeDisabled bool) ([]Definition, error)
	Get(ctx context.Context, courseID string, labID string) (Definition, bool, error)
	Upsert(ctx context.Context, definition Definition, changedBy string) (Definition, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("repository is nil")
	}
	return &Service{repo: repo}, nil
}

func (s *Service) List(ctx context.Context, includeDisabled bool) (ListResult, error) {
	labs, err := s.repo.List(ctx, includeDisabled)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Labs: labs}, nil
}

func (s *Service) Get(ctx context.Context, courseID string, labID string) (Definition, bool, error) {
	return s.repo.Get(ctx, strings.TrimSpace(courseID), strings.TrimSpace(labID))
}

func (s *Service) Update(ctx context.Context, req UpdateRequest) (UpdateResult, error) {
	definition := req.Definition
	definition.CourseID = strings.TrimSpace(definition.CourseID)
	definition.LabID = strings.TrimSpace(definition.LabID)
	definition.Title = strings.TrimSpace(definition.Title)
	definition.Description = strings.TrimSpace(definition.Description)
	req.ChangedBy = strings.TrimSpace(req.ChangedBy)
	if req.ChangedBy == "" {
		return UpdateResult{}, errors.New("changed_by is required")
	}
	if err := validateDefinition(definition); err != nil {
		return UpdateResult{}, err
	}
	saved, err := s.repo.Upsert(ctx, definition, req.ChangedBy)
	if err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Lab: saved}, nil
}

func validateDefinition(definition Definition) error {
	if definition.CourseID == "" {
		return errors.New("course_id is required")
	}
	if definition.LabID == "" {
		return errors.New("lab_id is required")
	}
	if definition.Title == "" {
		return errors.New("title is required")
	}
	if definition.Resources.VCPU <= 0 {
		return errors.New("resources.vcpu must be positive")
	}
	if definition.Resources.RAMMiB <= 0 {
		return errors.New("resources.ram_mib must be positive")
	}
	if definition.Resources.DiskGiB <= 0 {
		return errors.New("resources.disk_gib must be positive")
	}
	if len(definition.Instances) == 0 {
		return errors.New("instances are required")
	}
	seen := map[string]struct{}{}
	var diskSum int64
	for _, instance := range definition.Instances {
		name := strings.TrimSpace(instance.Name)
		if name == "" {
			return errors.New("instance.name is required")
		}
		if _, ok := seen[name]; ok {
			return errors.New("instance names must be unique")
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(instance.ImageID) == "" {
			return errors.New("instance.image_id is required")
		}
		if strings.TrimSpace(instance.FlavorID) == "" {
			return errors.New("instance.flavor_id is required")
		}
		if instance.DiskGiB <= 0 {
			return errors.New("instance.disk_gib must be positive")
		}
		diskSum += instance.DiskGiB
	}
	if diskSum > definition.Resources.DiskGiB {
		return errors.New("instances disk_gib exceeds resources.disk_gib")
	}
	return nil
}
