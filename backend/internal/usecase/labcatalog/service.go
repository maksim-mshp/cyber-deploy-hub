package labcatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cyber-deploy-hub/internal/contracts/commands"
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
	normalizeCheckProfile(definition.CheckProfile)
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
	if err := validateCheckProfile(definition.CheckProfile); err != nil {
		return err
	}
	return nil
}

func normalizeCheckProfile(profile *commands.CheckerProfileV1) {
	if profile == nil {
		return
	}
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.SSHUser = strings.TrimSpace(profile.SSHUser)
	for i := range profile.Steps {
		profile.Steps[i].Name = strings.TrimSpace(profile.Steps[i].Name)
		profile.Steps[i].Type = strings.TrimSpace(profile.Steps[i].Type)
		profile.Steps[i].Package = strings.TrimSpace(profile.Steps[i].Package)
		profile.Steps[i].Path = strings.TrimSpace(profile.Steps[i].Path)
		profile.Steps[i].Service = strings.TrimSpace(profile.Steps[i].Service)
		profile.Steps[i].Command = strings.TrimSpace(profile.Steps[i].Command)
		if profile.Steps[i].Sequence <= 0 {
			profile.Steps[i].Sequence = i + 1
		}
	}
}

func validateCheckProfile(profile *commands.CheckerProfileV1) error {
	if profile == nil {
		return nil
	}
	if profile.ID == "" {
		return errors.New("check_profile.id is required")
	}
	if profile.SSHUser == "" {
		return errors.New("check_profile.ssh_user is required")
	}
	if len(profile.Steps) == 0 {
		return errors.New("check_profile.steps are required")
	}
	if len(profile.Steps) > 24 {
		return errors.New("check_profile.steps must not exceed 24")
	}
	seen := map[int]struct{}{}
	for _, step := range profile.Steps {
		if _, ok := seen[step.Sequence]; ok {
			return fmt.Errorf("check_profile.steps sequence %d repeats", step.Sequence)
		}
		seen[step.Sequence] = struct{}{}
		if err := validateCheckStep(step); err != nil {
			return fmt.Errorf("check_profile.steps[%d]: %w", step.Sequence, err)
		}
	}
	return nil
}

func validateCheckStep(step commands.CheckerStepV1) error {
	if step.Name == "" {
		return errors.New("name is required")
	}
	if step.TimeoutSeconds <= 0 || step.TimeoutSeconds > 60 {
		return errors.New("timeout_seconds must be in 1..60")
	}
	switch step.Type {
	case "package_installed":
		if step.Package == "" {
			return errors.New("package is required")
		}
	case "file_exists":
		if step.Path == "" {
			return errors.New("path is required")
		}
	case "file_contains":
		if step.Path == "" || step.Contains == "" {
			return errors.New("path and contains are required")
		}
	case "service_active":
		if step.Service == "" {
			return errors.New("service is required")
		}
	case "port_open":
		if step.Port <= 0 || step.Port > 65535 {
			return errors.New("port must be in 1..65535")
		}
	case "command_exit_code":
		if step.Command == "" {
			return errors.New("command is required")
		}
	default:
		return fmt.Errorf("type %q is unsupported", step.Type)
	}
	return nil
}
