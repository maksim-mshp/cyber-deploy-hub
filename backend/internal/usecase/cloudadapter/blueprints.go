package cloudadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts/commands"
)

func LoadBlueprints(ctx context.Context, cfg config.CloudConfig) ([]commands.VMBlueprint, error) {
	if strings.TrimSpace(cfg.BlueprintJSON) != "" {
		return decodeBlueprints([]byte(cfg.BlueprintJSON))
	}
	if strings.TrimSpace(cfg.BlueprintFile) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(cfg.BlueprintFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return decodeBlueprints(raw)
}

func decodeBlueprints(raw []byte) ([]commands.VMBlueprint, error) {
	var blueprints []commands.VMBlueprint
	if err := json.Unmarshal(raw, &blueprints); err != nil {
		return nil, fmt.Errorf("decode lab blueprints: %w", err)
	}
	if err := validateBlueprints(blueprints); err != nil {
		return nil, err
	}
	return blueprints, nil
}

func validateBlueprints(blueprints []commands.VMBlueprint) error {
	for i, blueprint := range blueprints {
		if strings.TrimSpace(blueprint.Name) == "" {
			return fmt.Errorf("blueprint %d name is required", i)
		}
		if strings.TrimSpace(blueprint.ImageID) == "" {
			return fmt.Errorf("blueprint %s image_id is required", blueprint.Name)
		}
		if strings.TrimSpace(blueprint.FlavorID) == "" {
			return fmt.Errorf("blueprint %s flavor_id is required", blueprint.Name)
		}
		if blueprint.DiskGiB <= 0 {
			return fmt.Errorf("blueprint %s disk_gib must be positive", blueprint.Name)
		}
	}
	return nil
}
