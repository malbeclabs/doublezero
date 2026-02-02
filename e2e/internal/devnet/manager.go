package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	serviceabilityProgramContainerKeypairPath = "/etc/doublezero/manager/dz-program-keypair.json"
	telemetryProgramContainerKeypairPath      = "/etc/doublezero/manager/dz-telemetry-program-keypair.json"
)

type ManagerSpec struct {
	ContainerImage                   string
	ManagerKeypairPath               string
	ServiceabilityProgramKeypairPath string
	TelemetryProgramKeypairPath      string

	// ServiceabilityProgramID, when set, overrides the program ID that would
	// normally be derived from the ServiceabilityProgramKeypairPath. This is
	// used when testing against a cloned mainnet program where the program ID
	// differs from the local keypair.
	ServiceabilityProgramID string
}

func (s *ManagerSpec) Validate() error {
	// If the container image is not set, use the DZ_MANAGER_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_MANAGER_IMAGE")
	}

	// Check required fields.
	if s.ManagerKeypairPath == "" {
		return fmt.Errorf("manager keypair path is required")
	}

	if s.ServiceabilityProgramKeypairPath == "" {
		return fmt.Errorf("serviceability program keypair path is required")
	}

	if s.TelemetryProgramKeypairPath == "" {
		return fmt.Errorf("telemetry program keypair path is required")
	}

	// Check that the manager keypair file exists and is an absolute path.
	if _, err := os.Stat(s.ManagerKeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("manager keypair path does not exist: %s", s.ManagerKeypairPath)
	}
	if !filepath.IsAbs(s.ManagerKeypairPath) {
		return fmt.Errorf("manager keypair path must be an absolute path: %s", s.ManagerKeypairPath)
	}

	// Check that the serviceability program keypair file exists and is an absolute path.
	if _, err := os.Stat(s.ServiceabilityProgramKeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("serviceability program keypair path does not exist: %s", s.ServiceabilityProgramKeypairPath)
	}
	if !filepath.IsAbs(s.ServiceabilityProgramKeypairPath) {
		return fmt.Errorf("serviceability program keypair path must be an absolute path: %s", s.ServiceabilityProgramKeypairPath)
	}

	// Check that the telemetry program keypair file exists and is an absolute path.
	if _, err := os.Stat(s.TelemetryProgramKeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("telemetry program keypair path does not exist: %s", s.TelemetryProgramKeypairPath)
	}
	if !filepath.IsAbs(s.TelemetryProgramKeypairPath) {
		return fmt.Errorf("telemetry program keypair path must be an absolute path: %s", s.TelemetryProgramKeypairPath)
	}

	return nil
}

type Manager struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID             string
	Pubkey                  string
	ServiceabilityProgramID string
	TelemetryProgramID      string
}

// dockerContainerName returns the name of the deterministic manager container based on the
// deployID and component name.
func (m *Manager) dockerContainerName() string {
	return m.dn.Spec.DeployID + "-" + m.dockerContainerHostname()
}

func (m *Manager) dockerContainerHostname() string {
	return "manager"
}

// Exists checks if the ledger container exists.
func (m *Manager) Exists(ctx context.Context) (bool, error) {
	containers, err := m.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", m.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+m.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the ledger container if it's not already running.
func (m *Manager) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := m.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if manager exists: %w", err)
	}
	if exists {
		container, err := m.dn.dockerClient.ContainerInspect(ctx, m.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			m.log.Info("--> Manager already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = m.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set manager state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		m.log.Info("--> Starting manager", "container", container.ID, "serviceabilityProgramID", m.ServiceabilityProgramID, "telemetryProgramID", m.TelemetryProgramID)
		err = m.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start manager: %w", err)
		}

		// Set the component's state.
		err = m.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set manager state: %w", err)
		}

		return true, nil
	}

	return false, m.Start(ctx)
}

// Start creates and starts the manager container and attaches it to the default network.
func (m *Manager) Start(ctx context.Context) error {
	m.log.Info("==> Starting manager", "image", m.dn.Spec.Manager.ContainerImage)

	req := testcontainers.ContainerRequest{
		Image: m.dn.Spec.Manager.ContainerImage,
		Name:  m.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = m.dockerContainerHostname()
		},
		WaitingFor: wait.ForLog("Config initialized").WithStartupTimeout(30 * time.Second),
		Env: map[string]string{
			"DZ_LEDGER_URL":                          m.dn.Ledger.InternalRPCURL,
			"DZ_LEDGER_WS":                           m.dn.Ledger.InternalRPCWSURL,
			"DZ_SERVICEABILITY_PROGRAM_KEYPAIR_PATH": serviceabilityProgramContainerKeypairPath,
			"DZ_TELEMETRY_PROGRAM_KEYPAIR_PATH":      telemetryProgramContainerKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      m.dn.Spec.Manager.ManagerKeypairPath,
				ContainerFilePath: containerDoublezeroKeypairPath,
			},
			{
				HostFilePath:      m.dn.Spec.Manager.ManagerKeypairPath,
				ContainerFilePath: containerSolanaKeypairPath,
			},
			{
				HostFilePath:      m.dn.Spec.Manager.ServiceabilityProgramKeypairPath,
				ContainerFilePath: serviceabilityProgramContainerKeypairPath,
			},
			{
				HostFilePath:      m.dn.Spec.Manager.TelemetryProgramKeypairPath,
				ContainerFilePath: telemetryProgramContainerKeypairPath,
			},
		},
		Networks: []string{m.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			m.dn.DefaultNetwork.Name: {"manager"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: m.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(m.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	// Set the component's state.
	err = m.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set manager state: %w", err)
	}

	m.log.Info("--> Manager started", "container", m.ContainerID, "pubkey", m.Pubkey)
	return nil
}

func (m *Manager) setState(ctx context.Context, containerID string) error {
	m.ContainerID = shortContainerID(containerID)

	// Get the manager pubkey from the manager keypair.
	output, err := m.Exec(ctx, []string{"solana", "address"}, docker.NoPrintOnError())
	if err != nil {
		return fmt.Errorf("failed to get manager pubkey: %v", err)
	}
	m.Pubkey = strings.TrimSpace(string(output))

	// Get the serviceability program ID. If an override is set in the spec, use that;
	// otherwise derive it from the keypair inside the container.
	if m.dn.Spec.Manager.ServiceabilityProgramID != "" {
		m.ServiceabilityProgramID = m.dn.Spec.Manager.ServiceabilityProgramID
	} else {
		output, err = m.Exec(ctx, []string{"solana", "address", "-k", serviceabilityProgramContainerKeypairPath}, docker.NoPrintOnError())
		if err != nil {
			return fmt.Errorf("failed to get serviceability program pubkey: %v", err)
		}
		m.ServiceabilityProgramID = strings.TrimSpace(string(output))
	}

	// Get the telemetry program ID from the telemetry program keypair.
	output, err = m.Exec(ctx, []string{"solana", "address", "-k", telemetryProgramContainerKeypairPath}, docker.NoPrintOnError())
	if err != nil {
		return fmt.Errorf("failed to get telemetry program pubkey: %v", err)
	}
	m.TelemetryProgramID = strings.TrimSpace(string(output))

	return nil
}

func (m *Manager) Exec(ctx context.Context, command []string, opts ...docker.ExecOption) ([]byte, error) {
	m.log.Debug("--> Executing command", "command", command)
	output, err := docker.Exec(ctx, m.dn.dockerClient, m.ContainerID, command, opts...)
	if err != nil {
		// NOTE: We return the output here because it can contain useful information on error.
		return output, fmt.Errorf("failed to execute command from manager: %w", err)
	}
	return output, nil
}
