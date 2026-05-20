package capacity

import (
	"context"
	"strings"
	"testing"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/contracts/commands"
)

func TestOpenStackQuotaCheckerAllowsWithinQuota(t *testing.T) {
	checker := NewOpenStackQuotaChecker(fakeQuotaProvider{quota: &openstack.ProjectQuota{
		VCPUFree:    16,
		RAMMiBFree:  32768,
		DiskGiBFree: 500,
	}})

	result, err := checker.Check(context.Background(), "project-1", commands.LabResourceProfile{
		VCPU:    8,
		RAMMiB:  16384,
		DiskGiB: 100,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("quota denied: %s", result.Reason)
	}
}

func TestOpenStackQuotaCheckerDeniesExceedingQuota(t *testing.T) {
	checker := NewOpenStackQuotaChecker(fakeQuotaProvider{quota: &openstack.ProjectQuota{
		VCPUFree:    4,
		RAMMiBFree:  4096,
		DiskGiBFree: 20,
	}})

	result, err := checker.Check(context.Background(), "project-1", commands.LabResourceProfile{
		VCPU:    8,
		RAMMiB:  8192,
		DiskGiB: 100,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected quota denial")
	}
	for _, part := range []string{"free vcpus", "free ram", "free disk"} {
		if !strings.Contains(result.Reason, part) {
			t.Fatalf("reason %q missing %q", result.Reason, part)
		}
	}
}

type fakeQuotaProvider struct {
	quota *openstack.ProjectQuota
}

func (p fakeQuotaProvider) ProjectQuota(context.Context, string) (*openstack.ProjectQuota, error) {
	return p.quota, nil
}
