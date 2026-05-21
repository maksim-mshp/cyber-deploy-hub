package openstack

import (
	"context"
	"sort"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
)

type ImageOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
	DiskFormat string `json:"disk_format,omitempty"`
	MinDiskGiB int    `json:"min_disk_gib"`
	MinRAMMiB  int    `json:"min_ram_mib"`
	SizeGiB    int64  `json:"size_gib,omitempty"`
}

type FlavorOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	VCPUs     int    `json:"vcpus"`
	RAMMiB    int    `json:"ram_mib"`
	DiskGiB   int    `json:"disk_gib"`
	Ephemeral int    `json:"ephemeral_gib"`
	IsPublic  bool   `json:"is_public"`
}

func (c *Client) ListImages(ctx context.Context) ([]ImageOption, error) {
	services, err := c.Services(ctx)
	if err != nil {
		return nil, err
	}
	page, err := images.List(services.Image, images.ListOpts{
		Status: images.ImageStatusActive,
		Hidden: false,
		Sort:   "name:asc",
	}).AllPages(ctx)
	if err != nil {
		return nil, err
	}
	items, err := images.ExtractImages(page)
	if err != nil {
		return nil, err
	}
	result := make([]ImageOption, 0, len(items))
	for _, item := range items {
		result = append(result, ImageOption{
			ID:         item.ID,
			Name:       item.Name,
			Status:     string(item.Status),
			Visibility: string(item.Visibility),
			DiskFormat: item.DiskFormat,
			MinDiskGiB: item.MinDiskGigabytes,
			MinRAMMiB:  item.MinRAMMegabytes,
			SizeGiB:    bytesToGiB(item.SizeBytes),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (c *Client) ListFlavors(ctx context.Context) ([]FlavorOption, error) {
	services, err := c.Services(ctx)
	if err != nil {
		return nil, err
	}
	page, err := flavors.ListDetail(services.Compute, flavors.ListOpts{
		SortKey: "name",
		SortDir: "asc",
	}).AllPages(ctx)
	if err != nil {
		return nil, err
	}
	items, err := flavors.ExtractFlavors(page)
	if err != nil {
		return nil, err
	}
	result := make([]FlavorOption, 0, len(items))
	for _, item := range items {
		result = append(result, FlavorOption{
			ID:        item.ID,
			Name:      item.Name,
			VCPUs:     item.VCPUs,
			RAMMiB:    item.RAM,
			DiskGiB:   item.Disk,
			Ephemeral: item.Ephemeral,
			IsPublic:  item.IsPublic,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func bytesToGiB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	const gib = 1024 * 1024 * 1024
	return (value + gib - 1) / gib
}
