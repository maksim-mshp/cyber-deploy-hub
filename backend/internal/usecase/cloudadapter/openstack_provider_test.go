package cloudadapter

import (
	"strings"
	"testing"

	"cyber-deploy-hub/internal/config"
)

func TestCleanupPreservesReusedPrivateNetwork(t *testing.T) {
	cfg := config.CloudConfig{
		PrivateNetworkID:    "network-template",
		PrivateSubnetID:     "subnet-template",
		ReusePrivateNetwork: true,
	}

	if shouldDeleteNetwork("network-template", cfg) {
		t.Fatal("expected configured reusable network to be preserved")
	}
	if shouldDeleteSubnet("subnet-template", cfg) {
		t.Fatal("expected configured reusable subnet to be preserved")
	}
	if shouldDeleteNetwork("network-from-subnet", config.CloudConfig{ReusePrivateNetwork: true}) {
		t.Fatal("expected implicit reusable network to be preserved")
	}
	if shouldDeleteSubnet("subnet-from-config", config.CloudConfig{ReusePrivateNetwork: true}) {
		t.Fatal("expected implicit reusable subnet to be preserved")
	}
}

func TestCleanupDeletesOnlyOwnedPrivateNetwork(t *testing.T) {
	cfg := config.CloudConfig{
		PrivateNetworkID: "network-template",
		PrivateSubnetID:  "subnet-template",
	}

	if shouldDeleteNetwork("", cfg) {
		t.Fatal("empty network id must not be deleted")
	}
	if shouldDeleteSubnet("", cfg) {
		t.Fatal("empty subnet id must not be deleted")
	}
	if shouldDeleteNetwork("network-template", cfg) {
		t.Fatal("template network must not be deleted")
	}
	if shouldDeleteSubnet("subnet-template", cfg) {
		t.Fatal("template subnet must not be deleted")
	}
	if !shouldDeleteNetwork("network-owned", cfg) {
		t.Fatal("owned deployment network must be deleted")
	}
	if !shouldDeleteSubnet("subnet-owned", cfg) {
		t.Fatal("owned deployment subnet must be deleted")
	}
}

func TestFixedIPConflictMessageIncludesActionableResourceDetails(t *testing.T) {
	message := fixedIPConflictMessage("10.0.0.10", &fixedIPConflictInfo{
		PortID:          "port-1",
		PortName:        "cdh-old-vm-1-port",
		DeviceOwner:     "compute:nova",
		DeviceID:        "server-1",
		ServerName:      "cdh-old-vm-1-vm",
		ServerStatus:    "ACTIVE",
		ServerManagedBy: managedByMetadataValue,
		ServerLabRunID:  "old-lab-run",
	})

	for _, want := range []string{
		"fixed IP 10.0.0.10 is already in use",
		"port_id=port-1",
		"device_id=server-1",
		"server_name=cdh-old-vm-1-vm",
		"resource_lab_run_id=old-lab-run",
		"cleanup the stale Cyber Deploy Hub resource",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
}
