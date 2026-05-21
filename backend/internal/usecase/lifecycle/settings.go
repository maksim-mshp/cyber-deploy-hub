package lifecycle

import (
	"errors"
	"fmt"
	"math"
	"time"

	"cyber-deploy-hub/internal/config"
)

func DefaultSettings(cfg config.LifecycleConfig) RuntimeSettings {
	labTTL := cfg.DefaultLabTTL
	if labTTL <= 0 {
		labTTL = 2 * time.Hour
	}
	freezeTTL := cfg.DefaultFreezeTTL
	if freezeTTL <= 0 {
		freezeTTL = 24 * time.Hour
	}
	threshold := cfg.DefaultCapacityThreshold
	if threshold <= 0 {
		threshold = DefaultCapacityThresholdValue
	}
	return RuntimeSettings{
		LabTTLSeconds:            int64(labTTL.Seconds()),
		FreezeTTLSeconds:         int64(freezeTTL.Seconds()),
		CapacityThresholdPercent: threshold,
	}
}

func MergeSettings(defaults RuntimeSettings, overrides map[string]any) (RuntimeSettings, error) {
	settings := defaults
	for key, value := range overrides {
		switch key {
		case SettingLabTTLSeconds:
			seconds, err := positiveInt64(value)
			if err != nil {
				return RuntimeSettings{}, fmt.Errorf("%s: %w", key, err)
			}
			settings.LabTTLSeconds = seconds
		case SettingFreezeTTLSeconds:
			seconds, err := positiveInt64(value)
			if err != nil {
				return RuntimeSettings{}, fmt.Errorf("%s: %w", key, err)
			}
			settings.FreezeTTLSeconds = seconds
		case SettingCapacityThreshold:
			threshold, err := thresholdPercent(value)
			if err != nil {
				return RuntimeSettings{}, fmt.Errorf("%s: %w", key, err)
			}
			settings.CapacityThresholdPercent = threshold
		default:
			return RuntimeSettings{}, fmt.Errorf("unsupported setting %q", key)
		}
	}
	return settings, nil
}

func positiveInt64(value any) (int64, error) {
	number, err := numeric(value)
	if err != nil {
		return 0, err
	}
	if number <= 0 || math.Trunc(number) != number {
		return 0, errors.New("must be a positive integer")
	}
	return int64(number), nil
}

func thresholdPercent(value any) (float64, error) {
	number, err := numeric(value)
	if err != nil {
		return 0, err
	}
	if number <= 0 || number > 100 {
		return 0, errors.New("must be in range (0, 100]")
	}
	return number, nil
}

func numeric(value any) (float64, error) {
	switch typed := value.(type) {
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case jsonNumber:
		return typed.Float64()
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", value)
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}
