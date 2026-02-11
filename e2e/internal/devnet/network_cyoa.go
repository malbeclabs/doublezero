package devnet

import (
	"context"
	"fmt"
	"log/slog"

	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
)

type CYOANetwork struct {
	dn  *Devnet
	log *slog.Logger

	Name       string
	SubnetCIDR string
}

type CYOANetworkSpec struct {
	CIDRPrefix int
}

func (s *CYOANetworkSpec) Validate() error {
	if s.CIDRPrefix <= 0 || s.CIDRPrefix > 32 {
		return fmt.Errorf("CIDRPrefix must be between 1 and 32")
	}
	return nil
}

func (n *CYOANetwork) dockerNetworkName() string {
	return n.dn.Spec.DeployID + "-cyoa"
}

func (n *CYOANetwork) Exists(ctx context.Context) (bool, error) {
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

func (n *CYOANetwork) CreateIfNotExists(ctx context.Context) (bool, error) {
	exists, err := n.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if CYOA network exists: %w", err)
	}
	if exists {
		subnetCIDR, err := n.getSubnetCIDR(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get subnet CIDR: %w", err)
		}
		n.Name = n.dockerNetworkName()
		n.SubnetCIDR = subnetCIDR

		n.log.Debug("--> CYOA network already exists", "network", n.Name)
		return true, nil
	}
	return false, n.Create(ctx)
}

func (n *CYOANetwork) Create(ctx context.Context) error {
	n.log.Debug("==> Creating CYOA network", "labels", n.dn.labels)

	// Get an available subnet for the CYOA network.
	subnetCIDR, err := n.dn.subnetAllocator.FindAvailableSubnet(ctx, n.dn.Spec.DeployID)
	if err != nil {
		return fmt.Errorf("failed to get available subnet: %w", err)
	}
	n.log.Debug("--> Network subnet selected", "subnet", subnetCIDR)

	// Create the docker network.
	// NOTE: We use the deprecated GenericNetworkRequest because the newer network.New doesn't
	// allow us to set the name of the network, and we want something we can find by name.
	//nolint:staticcheck // SA1019
	networkName := n.dockerNetworkName()
	req := testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:       networkName,
			Driver:     "bridge",
			Attachable: true,
			Labels:     n.dn.labels,
			Internal:   true,
			IPAM: &dockernetwork.IPAM{
				Config: []dockernetwork.IPAMConfig{
					{Subnet: subnetCIDR},
				},
			},
		},
	}
	//nolint:staticcheck // SA1019
	_, err = testcontainers.GenericNetwork(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	n.Name = networkName
	n.SubnetCIDR = subnetCIDR

	n.log.Debug("--> CYOA network created", "network", n.Name, "subnet", n.SubnetCIDR)

	return nil
}

func (n *CYOANetwork) getSubnetCIDR(ctx context.Context) (string, error) {
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
