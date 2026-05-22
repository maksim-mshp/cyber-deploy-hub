package checker

import (
	"os"
	"testing"
)

func TestBundledProfilesIncludeLab3Storage(t *testing.T) {
	raw, err := os.ReadFile("../../../config/checker_profiles.json")
	if err != nil {
		t.Fatalf("read bundled profiles: %v", err)
	}
	profiles, err := decodeProfiles(raw, "ubuntu")
	if err != nil {
		t.Fatalf("decodeProfiles: %v", err)
	}

	var storage Profile
	for _, profile := range profiles {
		if profile.ID == "lab-3-storage" {
			storage = profile
			break
		}
	}
	if storage.ID == "" {
		t.Fatal("lab-3-storage profile is missing")
	}
	if storage.SSHUser != "ubuntu" {
		t.Fatalf("ssh_user = %q", storage.SSHUser)
	}
	if len(storage.Steps) < 6 {
		t.Fatalf("storage profile must include real OS, SSH and storage checks, got %d steps", len(storage.Steps))
	}
}
