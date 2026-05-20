package capacity

import (
	"context"
	"fmt"
	"strings"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/contracts/commands"
)

type QuotaChecker interface {
	Check(ctx context.Context, projectID string, resources commands.LabResourceProfile) (QuotaCheck, error)
}

type QuotaCheck struct {
	Allowed bool
	Reason  string
}

type NoopQuotaChecker struct{}

func (NoopQuotaChecker) Check(context.Context, string, commands.LabResourceProfile) (QuotaCheck, error) {
	return QuotaCheck{Allowed: true}, nil
}

type OpenStackQuotaProvider interface {
	ProjectQuota(ctx context.Context, projectID string) (*openstack.ProjectQuota, error)
}

type OpenStackQuotaChecker struct {
	provider OpenStackQuotaProvider
}

func NewOpenStackQuotaChecker(provider OpenStackQuotaProvider) *OpenStackQuotaChecker {
	return &OpenStackQuotaChecker{provider: provider}
}

func (c *OpenStackQuotaChecker) Check(ctx context.Context, projectID string, resources commands.LabResourceProfile) (QuotaCheck, error) {
	quota, err := c.provider.ProjectQuota(ctx, projectID)
	if err != nil {
		return QuotaCheck{}, err
	}

	reasons := make([]string, 0, 3)
	if resources.VCPU > quota.VCPUFree {
		reasons = append(reasons, fmt.Sprintf("project quota has %d free vcpus", quota.VCPUFree))
	}
	if resources.RAMMiB > quota.RAMMiBFree {
		reasons = append(reasons, fmt.Sprintf("project quota has %d MiB free ram", quota.RAMMiBFree))
	}
	if resources.DiskGiB > quota.DiskGiBFree {
		reasons = append(reasons, fmt.Sprintf("project quota has %d GiB free disk", quota.DiskGiBFree))
	}
	if len(reasons) > 0 {
		return QuotaCheck{Allowed: false, Reason: strings.Join(reasons, "; ")}, nil
	}
	return QuotaCheck{Allowed: true}, nil
}
