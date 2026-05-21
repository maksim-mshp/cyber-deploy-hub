package cloudadapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts/commands"
)

type OpenStackProvider struct {
	client             *openstack.Client
	cfg                config.CloudConfig
	securityGroupIDs   []string
	deployTimeout      time.Duration
	pollInterval       time.Duration
	deletePollInterval time.Duration
}

func NewOpenStackProvider(client *openstack.Client, cfg config.CloudConfig) *OpenStackProvider {
	return &OpenStackProvider{
		client:             client,
		cfg:                cfg,
		securityGroupIDs:   splitCSV(cfg.SecurityGroupIDs),
		deployTimeout:      nonZeroDuration(cfg.DeployTimeout, 20*time.Minute),
		pollInterval:       nonZeroDuration(cfg.PollInterval, 5*time.Second),
		deletePollInterval: nonZeroDuration(cfg.DeletePollInterval, 3*time.Second),
	}
}

func (p *OpenStackProvider) Deploy(ctx context.Context, req DeployRequest) (DeployResult, error) {
	if p.client == nil || !p.client.Configured() {
		return DeployResult{}, errors.New("openstack credentials are not configured")
	}
	if strings.TrimSpace(p.cfg.PrivateNetworkID) == "" {
		return DeployResult{}, errors.New("CLOUD_PRIVATE_NETWORK_ID is required")
	}
	if strings.TrimSpace(p.cfg.PrivateSubnetID) == "" {
		return DeployResult{}, errors.New("CLOUD_PRIVATE_SUBNET_ID is required")
	}

	deployCtx, cancel := context.WithTimeout(ctx, p.deployTimeout)
	defer cancel()

	services, err := p.client.Services(deployCtx)
	if err != nil {
		return DeployResult{}, err
	}

	result := DeployResult{KeyPairName: keyPairName(req.LabRunID)}
	keyPair, err := keypairs.Create(deployCtx, services.Compute, keypairs.CreateOpts{Name: result.KeyPairName}).Extract()
	if err != nil {
		return result, &DeployError{Result: result, Err: fmt.Errorf("create keypair: %w", err)}
	}
	if strings.TrimSpace(keyPair.PrivateKey) == "" {
		return result, &DeployError{Result: result, Err: errors.New("nova did not return generated private key")}
	}
	result.PrivateKey = []byte(keyPair.PrivateKey)

	for _, blueprint := range req.Instances {
		instance, err := p.deployInstance(deployCtx, services, req, blueprint, result.KeyPairName)
		result.Instances = appendOrReplaceInstance(result.Instances, instance)
		if err != nil {
			return result, &DeployError{Result: result, Err: err}
		}
	}
	return result, nil
}

func (p *OpenStackProvider) Cleanup(ctx context.Context, deployment Deployment) error {
	if p.client == nil || !p.client.Configured() {
		return errors.New("openstack credentials are not configured")
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, p.deployTimeout)
	defer cancel()

	services, err := p.client.Services(cleanupCtx)
	if err != nil {
		return err
	}

	var cleanupErr error
	for i := len(deployment.Instances) - 1; i >= 0; i-- {
		instance := deployment.Instances[i]
		if instance.ServerID != "" {
			if err := deleteServer(cleanupCtx, services.Compute, instance.ServerID, p.deletePollInterval); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete server %s: %w", instance.ServerID, err))
			}
		}
		if instance.VolumeID != "" {
			if err := deleteVolume(cleanupCtx, services.Block, instance.VolumeID, p.deletePollInterval); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete volume %s: %w", instance.VolumeID, err))
			}
		}
		if instance.PortID != "" {
			if err := deletePort(cleanupCtx, services.Network, instance.PortID); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete port %s: %w", instance.PortID, err))
			}
		}
	}
	if deployment.KeyPairName != "" {
		if err := keypairs.Delete(cleanupCtx, services.Compute, deployment.KeyPairName, nil).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete keypair %s: %w", deployment.KeyPairName, err))
		}
	}
	return cleanupErr
}

func (p *OpenStackProvider) deployInstance(ctx context.Context, services *openstack.ServiceClients, req DeployRequest, blueprint commands.VMBlueprint, keyName string) (Instance, error) {
	instance := Instance{
		Name:     blueprint.Name,
		ImageID:  blueprint.ImageID,
		FlavorID: blueprint.FlavorID,
		FixedIP:  blueprint.FixedIP,
		DiskGiB:  blueprint.DiskGiB,
		State:    "CREATING",
	}

	if blueprint.FixedIP != "" {
		occupied, err := p.fixedIPOccupied(ctx, services.Network, blueprint.FixedIP)
		if err != nil {
			return instance, fmt.Errorf("check fixed ip %s: %w", blueprint.FixedIP, err)
		}
		if occupied {
			return instance, fmt.Errorf("fixed ip %s is already in use", blueprint.FixedIP)
		}
	}

	port, err := p.createPort(ctx, services.Network, req, blueprint)
	if err != nil {
		return instance, fmt.Errorf("create port for %s: %w", blueprint.Name, err)
	}
	instance.PortID = port.ID
	if instance.FixedIP == "" {
		instance.FixedIP = firstPortIPAddress(port.FixedIPs)
	}

	server, err := p.createServer(ctx, services.Compute, req, blueprint, keyName, port.ID)
	if err != nil {
		return instance, fmt.Errorf("create server %s: %w", blueprint.Name, err)
	}
	instance.ServerID = server.ID

	server, err = waitServerActive(ctx, services.Compute, server.ID, p.pollInterval)
	if err != nil {
		instance.State = "ERROR"
		return instance, fmt.Errorf("wait server %s active: %w", blueprint.Name, err)
	}
	instance.State = server.Status
	instance.VolumeID = firstAttachedVolumeID(server)
	return instance, nil
}

func (p *OpenStackProvider) fixedIPOccupied(ctx context.Context, network *gophercloud.ServiceClient, fixedIP string) (bool, error) {
	page, err := ports.List(network, ports.ListOpts{
		NetworkID: p.cfg.PrivateNetworkID,
		FixedIPs:  []ports.FixedIPOpts{{IPAddress: fixedIP}},
	}).AllPages(ctx)
	if err != nil {
		return false, err
	}
	existing, err := ports.ExtractPorts(page)
	if err != nil {
		return false, err
	}
	return len(existing) > 0, nil
}

func (p *OpenStackProvider) createPort(ctx context.Context, network *gophercloud.ServiceClient, req DeployRequest, blueprint commands.VMBlueprint) (*ports.Port, error) {
	adminUp := true
	opts := ports.CreateOpts{
		NetworkID:    p.cfg.PrivateNetworkID,
		Name:         resourceName(req.LabRunID, blueprint.Name, "port"),
		AdminStateUp: &adminUp,
		ProjectID:    req.ProjectID,
	}
	if blueprint.FixedIP != "" {
		opts.FixedIPs = []ports.IP{{SubnetID: p.cfg.PrivateSubnetID, IPAddress: blueprint.FixedIP}}
	}
	if len(p.securityGroupIDs) > 0 {
		groups := append([]string(nil), p.securityGroupIDs...)
		opts.SecurityGroups = &groups
	}
	return ports.Create(ctx, network, opts).Extract()
}

func (p *OpenStackProvider) createServer(ctx context.Context, compute *gophercloud.ServiceClient, req DeployRequest, blueprint commands.VMBlueprint, keyName string, portID string) (*servers.Server, error) {
	createOpts := servers.CreateOpts{
		Name:      resourceName(req.LabRunID, blueprint.Name, "vm"),
		FlavorRef: blueprint.FlavorID,
		Networks:  []servers.Network{{Port: portID}},
		Metadata: map[string]string{
			"lab_run_id": req.LabRunID,
			"managed_by": "cyber-deploy-hub",
			"vm_role":    blueprint.Name,
		},
		BlockDevice: []servers.BlockDevice{{
			SourceType:          servers.SourceImage,
			DestinationType:     servers.DestinationVolume,
			UUID:                blueprint.ImageID,
			VolumeSize:          int(blueprint.DiskGiB),
			BootIndex:           0,
			DeleteOnTermination: true,
		}},
	}
	opts := keypairs.CreateOptsExt{
		CreateOptsBuilder: createOpts,
		KeyName:           keyName,
	}
	return servers.Create(ctx, compute, opts, nil).Extract()
}

func waitServerActive(ctx context.Context, compute *gophercloud.ServiceClient, serverID string, interval time.Duration) (*servers.Server, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		server, err := servers.Get(ctx, compute, serverID).Extract()
		if err != nil {
			return nil, err
		}
		switch strings.ToUpper(server.Status) {
		case "ACTIVE":
			return server, nil
		case "ERROR":
			return nil, fmt.Errorf("server entered ERROR state: %+v", server.Fault)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func deleteServer(ctx context.Context, compute *gophercloud.ServiceClient, serverID string, interval time.Duration) error {
	if err := servers.Delete(ctx, compute, serverID).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, err := servers.Get(ctx, compute, serverID).Extract()
		if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func deleteVolume(ctx context.Context, block *gophercloud.ServiceClient, volumeID string, interval time.Duration) error {
	if err := volumes.Delete(ctx, block, volumeID, volumes.DeleteOpts{Cascade: true}).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, err := volumes.Get(ctx, block, volumeID).Extract()
		if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func deletePort(ctx context.Context, network *gophercloud.ServiceClient, portID string) error {
	if err := ports.Delete(ctx, network, portID).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}
	return nil
}

func firstAttachedVolumeID(server *servers.Server) string {
	for _, volume := range server.AttachedVolumes {
		if volume.ID != "" {
			return volume.ID
		}
	}
	return ""
}

func firstPortIPAddress(ips []ports.IP) string {
	for _, ip := range ips {
		if ip.IPAddress != "" {
			return ip.IPAddress
		}
	}
	return ""
}

func keyPairName(labRunID string) string {
	return resourceName(labRunID, "ssh", "key")
}

func resourceName(labRunID string, name string, suffix string) string {
	id := strings.ReplaceAll(labRunID, "-", "")
	if len(id) > 12 {
		id = id[:12]
	}
	safeName := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	return fmt.Sprintf("cdh-%s-%s-%s", id, safeName, suffix)
}

func appendOrReplaceInstance(instances []Instance, instance Instance) []Instance {
	for i := range instances {
		if instances[i].Name == instance.Name {
			instances[i] = instance
			return instances
		}
	}
	return append(instances, instance)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func nonZeroDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

type UnavailableProvider struct {
	Reason string
}

func (p UnavailableProvider) Deploy(context.Context, DeployRequest) (DeployResult, error) {
	if p.Reason == "" {
		p.Reason = "cloud provider is unavailable"
	}
	return DeployResult{}, errors.New(p.Reason)
}

func (p UnavailableProvider) Cleanup(context.Context, Deployment) error {
	if p.Reason == "" {
		p.Reason = "cloud provider is unavailable"
	}
	return errors.New(p.Reason)
}
