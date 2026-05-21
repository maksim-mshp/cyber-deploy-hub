package readmodel

import "testing"

func TestInstanceVDIAccessBuildsTargetURL(t *testing.T) {
	base := VDIAccessView{
		LabRunID:  "lab-1",
		Available: true,
		URL:       "https://vdi.example/vdi/session/token",
		State:     "READY",
	}
	instance := LabInstanceView{
		Name:     "L-MS",
		State:    "ACTIVE",
		ServerID: "server-1",
		FixedIP:  "10.0.0.10",
	}

	access := instanceVDIAccess(base, instance)

	if !access.Available {
		t.Fatalf("access is not available: %#v", access)
	}
	want := "https://vdi.example/vdi/session/token?fixed_ip=10.0.0.10&instance_name=L-MS&server_id=server-1"
	if access.URL != want {
		t.Fatalf("url = %q, want %q", access.URL, want)
	}
}

func TestInstanceVDIAccessRequiresActiveServer(t *testing.T) {
	base := VDIAccessView{
		LabRunID:  "lab-1",
		Available: true,
		URL:       "https://vdi.example/vdi/session/token",
		State:     "READY",
	}
	instance := LabInstanceView{
		Name:  "L-MS",
		State: "CLEANED",
	}

	access := instanceVDIAccess(base, instance)

	if access.Available {
		t.Fatalf("access should not be available: %#v", access)
	}
	if access.Reason != "instance_vdi_unavailable" {
		t.Fatalf("reason = %q", access.Reason)
	}
}
