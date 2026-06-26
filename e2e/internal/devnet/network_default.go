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

		n.log.Debug("--> Default network already exists", "network", n.Name)
		return true, nil
	}
	return false, n.Create(ctx)
}

func (n *DefaultNetwork) Create(ctx context.Context) error {
	n.log.Debug("==> Creating default network", "labels", n.dn.labels)

	networkName := n.dockerNetworkName()

	// Allocate a collision-safe subnet from the 10.0.0.0/8 range via the dedicated default-network
	// allocator (which skips subnets already in use by existing docker networks and retries on
	// overlap) instead of a fixed deterministic hash, which fails hard with "Pool overlaps" when
	// the chosen CIDR is already taken by a leftover or concurrently-created network. This range is
	// kept separate from the CYOA network (9.128.0.0/9) to avoid conflicts in tests that detect
	// interfaces by IP range.
	subnetCIDR, err := createNetworkWithSubnet(ctx, n.dn.defaultNetworkAllocator, n.dn.Spec.DeployID, func(subnetCIDR string) error {
		//nolint:staticcheck // SA1019
		req := testcontainers.GenericNetworkRequest{
			NetworkRequest: testcontainers.NetworkRequest{
				Name:       networkName,
				Driver:     "bridge",
				Attachable: true,
				Labels:     n.dn.labels,
				IPAM: &dockernetwork.IPAM{
					Config: []dockernetwork.IPAMConfig{
						{Subnet: subnetCIDR},
					},
				},
			},
		}
		//nolint:staticcheck // SA1019
		_, err := testcontainers.GenericNetwork(ctx, req)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	n.Name = networkName
	n.SubnetCIDR = subnetCIDR

	n.log.Debug("--> Network created", "network", n.Name, "subnet", n.SubnetCIDR)

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
