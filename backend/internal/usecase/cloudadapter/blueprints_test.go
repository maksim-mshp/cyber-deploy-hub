package cloudadapter

import (
	"testing"
)

func TestDecodeBlueprintsValidatesRequiredFields(t *testing.T) {
	_, err := decodeBlueprints([]byte(`[{"name":"vm","image_id":"image","flavor_id":"flavor","disk_gib":10}]`))
	if err != nil {
		t.Fatalf("decodeBlueprints: %v", err)
	}

	_, err = decodeBlueprints([]byte(`[{"name":"vm","image_id":"image","flavor_id":"flavor","disk_gib":0}]`))
	if err == nil {
		t.Fatal("expected disk validation error")
	}
}

func TestResourceNamesAreStableAndBounded(t *testing.T) {
	got := resourceName("11111111-2222-3333-4444-555555555555", "L_MS", "vm")
	want := "cdh-111111112222-l-ms-vm"
	if got != want {
		t.Fatalf("resourceName = %q, want %q", got, want)
	}
}
