package devnet

import (
	"context"
	"fmt"
	"log/slog"

	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
)

type DefaultNetwork struct {
	dn  *Devnet
	log *slog.Logger

	Name       string
	SubnetCIDR string
}

func (n *DefaultNetwork) dockerNetworkName() string {
	return n.dn.Spec.DeployID + "-default"
}

func (n *DefaultNetwork) Exists(ctx context.Context) (bool, error) {
	networkName := n.dockerNetworkName()
	networks, err := n.dn.dockerClient.NetworkList(ctx, dockernetwork.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", networkName)),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list networks: %w", err)
	}
	for _, network := range networks {
		if network.Name == networkName {
			return true, nil
		}
	}
	return false, nil
}

func (n *DefaultNetwork) CreateIfNotExists(ctx context.Context) (bool, error) {
	exists, err := n.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if default network exists: %w", err)
	}
	if exists {
		subnetCIDR, err := n.getSubnetCIDR(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get subnet CIDR: %w", err)
		}
		n.Name = n.dockerNetworkName()
		n.SubnetCIDR = subnetCIDR

		n.log.Info("--> Default network already exists", "network", n.Name)
		return true, nil
	}
	return false, n.Create(ctx)
}

func (n *DefaultNetwork) Create(ctx context.Context) error {
	n.log.Info("==> Creating default network", "labels", n.dn.labels)

	// Get an available subnet for the default network.
	subnetCIDR, err := n.dn.subnetAllocator.FindAvailableSubnet(ctx, n.dn.Spec.DeployID)
	if err != nil {
		return fmt.Errorf("failed to get available subnet: %w", err)
	}
	n.log.Info("--> Default network subnet selected", "subnet", subnetCIDR)

	// Create a docker network.
	//nolint:staticcheck // SA1019
	networkName := n.dockerNetworkName()
	req := testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:       networkName,
			Driver:     "bridge",
			Attachable: true,
			Labels:     n.dn.labels,
		},
	}
	//nolint:staticcheck // SA1019
	_, err = testcontainers.GenericNetwork(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	n.Name = networkName
	n.SubnetCIDR = subnetCIDR

	n.log.Info("--> Network created", "network", n.Name, "subnet", n.SubnetCIDR)

	return nil
}

func (n *DefaultNetwork) getSubnetCIDR(ctx context.Context) (string, error) {
	networkName := n.dockerNetworkName()
	inspect, err := n.dn.dockerClient.NetworkInspect(ctx, networkName, dockernetwork.InspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect network: %w", err)
	}
	if len(inspect.IPAM.Config) == 0 {
		return "", fmt.Errorf("network %s has no IPAM config", networkName)
	}
	return inspect.IPAM.Config[0].Subnet, nil
}
