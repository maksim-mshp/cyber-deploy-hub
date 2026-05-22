package openstack

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud/v2"
	gcopenstack "github.com/gophercloud/gophercloud/v2/openstack"
	blocklimits "github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/limits"
	computelimits "github.com/gophercloud/gophercloud/v2/openstack/compute/v2/limits"
	tokens3 "github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"

	"cyber-deploy-hub/internal/config"
)

type Client struct {
	cfg config.OpenStackConfig
}

type ServiceClients struct {
	Provider *gophercloud.ProviderClient
	Compute  *gophercloud.ServiceClient
	Image    *gophercloud.ServiceClient
	Network  *gophercloud.ServiceClient
	Block    *gophercloud.ServiceClient
}

type ProjectQuota struct {
	VCPUFree    int
	RAMMiBFree  int
	DiskGiBFree int64
}

type ProjectInfo struct {
	ID         string
	Name       string
	DomainID   string
	DomainName string
}

func NewClient(cfg config.OpenStackConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Configured() bool {
	return c.cfg.Configured()
}

func (c *Client) CredentialsConfigured() bool {
	return c.cfg.CredentialsConfigured()
}

func (c *Client) Check(ctx context.Context) error {
	services, err := c.Services(ctx)
	if err != nil {
		return err
	}
	if services.Compute == nil || services.Image == nil || services.Network == nil || services.Block == nil {
		return errors.New("openstack service catalog is incomplete")
	}
	return nil
}

func (c *Client) CurrentProject(ctx context.Context) (*ProjectInfo, error) {
	provider, err := c.Provider(ctx)
	if err != nil {
		return nil, err
	}
	result, ok := provider.GetAuthResult().(tokens3.CreateResult)
	if !ok {
		return nil, errors.New("keystone v3 auth result is unavailable")
	}
	project, err := result.ExtractProject()
	if err != nil {
		return nil, fmt.Errorf("extract scoped project: %w", err)
	}
	if project == nil || project.ID == "" {
		return nil, errors.New("keystone token is not project-scoped")
	}
	return &ProjectInfo{
		ID:         project.ID,
		Name:       project.Name,
		DomainID:   project.Domain.ID,
		DomainName: project.Domain.Name,
	}, nil
}

func (c *Client) ProjectQuota(ctx context.Context, projectID string) (*ProjectQuota, error) {
	services, err := c.Services(ctx)
	if err != nil {
		return nil, err
	}
	projectID = normalizeProjectID(projectID)

	compute, err := computelimits.Get(ctx, services.Compute, computelimits.GetOpts{TenantID: projectID}).Extract()
	if err != nil {
		return nil, fmt.Errorf("get compute limits: %w", err)
	}
	block, err := blocklimits.Get(ctx, services.Block).Extract()
	if err != nil {
		return nil, fmt.Errorf("get block storage limits: %w", err)
	}

	return &ProjectQuota{
		VCPUFree:    limitFree(compute.Absolute.MaxTotalCores, compute.Absolute.TotalCoresUsed),
		RAMMiBFree:  limitFree(compute.Absolute.MaxTotalRAMSize, compute.Absolute.TotalRAMUsed),
		DiskGiBFree: int64(limitFree(block.Absolute.MaxTotalVolumeGigabytes, block.Absolute.TotalGigabytesUsed)),
	}, nil
}

func (c *Client) Services(ctx context.Context) (*ServiceClients, error) {
	return c.services(ctx, c.cfg)
}

func (c *Client) ServicesForProject(ctx context.Context, projectID string) (*ServiceClients, error) {
	cfg, err := c.projectScopedConfig(projectID)
	if err != nil {
		return nil, err
	}
	return c.services(ctx, cfg)
}

func (c *Client) services(ctx context.Context, cfg config.OpenStackConfig) (*ServiceClients, error) {
	provider, err := c.provider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	endpointOpts := gophercloud.EndpointOpts{
		Region:       cfg.Region,
		Availability: gophercloud.AvailabilityPublic,
	}

	compute, err := gcopenstack.NewComputeV2(provider, endpointOpts)
	if err != nil {
		return nil, err
	}
	image, err := gcopenstack.NewImageV2(provider, endpointOpts)
	if err != nil {
		return nil, err
	}
	network, err := gcopenstack.NewNetworkV2(provider, endpointOpts)
	if err != nil {
		return nil, err
	}
	block, err := gcopenstack.NewBlockStorageV3(provider, endpointOpts)
	if err != nil {
		return nil, err
	}

	return &ServiceClients{
		Provider: provider,
		Compute:  compute,
		Image:    image,
		Network:  network,
		Block:    block,
	}, nil
}

func (c *Client) Provider(ctx context.Context) (*gophercloud.ProviderClient, error) {
	return c.provider(ctx, c.cfg)
}

func (c *Client) ProviderForProject(ctx context.Context, projectID string) (*gophercloud.ProviderClient, error) {
	cfg, err := c.projectScopedConfig(projectID)
	if err != nil {
		return nil, err
	}
	return c.provider(ctx, cfg)
}

func (c *Client) provider(ctx context.Context, cfg config.OpenStackConfig) (*gophercloud.ProviderClient, error) {
	if !cfg.Configured() {
		return nil, errors.New("openstack credentials are not configured")
	}
	cfg.ProjectID = normalizeProjectID(cfg.ProjectID)

	provider, err := gcopenstack.NewClient(cfg.AuthURL)
	if err != nil {
		return nil, err
	}
	provider.HTTPClient = http.Client{
		Timeout:   c.timeout(),
		Transport: c.transport(),
	}

	authOptions := gophercloud.AuthOptions{
		IdentityEndpoint: cfg.AuthURL,
		Username:         cfg.Username,
		Password:         cfg.Password,
		DomainName:       cfg.UserDomainName,
		AllowReauth:      cfg.AllowReauth,
		Scope:            authScope(cfg),
	}
	if cfg.ProjectID != "" {
		authOptions.TenantID = cfg.ProjectID
	} else {
		authOptions.TenantName = cfg.ProjectName
	}

	if err := gcopenstack.Authenticate(ctx, provider, authOptions); err != nil {
		return nil, err
	}
	return provider, nil
}

func (c *Client) projectScopedConfig(projectID string) (config.OpenStackConfig, error) {
	projectID = normalizeProjectID(projectID)
	if projectID == "" {
		return config.OpenStackConfig{}, errors.New("openstack project_id is required")
	}
	cfg := c.cfg
	cfg.ProjectID = projectID
	cfg.ProjectName = ""
	return cfg, nil
}

func authScope(cfg config.OpenStackConfig) *gophercloud.AuthScope {
	if cfg.ProjectID != "" {
		return &gophercloud.AuthScope{ProjectID: cfg.ProjectID}
	}
	return &gophercloud.AuthScope{
		ProjectName: cfg.ProjectName,
		DomainName:  cfg.ProjectDomainName,
	}
}

func (c *Client) timeout() time.Duration {
	if c.cfg.Timeout <= 0 {
		return 20 * time.Second
	}
	return c.cfg.Timeout
}

func (c *Client) transport() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- enabled only by explicit OS_INSECURE for isolated lab stands.
	}
	return transport
}

func normalizeProjectID(projectID string) string {
	trimmed := strings.TrimSpace(projectID)
	if _, err := uuid.Parse(trimmed); err != nil {
		return trimmed
	}
	return strings.ReplaceAll(trimmed, "-", "")
}

func limitFree(maximum int, used int) int {
	if maximum < 0 {
		return math.MaxInt / 2
	}
	free := maximum - used
	if free < 0 {
		return 0
	}
	return free
}
