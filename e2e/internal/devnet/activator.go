package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
)

type ActivatorSpec struct {
	ContainerImage string
}

func (s *ActivatorSpec) Validate() error {
	// If the container image is not set, use the DZ_ACTIVATOR_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_ACTIVATOR_IMAGE")
	}

	return nil
}

type Activator struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID string
}

func (a *Activator) Start(ctx context.Context) error {
	a.log.Info("==> Starting activator", "image", a.dn.Spec.Activator.ContainerImage)

	containerManagerKeypairPath := "/etc/doublezero/activator/dz-manager-keypair.json"
	req := testcontainers.ContainerRequest{
		Image: a.dn.Spec.Activator.ContainerImage,
		Name:  a.dn.Spec.DeployID + "-activator",
		Env: map[string]string{
			"DZ_LEDGER_URL":           a.dn.Ledger.InternalURL,
			"DZ_LEDGER_WS":            a.dn.Ledger.InternalWSURL,
			"DZ_PROGRAM_ID":           a.dn.Ledger.ProgramID,
			"DZ_MANAGER_KEYPAIR_PATH": containerManagerKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      a.dn.Spec.Manager.KeypairPath,
				ContainerFilePath: containerManagerKeypairPath,
			},
		},
		Networks: []string{a.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			a.dn.DefaultNetwork.Name: {"activator"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: a.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(a.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start activator: %w", err)
	}

	a.ContainerID = shortContainerID(container.GetContainerID())

	a.log.Info("--> Activator started", "container", a.ContainerID)
	return nil
}
