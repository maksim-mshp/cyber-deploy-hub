package capacity

import (
	"context"
	"encoding/json"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/ki"
)

type StatProvider interface {
	Current(ctx context.Context) (Snapshot, error)
}

type StaticProvider struct {
	snapshot Snapshot
}

func NewStaticProvider(cfg config.CapacityConfig) *StaticProvider {
	return &StaticProvider{
		snapshot: Snapshot{
			Source:          "static",
			VCPUsTotal:      cfg.DemoVCPUs,
			VCPUsFree:       cfg.DemoVCPUsFree,
			RAMMiBTotal:     cfg.DemoRAMMiB,
			RAMMiBFree:      cfg.DemoRAMFreeMiB,
			StorageGiBTotal: cfg.DemoStorageGiB,
			StorageGiBUsed:  cfg.DemoStorageUsedGiB,
			RawPayload:      json.RawMessage(`{}`),
		},
	}
}

func (p *StaticProvider) Current(context.Context) (Snapshot, error) {
	return p.snapshot, nil
}

type KIProvider struct {
	client interface {
		ClusterStat(ctx context.Context) (*ki.ClusterStat, error)
	}
}

func NewKIProvider(client interface {
	ClusterStat(ctx context.Context) (*ki.ClusterStat, error)
}) *KIProvider {
	return &KIProvider{client: client}
}

func (p *KIProvider) Current(ctx context.Context) (Snapshot, error) {
	stat, err := p.client.ClusterStat(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := json.Marshal(stat)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Source:          "ki",
		VCPUsTotal:      stat.Compute.VCPUs,
		VCPUsFree:       stat.Compute.VCPUsFree,
		RAMMiBTotal:     memoryToMiB(stat.Compute.VMMemCapacity),
		RAMMiBFree:      memoryToMiB(stat.Compute.VMMemFree),
		StorageGiBTotal: storageToGiB(stat.Compute.BlockCapacity),
		StorageGiBUsed:  storageToGiB(stat.Compute.BlockUsage),
		RawPayload:      raw,
	}, nil
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
