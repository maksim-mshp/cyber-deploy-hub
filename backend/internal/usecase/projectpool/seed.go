package projectpool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"cyber-deploy-hub/internal/config"
)

type Seed struct {
	Domains  []SeedDomain  `json:"domains"`
	Projects []SeedProject `json:"projects"`
}

type SeedDomain struct {
	DomainID string `json:"domain_id"`
	CourseID string `json:"course_id"`
	Name     string `json:"name"`
}

type SeedProject struct {
	ProjectID string `json:"project_id"`
	DomainID  string `json:"domain_id"`
	Name      string `json:"name"`
}

func LoadSeed(_ context.Context, cfg config.ProjectPoolConfig) (Seed, error) {
	if strings.TrimSpace(cfg.SeedJSON) != "" {
		return ParseSeed([]byte(cfg.SeedJSON))
	}
	if strings.TrimSpace(cfg.SeedFile) == "" {
		return Seed{}, nil
	}

	payload, err := os.ReadFile(cfg.SeedFile)
	if err != nil {
		return Seed{}, err
	}
	return ParseSeed(payload)
}

func ParseSeed(payload []byte) (Seed, error) {
	var seed Seed
	if err := json.Unmarshal(payload, &seed); err != nil {
		return Seed{}, err
	}
	if err := seed.Validate(); err != nil {
		return Seed{}, err
	}
	return seed, nil
}

func (s Seed) Empty() bool {
	return len(s.Domains) == 0 && len(s.Projects) == 0
}

func (s Seed) Validate() error {
	domains := make(map[string]struct{}, len(s.Domains))
	courses := make(map[string]struct{}, len(s.Domains))
	for _, domain := range s.Domains {
		if strings.TrimSpace(domain.DomainID) == "" {
			return errors.New("domain_id is required")
		}
		if strings.TrimSpace(domain.CourseID) == "" {
			return fmt.Errorf("course_id is required for domain %s", domain.DomainID)
		}
		if _, ok := domains[domain.DomainID]; ok {
			return fmt.Errorf("duplicate domain_id %s", domain.DomainID)
		}
		if _, ok := courses[domain.CourseID]; ok {
			return fmt.Errorf("duplicate course_id %s", domain.CourseID)
		}
		domains[domain.DomainID] = struct{}{}
		courses[domain.CourseID] = struct{}{}
	}

	projects := make(map[string]struct{}, len(s.Projects))
	for _, project := range s.Projects {
		if _, err := uuid.Parse(project.ProjectID); err != nil {
			return fmt.Errorf("project_id %q must be uuid: %w", project.ProjectID, err)
		}
		if strings.TrimSpace(project.DomainID) == "" {
			return fmt.Errorf("domain_id is required for project %s", project.ProjectID)
		}
		if _, ok := domains[project.DomainID]; !ok {
			return fmt.Errorf("project %s references unknown domain %s", project.ProjectID, project.DomainID)
		}
		if strings.TrimSpace(project.Name) == "" {
			return fmt.Errorf("name is required for project %s", project.ProjectID)
		}
		if _, ok := projects[project.ProjectID]; ok {
			return fmt.Errorf("duplicate project_id %s", project.ProjectID)
		}
		projects[project.ProjectID] = struct{}{}
	}
	return nil
}
