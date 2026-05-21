package capacity

import (
	"context"
	"encoding/json"
	"strings"

	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/ki"
)

type StatProvider interface {
	Current(ctx context.Context) (Snapshot, error)
}

type KIProvider struct {
	client interface {
		ClusterStat(ctx context.Context) (*ki.ClusterStat, error)
		ProjectQuota(ctx context.Context, projectID string) (*ki.ProjectQuota, error)
	}
	projectID string
}

func NewKIProvider(client interface {
	ClusterStat(ctx context.Context) (*ki.ClusterStat, error)
	ProjectQuota(ctx context.Context, projectID string) (*ki.ProjectQuota, error)
}, projectID string) *KIProvider {
	return &KIProvider{client: client, projectID: projectID}
}

func (p *KIProvider) Current(ctx context.Context) (Snapshot, error) {
	stat, err := p.client.ClusterStat(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	quota, err := p.client.ProjectQuota(ctx, p.projectID)
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := json.Marshal(stat)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{
		Source:          "ki",
		VCPUsTotal:      stat.Compute.VCPUs,
		VCPUsFree:       stat.Compute.VCPUsFree,
		RAMMiBTotal:     memoryToMiB(stat.Compute.VMMemCapacity),
		RAMMiBFree:      memoryToMiB(stat.Compute.VMMemFree),
		StorageGiBTotal: storageToGiB(stat.Compute.BlockCapacity),
		StorageGiBUsed:  storageToGiB(stat.Compute.BlockUsage),
		RawPayload:      raw,
	}
	applyProjectStorageQuota(&snapshot, quota)
	return snapshot, nil
}

type KIQuotaChecker struct {
	client interface {
		ProjectQuota(ctx context.Context, projectID string) (*ki.ProjectQuota, error)
	}
}

func NewKIQuotaChecker(client interface {
	ProjectQuota(ctx context.Context, projectID string) (*ki.ProjectQuota, error)
}) *KIQuotaChecker {
	return &KIQuotaChecker{client: client}
}

func (c *KIQuotaChecker) Check(ctx context.Context, projectID string, resources commands.LabResourceProfile) (QuotaCheck, error) {
	quota, err := c.client.ProjectQuota(ctx, projectID)
	if err != nil {
		return QuotaCheck{}, err
	}
	coresFree := int(quota.Compute.Cores.Limit - quota.Compute.Cores.Used)
	ramMiBFree := int(memoryToMiB(quota.Compute.RAM.Limit - quota.Compute.RAM.Used))
	storageGiBFree := storageToGiB(storageLimit(quota) - storageUsed(quota))

	reasons := make([]string, 0, 3)
	if resources.VCPU > coresFree {
		reasons = append(reasons, "project quota has insufficient free vcpus")
	}
	if resources.RAMMiB > ramMiBFree {
		reasons = append(reasons, "project quota has insufficient free ram")
	}
	if resources.DiskGiB > storageGiBFree {
		reasons = append(reasons, "project quota has insufficient free storage")
	}
	if len(reasons) > 0 {
		return QuotaCheck{Allowed: false, Reason: strings.Join(reasons, "; ")}, nil
	}
	return QuotaCheck{Allowed: true}, nil
}

func applyProjectStorageQuota(snapshot *Snapshot, quota *ki.ProjectQuota) {
	limit := storageLimit(quota)
	if limit <= 0 {
		return
	}
	snapshot.StorageGiBTotal = storageToGiB(limit)
	snapshot.StorageGiBUsed = storageToGiB(storageUsed(quota))
}

func storageLimit(quota *ki.ProjectQuota) int64 {
	if quota == nil {
		return 0
	}
	if usage, ok := quota.Storage.StoragePolicies["default"]; ok {
		return usage.Limit
	}
	for _, usage := range quota.Storage.StoragePolicies {
		return usage.Limit
	}
	return 0
}

func storageUsed(quota *ki.ProjectQuota) int64 {
	if quota == nil {
		return 0
	}
	if usage, ok := quota.Storage.StoragePolicies["default"]; ok {
		return usage.Used
	}
	for _, usage := range quota.Storage.StoragePolicies {
		return usage.Used
	}
	return 0
}

func memoryToMiB(value int64) int64 {
	if value > 10*1024*1024 {
		return value / 1024 / 1024
	}
	return value
}

func storageToGiB(value int64) int64 {
	if value > 10*1024*1024 {
		return value / 1024 / 1024 / 1024
	}
	return value
}
