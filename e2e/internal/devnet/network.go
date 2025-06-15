package devnet

import (
	"context"
	"fmt"
	"log/slog"

	dockernetwork "github.com/docker/docker/api/types/network"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
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

func (n *CYOANetwork) Create(ctx context.Context) error {
	n.log.Info("==> Creating CYOA network", "cidrPrefix", n.dn.Spec.CYOANetworkSpec.CIDRPrefix)

	// Get an available subnet for the CYOA network.
	subnetCIDR, err := n.dn.subnetAllocator.FindAvailableSubnet(ctx, n.dn.Spec.DeployID)
	if err != nil {
		return fmt.Errorf("failed to get available subnet: %w", err)
	}
	n.log.Info("--> Network subnet selected", "subnet", subnetCIDR)

	// Create the docker network.
	network, err := tcnetwork.New(ctx,
		tcnetwork.WithDriver("bridge"),
		tcnetwork.WithAttachable(),
		tcnetwork.WithLabels(n.dn.labels),
		tcnetwork.WithInternal(),
		tcnetwork.WithIPAM(&dockernetwork.IPAM{
			Config: []dockernetwork.IPAMConfig{
				{Subnet: subnetCIDR},
			},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	n.Name = network.Name
	n.SubnetCIDR = subnetCIDR

	n.log.Info("--> CYOA network created", "network", n.Name, "subnet", n.SubnetCIDR)

	return nil
}

type DefaultNetwork struct {
	dn  *Devnet
	log *slog.Logger

	Name       string
	SubnetCIDR string
}

func (n *DefaultNetwork) Create(ctx context.Context) error {
	n.log.Info("==> Creating default network")

	// Create a docker network.
	network, err := tcnetwork.New(ctx,
		tcnetwork.WithDriver("bridge"),
		tcnetwork.WithAttachable(),
		tcnetwork.WithLabels(n.dn.labels),
	)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	// Get the subnet CIDR from the network.
	// We need to inspect for this in case the subnet was allocated by docker instead of our subnet allocator.
	inspect, err := n.dn.dockerClient.NetworkInspect(ctx, network.Name, dockernetwork.InspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect network: %w", err)
	}
	if len(inspect.IPAM.Config) == 0 {
		return fmt.Errorf("network %s has no IPAM config", network.Name)
	}
	subnetCIDR := inspect.IPAM.Config[0].Subnet

	n.Name = network.Name
	n.SubnetCIDR = subnetCIDR

	n.log.Info("--> Network created", "network", n.Name, "subnet", n.SubnetCIDR)

	return nil
}
