package ki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyber-deploy-hub/internal/config"
)

type Client struct {
	cfg        config.KIConfig
	httpClient *http.Client
}

type ClusterStat struct {
	DateTime string `json:"datetime"`
	Compute  struct {
		CPUUsage      float64 `json:"cpu_usage"`
		VCPUs         int     `json:"vcpus"`
		VCPUsFree     int     `json:"vcpus_free"`
		VMMemReserved int64   `json:"vm_mem_reserved"`
		VMMemCapacity int64   `json:"vm_mem_capacity"`
		VMMemFree     int64   `json:"vm_mem_free"`
		BlockCapacity int64   `json:"block_capacity"`
		BlockUsage    int64   `json:"block_usage"`
		CPUAllocRatio float64 `json:"cpu_allocation_ratio"`
		RAMAllocRatio float64 `json:"ram_allocation_ratio"`
	} `json:"compute"`
	Servers struct {
		Count      int `json:"count"`
		Running    int `json:"running"`
		Error      int `json:"error"`
		InProgress int `json:"in_progress"`
		Stopped    int `json:"stopped"`
	} `json:"servers"`
}

func NewClient(cfg config.KIConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Configured() bool {
	return strings.TrimSpace(c.cfg.APIBaseURL) != "" && strings.TrimSpace(c.cfg.AuthToken) != ""
}

func (c *Client) ClusterStat(ctx context.Context) (*ClusterStat, error) {
	if !c.Configured() {
		return nil, errors.New("ki api client is not configured")
	}

	url := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/api/v2/compute/cluster/stat/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-auth-token", c.cfg.AuthToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ki api returned status %d", resp.StatusCode)
	}

	var stat ClusterStat
	if err := json.NewDecoder(resp.Body).Decode(&stat); err != nil {
		return nil, err
	}
	return &stat, nil
}
