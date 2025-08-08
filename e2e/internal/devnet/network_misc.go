package devnet

import (
	"context"
	"fmt"
	"log/slog"

	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/testcontainers/testcontainers-go"
)

type MiscNetwork struct {
	dn  *Devnet
	log *slog.Logger

	Name string
}

func NewMiscNetwork(dn *Devnet, log *slog.Logger, suffix string) *MiscNetwork {
	return &MiscNetwork{
		dn:   dn,
		log:  log,
		Name: dn.Spec.DeployID + "-" + suffix,
	}
}
func (n *MiscNetwork) Exists(ctx context.Context) (bool, error) {
	networks, err := n.dn.dockerClient.NetworkList(ctx, dockernetwork.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", n.Name)),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list networks: %w", err)
	}
	for _, network := range networks {
		if network.Name == n.Name {
			return true, nil
		}
	}
	return false, nil
}

func (n *MiscNetwork) CreateIfNotExists(ctx context.Context) (bool, error) {
	exists, err := n.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if misc network exists: %w", err)
	}
	if exists {
		n.log.Info("--> Default network already exists", "network", n.Name)
		return true, nil
	}
	return false, n.Create(ctx)
}

func (n *MiscNetwork) Create(ctx context.Context) error {
	n.log.Info("==> Creating misc network", "labels", n.dn.labels)

	// Create a docker network.
	//nolint:staticcheck // SA1019
	req := testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:       n.Name,
			Driver:     "bridge",
			Attachable: false,
			Labels:     n.dn.labels,
		},
	}
	//nolint:staticcheck // SA1019
	_, err := testcontainers.GenericNetwork(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	n.log.Info("--> Network created", "network", n.Name)
	return nil
}
