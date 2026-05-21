package openstack

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

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
	provider, err := c.Provider(ctx)
	if err != nil {
		return nil, err
	}

	endpointOpts := gophercloud.EndpointOpts{
		Region:       c.cfg.Region,
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
	if !c.Configured() {
		return nil, errors.New("openstack credentials are not configured")
	}

	provider, err := gcopenstack.NewClient(c.cfg.AuthURL)
	if err != nil {
		return nil, err
	}
	provider.HTTPClient = http.Client{
		Timeout:   c.timeout(),
		Transport: c.transport(),
	}

	authOptions := gophercloud.AuthOptions{
		IdentityEndpoint: c.cfg.AuthURL,
		Username:         c.cfg.Username,
		Password:         c.cfg.Password,
		DomainName:       c.cfg.UserDomainName,
		AllowReauth:      c.cfg.AllowReauth,
		Scope:            authScope(c.cfg),
	}
	if c.cfg.ProjectID != "" {
		authOptions.TenantID = c.cfg.ProjectID
	} else {
		authOptions.TenantName = c.cfg.ProjectName
	}

	if err := gcopenstack.Authenticate(ctx, provider, authOptions); err != nil {
		return nil, err
	}
	return provider, nil
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
