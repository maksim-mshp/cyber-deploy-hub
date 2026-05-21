package lifecycle

import "testing"

func TestMergeSettingsValidatesAllowedSettings(t *testing.T) {
	defaults := RuntimeSettings{
		LabTTLSeconds:            7200,
		FreezeTTLSeconds:         86400,
		CapacityThresholdPercent: 90,
	}
	settings, err := MergeSettings(defaults, map[string]any{
		SettingLabTTLSeconds:     float64(60),
		SettingFreezeTTLSeconds:  float64(120),
		SettingCapacityThreshold: float64(80),
	})
	if err != nil {
		t.Fatalf("MergeSettings: %v", err)
	}
	if settings.LabTTLSeconds != 60 || settings.FreezeTTLSeconds != 120 || settings.CapacityThresholdPercent != 80 {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestMergeSettingsRejectsInvalidThreshold(t *testing.T) {
	_, err := MergeSettings(RuntimeSettings{}, map[string]any{
		SettingCapacityThreshold: float64(101),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
