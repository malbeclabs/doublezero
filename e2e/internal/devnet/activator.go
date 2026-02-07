package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
)

type ActivatorSpec struct {
	ContainerImage    string
	OnchainAllocation *bool // nil = default (enabled), false = explicitly disabled
}

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool {
	return &b
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

// dockerContainerName returns the name of the deterministic activator container based on the
// deployID and component name.
func (a *Activator) dockerContainerName() string {
	return a.dn.Spec.DeployID + "-" + a.dockerContainerHostname()
}

func (a *Activator) dockerContainerHostname() string {
	return "activator"
}

// Exists checks if the activator container exists.
func (a *Activator) Exists(ctx context.Context) (bool, error) {
	containers, err := a.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", a.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+a.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the activator container if it's not already running.
func (a *Activator) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := a.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if activator exists: %w", err)
	}
	if exists {
		container, err := a.dn.dockerClient.ContainerInspect(ctx, a.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			a.log.Debug("--> Activator already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = a.setState(container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set activator state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = a.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start activator: %w", err)
		}

		// Set the component's state.
		err = a.setState(container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set activator state: %w", err)
		}

		return true, nil
	}

	return false, a.Start(ctx)
}

// Start creates and starts the activator container and attaches it to the default network.
func (a *Activator) Start(ctx context.Context) error {
	a.log.Debug("==> Starting activator", "image", a.dn.Spec.Activator.ContainerImage)

	env := map[string]string{
		"DZ_LEDGER_URL":                a.dn.Ledger.InternalRPCURL,
		"DZ_LEDGER_WS":                 a.dn.Ledger.InternalRPCWSURL,
		"DZ_SERVICEABILITY_PROGRAM_ID": a.dn.Manager.ServiceabilityProgramID,
	}
	// Only set DZ_ONCHAIN_ALLOCATION=false when explicitly disabled.
	// Otherwise let the entrypoint default to enabled (matching main's behavior).
	if a.dn.Spec.Activator.OnchainAllocation != nil && !*a.dn.Spec.Activator.OnchainAllocation {
		env["DZ_ONCHAIN_ALLOCATION"] = "false"
	}

	req := testcontainers.ContainerRequest{
		Image: a.dn.Spec.Activator.ContainerImage,
		Name:  a.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = a.dockerContainerHostname()
		},
		Env: env,
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      a.dn.Spec.Manager.ManagerKeypairPath,
				ContainerFilePath: containerDoublezeroKeypairPath,
			},
			{
				HostFilePath:      a.dn.Spec.Manager.ServiceabilityProgramKeypairPath,
				ContainerFilePath: containerSolanaKeypairPath,
			},
		},
		Networks: []string{a.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			a.dn.DefaultNetwork.Name: {"activator"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: dockercontainer.Resources{
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

	// Set the component's state.
	err = a.setState(container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set activator state: %w", err)
	}

	a.log.Debug("--> Activator started", "container", a.ContainerID)
	return nil
}

func (a *Activator) setState(containerID string) error {
	a.ContainerID = shortContainerID(containerID)
	return nil
}

// GetContainerState returns the current state of the activator container.
func (a *Activator) GetContainerState(ctx context.Context) (*dockercontainer.State, error) {
	container, err := a.dn.dockerClient.ContainerInspect(ctx, a.dockerContainerName())
	if err != nil {
		return nil, fmt.Errorf("failed to inspect activator container: %w", err)
	}
	return container.State, nil
}
