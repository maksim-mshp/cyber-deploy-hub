package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"cyber-deploy-hub/internal/config"
)

func LoadProfiles(ctx context.Context, cfg config.CheckerConfig) ([]Profile, error) {
	if strings.TrimSpace(cfg.ProfileJSON) != "" {
		return decodeProfiles([]byte(cfg.ProfileJSON), cfg.DefaultSSHUser)
	}
	if strings.TrimSpace(cfg.ProfileFile) == "" {
		return defaultProfiles(cfg.DefaultSSHUser), nil
	}
	raw, err := os.ReadFile(cfg.ProfileFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultProfiles(cfg.DefaultSSHUser), nil
		}
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return decodeProfiles(raw, cfg.DefaultSSHUser)
}

func decodeProfiles(raw []byte, defaultSSHUser string) ([]Profile, error) {
	var profiles []Profile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, fmt.Errorf("decode checker profiles: %w", err)
	}
	if len(profiles) == 0 {
		return defaultProfiles(defaultSSHUser), nil
	}
	for i := range profiles {
		normalizeProfile(&profiles[i], defaultSSHUser)
		if err := validateProfile(profiles[i]); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func defaultProfiles(defaultSSHUser string) []Profile {
	profile := Profile{
		ID:      "default",
		Name:    "Linux baseline",
		SSHUser: defaultSSHUser,
		Steps: []Step{
			{Name: "OS release file exists", Type: StepFileExists, Path: "/etc/os-release", TimeoutSeconds: 10},
			{Name: "Command exit code", Type: StepCommandExitCode, Command: "test -r /etc/os-release", ExpectedExitCode: 0, TimeoutSeconds: 10},
		},
	}
	normalizeProfile(&profile, defaultSSHUser)
	return []Profile{profile}
}

func normalizeProfile(profile *Profile, defaultSSHUser string) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.SSHUser = strings.TrimSpace(profile.SSHUser)
	if profile.SSHUser == "" {
		profile.SSHUser = strings.TrimSpace(defaultSSHUser)
	}
	if profile.Name == "" {
		profile.Name = profile.ID
	}
	for i := range profile.Steps {
		if profile.Steps[i].Sequence <= 0 {
			profile.Steps[i].Sequence = i + 1
		}
		if profile.Steps[i].ExpectedExitCode == 0 {
			profile.Steps[i].ExpectedExitCode = 0
		}
		if profile.Steps[i].TimeoutSeconds <= 0 {
			profile.Steps[i].TimeoutSeconds = 15
		}
	}
}

func validateProfile(profile Profile) error {
	if profile.ID == "" {
		return errors.New("checker profile id is required")
	}
	if profile.SSHUser == "" {
		return fmt.Errorf("checker profile %s ssh_user is required", profile.ID)
	}
	if len(profile.Steps) == 0 {
		return fmt.Errorf("checker profile %s steps are required", profile.ID)
	}
	seen := map[int]struct{}{}
	for _, step := range profile.Steps {
		if _, ok := seen[step.Sequence]; ok {
			return fmt.Errorf("checker profile %s has duplicate step sequence %d", profile.ID, step.Sequence)
		}
		seen[step.Sequence] = struct{}{}
		if err := validateStep(step); err != nil {
			return fmt.Errorf("checker profile %s step %d: %w", profile.ID, step.Sequence, err)
		}
	}
	return nil
}

func validateStep(step Step) error {
	if strings.TrimSpace(step.Name) == "" {
		return errors.New("name is required")
	}
	switch step.Type {
	case StepPackageInstalled:
		if strings.TrimSpace(step.Package) == "" {
			return errors.New("package is required")
		}
	case StepFileExists:
		if strings.TrimSpace(step.Path) == "" {
			return errors.New("path is required")
		}
	case StepFileContains:
		if strings.TrimSpace(step.Path) == "" {
			return errors.New("path is required")
		}
		if step.Contains == "" {
			return errors.New("contains is required")
		}
	case StepServiceActive:
		if strings.TrimSpace(step.Service) == "" {
			return errors.New("service is required")
		}
	case StepPortOpen:
		if step.Port <= 0 || step.Port > 65535 {
			return errors.New("port must be in 1..65535")
		}
	case StepCommandExitCode:
		if strings.TrimSpace(step.Command) == "" {
			return errors.New("command is required")
		}
	default:
		return fmt.Errorf("unsupported type %q", step.Type)
	}
	if step.TimeoutSeconds <= 0 {
		return errors.New("timeout_seconds must be positive")
	}
	return nil
}
