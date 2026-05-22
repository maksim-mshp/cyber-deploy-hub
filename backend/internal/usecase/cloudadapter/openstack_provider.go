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
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"

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
	if p.client == nil {
		return DeployResult{}, errors.New("openstack credentials are not configured")
	}
	if strings.TrimSpace(p.cfg.PrivateSubnetID) == "" {
		return DeployResult{}, errors.New("CLOUD_PRIVATE_SUBNET_ID is required")
	}

	deployCtx, cancel := context.WithTimeout(ctx, p.deployTimeout)
	defer cancel()

	services, err := p.client.ServicesForProject(deployCtx, req.ProjectID)
	if err != nil {
		return DeployResult{}, err
	}

	result := DeployResult{KeyPairName: keyPairName(req.LabRunID)}
	labNetwork, err := p.ensureLabNetwork(deployCtx, services.Network, req)
	result.NetworkID = labNetwork.NetworkID
	result.SubnetID = labNetwork.SubnetID
	if err != nil {
		return result, &DeployError{Result: result, Err: fmt.Errorf("create lab network: %w", err)}
	}

	keyPair, err := createKeyPair(deployCtx, services.Compute, result.KeyPairName)
	if err != nil {
		return result, &DeployError{Result: result, Err: fmt.Errorf("create keypair: %w", err)}
	}
	if strings.TrimSpace(keyPair.PrivateKey) == "" {
		return result, &DeployError{Result: result, Err: errors.New("nova did not return generated private key")}
	}
	result.PrivateKey = []byte(keyPair.PrivateKey)

	for _, blueprint := range req.Instances {
		instance, err := p.deployInstance(deployCtx, services, req, blueprint, result.KeyPairName, labNetwork)
		result.Instances = appendOrReplaceInstance(result.Instances, instance)
		if err != nil {
			return result, &DeployError{Result: result, Err: err}
		}
	}
	return result, nil
}

func createKeyPair(ctx context.Context, compute *gophercloud.ServiceClient, name string) (*keypairs.KeyPair, error) {
	keyPair, err := keypairs.Create(ctx, compute, keypairs.CreateOpts{Name: name}).Extract()
	if err == nil {
		return keyPair, nil
	}
	if !gophercloud.ResponseCodeIs(err, http.StatusConflict) {
		return nil, err
	}
	if deleteErr := keypairs.Delete(ctx, compute, name, nil).ExtractErr(); deleteErr != nil && !gophercloud.ResponseCodeIs(deleteErr, http.StatusNotFound) {
		return nil, deleteErr
	}
	return keypairs.Create(ctx, compute, keypairs.CreateOpts{Name: name}).Extract()
}

func (p *OpenStackProvider) Cleanup(ctx context.Context, deployment Deployment) error {
	if p.client == nil {
		return errors.New("openstack credentials are not configured")
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, p.deployTimeout)
	defer cancel()

	services, err := p.client.ServicesForProject(cleanupCtx, deployment.ProjectID)
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
	if deployment.SubnetID != "" {
		if err := deleteSubnet(cleanupCtx, services.Network, deployment.SubnetID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete subnet %s: %w", deployment.SubnetID, err))
		}
	}
	if deployment.NetworkID != "" {
		if err := deleteNetwork(cleanupCtx, services.Network, deployment.NetworkID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete network %s: %w", deployment.NetworkID, err))
		}
	}
	return cleanupErr
}

type labNetwork struct {
	NetworkID string
	SubnetID  string
}

func (p *OpenStackProvider) ensureLabNetwork(ctx context.Context, network *gophercloud.ServiceClient, req DeployRequest) (labNetwork, error) {
	template, err := subnets.Get(ctx, network, p.cfg.PrivateSubnetID).Extract()
	if err != nil {
		return labNetwork{}, fmt.Errorf("load template subnet %s: %w", p.cfg.PrivateSubnetID, err)
	}
	if template == nil || strings.TrimSpace(template.CIDR) == "" {
		return labNetwork{}, fmt.Errorf("template subnet %s has empty CIDR", p.cfg.PrivateSubnetID)
	}

	networkName := resourceName(req.LabRunID, "lab", "net")
	createdNetwork, err := findNetworkByName(ctx, network, networkName)
	if err != nil {
		return labNetwork{}, err
	}
	if createdNetwork == nil {
		adminUp := true
		createdNetwork, err = networks.Create(ctx, network, networks.CreateOpts{
			Name:         networkName,
			Description:  "cyber-deploy-hub lab network " + req.LabRunID,
			AdminStateUp: &adminUp,
		}).Extract()
		if err != nil {
			return labNetwork{}, err
		}
	}

	subnetName := resourceName(req.LabRunID, "lab", "subnet")
	createdSubnet, err := findSubnetByName(ctx, network, createdNetwork.ID, subnetName)
	if err != nil {
		return labNetwork{}, err
	}
	if createdSubnet == nil {
		enableDHCP := template.EnableDHCP
		gatewayIP := template.GatewayIP
		ipVersion := gophercloud.IPVersion(template.IPVersion)
		if ipVersion == 0 {
			ipVersion = gophercloud.IPv4
		}
		createdSubnet, err = subnets.Create(ctx, network, subnets.CreateOpts{
			NetworkID:       createdNetwork.ID,
			Name:            subnetName,
			Description:     "cyber-deploy-hub lab subnet " + req.LabRunID,
			CIDR:            template.CIDR,
			IPVersion:       ipVersion,
			GatewayIP:       &gatewayIP,
			EnableDHCP:      &enableDHCP,
			DNSNameservers:  append([]string(nil), template.DNSNameservers...),
			AllocationPools: append([]subnets.AllocationPool(nil), template.AllocationPools...),
			HostRoutes:      append([]subnets.HostRoute(nil), template.HostRoutes...),
		}).Extract()
		if err != nil {
			return labNetwork{NetworkID: createdNetwork.ID}, err
		}
	}

	return labNetwork{NetworkID: createdNetwork.ID, SubnetID: createdSubnet.ID}, nil
}

func findNetworkByName(ctx context.Context, network *gophercloud.ServiceClient, name string) (*networks.Network, error) {
	page, err := networks.List(network, networks.ListOpts{Name: name}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list networks by name %s: %w", name, err)
	}
	items, err := networks.ExtractNetworks(page)
	if err != nil {
		return nil, fmt.Errorf("extract networks by name %s: %w", name, err)
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i], nil
		}
	}
	return nil, nil
}

func findSubnetByName(ctx context.Context, network *gophercloud.ServiceClient, networkID string, name string) (*subnets.Subnet, error) {
	page, err := subnets.List(network, subnets.ListOpts{NetworkID: networkID, Name: name}).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subnets by name %s: %w", name, err)
	}
	items, err := subnets.ExtractSubnets(page)
	if err != nil {
		return nil, fmt.Errorf("extract subnets by name %s: %w", name, err)
	}
	for i := range items {
		if items[i].Name == name && items[i].NetworkID == networkID {
			return &items[i], nil
		}
	}
	return nil, nil
}

func (p *OpenStackProvider) deployInstance(ctx context.Context, services *openstack.ServiceClients, req DeployRequest, blueprint commands.VMBlueprint, keyName string, labNetwork labNetwork) (Instance, error) {
	instance := Instance{
		Name:     blueprint.Name,
		ImageID:  blueprint.ImageID,
		FlavorID: blueprint.FlavorID,
		FixedIP:  blueprint.FixedIP,
		DiskGiB:  blueprint.DiskGiB,
		State:    "CREATING",
	}

	if blueprint.FixedIP != "" {
		occupied, err := fixedIPOccupied(ctx, services.Network, labNetwork.NetworkID, blueprint.FixedIP)
		if err != nil {
			return instance, fmt.Errorf("check fixed ip %s: %w", blueprint.FixedIP, err)
		}
		if occupied {
			return instance, fmt.Errorf("fixed ip %s is already in use", blueprint.FixedIP)
		}
	}

	port, err := p.createPort(ctx, services.Network, req, blueprint, labNetwork)
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

func fixedIPOccupied(ctx context.Context, network *gophercloud.ServiceClient, networkID string, fixedIP string) (bool, error) {
	page, err := ports.List(network, ports.ListOpts{
		NetworkID: networkID,
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

func (p *OpenStackProvider) createPort(ctx context.Context, network *gophercloud.ServiceClient, req DeployRequest, blueprint commands.VMBlueprint, labNetwork labNetwork) (*ports.Port, error) {
	adminUp := true
	opts := ports.CreateOpts{
		NetworkID:    labNetwork.NetworkID,
		Name:         resourceName(req.LabRunID, blueprint.Name, "port"),
		AdminStateUp: &adminUp,
	}
	if blueprint.FixedIP != "" {
		opts.FixedIPs = []ports.IP{{SubnetID: labNetwork.SubnetID, IPAddress: blueprint.FixedIP}}
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

func deleteSubnet(ctx context.Context, network *gophercloud.ServiceClient, subnetID string) error {
	if err := subnets.Delete(ctx, network, subnetID).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}
	return nil
}

func deleteNetwork(ctx context.Context, network *gophercloud.ServiceClient, networkID string) error {
	if err := networks.Delete(ctx, network, networkID).ExtractErr(); err != nil && !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
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
