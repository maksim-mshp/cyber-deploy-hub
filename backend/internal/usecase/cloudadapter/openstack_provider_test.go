package cloudadapter

import (
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
