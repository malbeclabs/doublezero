package devnet

import (
	"context"
	"fmt"
	"log/slog"

	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
)

// BehindNATNetwork is a private network for clients behind a NAT gateway.
// Clients on this network have private IPs and reach the CYOA network through a NAT gateway.
type BehindNATNetwork struct {
	dn  *Devnet
	log *slog.Logger

	// Code is a unique identifier for this NAT network (e.g., "nat1", "nat2").
	Code string

	Name       string
	SubnetCIDR string
}

func NewBehindNATNetwork(dn *Devnet, log *slog.Logger, code string) *BehindNATNetwork {
	return &BehindNATNetwork{
		dn:   dn,
		log:  log.With("component", "behind-nat-network", "code", code),
		Code: code,
	}
}

func (n *BehindNATNetwork) dockerNetworkName() string {
	return n.dn.Spec.DeployID + "-behind-nat-" + n.Code
}

func (n *BehindNATNetwork) Exists(ctx context.Context) (bool, error) {
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

func (n *BehindNATNetwork) CreateIfNotExists(ctx context.Context) (bool, error) {
	exists, err := n.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if behind-NAT network exists: %w", err)
	}
	if exists {
		subnetCIDR, err := n.getSubnetCIDR(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get subnet CIDR: %w", err)
		}
		n.Name = n.dockerNetworkName()
		n.SubnetCIDR = subnetCIDR

		n.log.Info("--> Behind-NAT network already exists", "network", n.Name)
		return true, nil
	}
	return false, n.Create(ctx)
}

func (n *BehindNATNetwork) Create(ctx context.Context) error {
	n.log.Info("==> Creating behind-NAT network", "labels", n.dn.labels)

	// Use a fixed private subnet for the behind-NAT network.
	// This avoids conflicts with the CYOA network which uses 9.x.x.x.
	// We use 10.255.x.0/24 where x is derived from the code to allow multiple NAT networks.
	subnetCIDR := n.deriveSubnetCIDR()
	n.log.Info("--> Network subnet selected", "subnet", subnetCIDR)

	networkName := n.dockerNetworkName()

	// Use Docker API directly to set driver options.
	// Internal must be false to allow traffic to be forwarded through the NAT gateway.
	// Disable Docker's automatic masquerade to use our custom NAT rules.
	_, err := n.dn.dockerClient.NetworkCreate(ctx, networkName, dockernetwork.CreateOptions{
		Driver:     "bridge",
		Attachable: true,
		Labels:     n.dn.labels,
		Internal:   false,
		IPAM: &dockernetwork.IPAM{
			Config: []dockernetwork.IPAMConfig{
				{Subnet: subnetCIDR},
			},
		},
		Options: map[string]string{
			"com.docker.network.bridge.enable_ip_masquerade": "false",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	n.Name = networkName
	n.SubnetCIDR = subnetCIDR

	n.log.Info("--> Behind-NAT network created", "network", n.Name, "subnet", n.SubnetCIDR)

	return nil
}

func (n *BehindNATNetwork) getSubnetCIDR(ctx context.Context) (string, error) {
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

// deriveSubnetCIDR generates a fixed private subnet for this behind-NAT network.
// Uses 10.255.x.0/24 where x is derived from the code to allow multiple NAT networks.
func (n *BehindNATNetwork) deriveSubnetCIDR() string {
	// Hash the code to get a deterministic third octet (1-254).
	var thirdOctet uint8 = 1
	for _, c := range n.Code {
		thirdOctet = uint8((int(thirdOctet) + int(c)) % 254)
	}
	if thirdOctet == 0 {
		thirdOctet = 1
	}
	return fmt.Sprintf("10.255.%d.0/24", thirdOctet)
}
