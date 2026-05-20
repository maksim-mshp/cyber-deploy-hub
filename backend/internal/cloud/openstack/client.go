package openstack

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	gcopenstack "github.com/gophercloud/gophercloud/v2/openstack"

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
		TenantID:         c.cfg.ProjectID,
		TenantName:       c.cfg.ProjectName,
		AllowReauth:      c.cfg.AllowReauth,
		Scope: &gophercloud.AuthScope{
			ProjectID:   c.cfg.ProjectID,
			ProjectName: c.cfg.ProjectName,
			DomainName:  c.cfg.ProjectDomainName,
		},
	}

	if err := gcopenstack.Authenticate(ctx, provider, authOptions); err != nil {
		return nil, err
	}
	return provider, nil
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
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return transport
}
