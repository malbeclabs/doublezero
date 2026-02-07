package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/prometheus"
	"github.com/testcontainers/testcontainers-go"
)

const (
	funderInternalMetricsPort = 8080
)

type FunderSpec struct {
	ContainerImage  string
	KeypairPath     string
	Interval        time.Duration
	MinBalanceSOL   float64
	TopUpSOL        float64
	Verbose         bool
	ExtraRecipients map[string]string
}

func (s *FunderSpec) Validate() error {
	// If the container image is not set, use the DZ_FUNDER_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_FUNDER_IMAGE")
	}

	// Check required fields.
	if s.KeypairPath == "" {
		return fmt.Errorf("keypair path is required")
	}

	// Check that the keypair file exists and is an absolute path.
	if _, err := os.Stat(s.KeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("keypair path does not exist: %s", s.KeypairPath)
	}
	if !filepath.IsAbs(s.KeypairPath) {
		return fmt.Errorf("keypair path must be an absolute path: %s", s.KeypairPath)
	}

	return nil
}

type Funder struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID         string
	ExternalMetricsPort int
}

// Exists checks if the funder container exists.
func (c *Funder) Exists(ctx context.Context) (bool, error) {
	containers, err := c.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", c.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+c.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the funder container if it's not already running.
func (c *Funder) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := c.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if funder exists: %w", err)
	}
	if exists {
		container, err := c.dn.dockerClient.ContainerInspect(ctx, c.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			c.log.Debug("--> Funder already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = c.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set funder state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = c.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start funder: %w", err)
		}

		// Set the component's state.
		err = c.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set funder state: %w", err)
		}

		return true, nil
	}

	return false, c.Start(ctx)
}

func (c *Funder) Start(ctx context.Context) error {
	c.log.Debug("==> Starting funder", "image", c.dn.Spec.Funder.ContainerImage)

	commandArgs := []string{
		"-ledger-rpc-url", c.dn.Ledger.InternalRPCURL,
		"-serviceability-program-id", c.dn.Manager.ServiceabilityProgramID,
		"-keypair", containerSolanaKeypairPath,
		"-metrics-enable",
		"-metrics-addr", fmt.Sprintf(":%d", funderInternalMetricsPort),
	}

	if c.dn.Spec.Funder.Interval > 0 {
		commandArgs = append(commandArgs, "-interval", c.dn.Spec.Funder.Interval.String())
	}
	if c.dn.Spec.Funder.MinBalanceSOL > 0 {
		commandArgs = append(commandArgs, "-min-balance-sol", fmt.Sprintf("%f", c.dn.Spec.Funder.MinBalanceSOL))
	}
	if c.dn.Spec.Funder.TopUpSOL > 0 {
		commandArgs = append(commandArgs, "-top-up-sol", fmt.Sprintf("%f", c.dn.Spec.Funder.TopUpSOL))
	}
	if c.dn.Spec.Funder.Verbose {
		commandArgs = append(commandArgs, "-verbose")
	}

	containerFiles := []testcontainers.ContainerFile{
		{
			HostFilePath:      c.dn.Spec.Funder.KeypairPath,
			ContainerFilePath: containerDoublezeroKeypairPath,
		},
		{
			HostFilePath:      c.dn.Spec.Funder.KeypairPath,
			ContainerFilePath: containerSolanaKeypairPath,
		},
	}

	if len(c.dn.Spec.Funder.ExtraRecipients) > 0 {
		// Write recipients to file.
		recipientsPath := filepath.Join(c.dn.Spec.DeployDir, "funder-recipients.json")
		recipientsFile, err := os.Create(recipientsPath)
		if err != nil {
			return fmt.Errorf("failed to create recipients file: %w", err)
		}
		defer recipientsFile.Close()
		recipientsJSON, err := json.Marshal(c.dn.Spec.Funder.ExtraRecipients)
		if err != nil {
			return fmt.Errorf("failed to marshal recipients: %w", err)
		}
		_, err = recipientsFile.Write(recipientsJSON)
		if err != nil {
			return fmt.Errorf("failed to write recipients: %w", err)
		}

		// Add recipients file to container.
		containerRecipientsPath := "/etc/doublezero-funder/recipients.json"
		containerFiles = append(containerFiles, testcontainers.ContainerFile{
			HostFilePath:      recipientsPath,
			ContainerFilePath: containerRecipientsPath,
		})

		// Add recipients path to command args.
		commandArgs = append(commandArgs, "-recipients", containerRecipientsPath)
	}

	req := testcontainers.ContainerRequest{
		Image: c.dn.Spec.Funder.ContainerImage,
		Name:  c.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = c.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", funderInternalMetricsPort)},
		Networks:     []string{c.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			c.dn.DefaultNetwork.Name: {"funder"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: c.dn.labels,
		Files:  containerFiles,
		Cmd:    commandArgs,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(c.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start funder: %w", err)
	}

	// Set the component's state.
	err = c.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set funder state: %w", err)
	}

	c.log.Debug("--> Funder started", "container", c.ContainerID)
	return nil
}

func (c *Funder) dockerContainerHostname() string {
	return "funder"
}

func (c *Funder) dockerContainerName() string {
	return c.dn.Spec.DeployID + "-" + c.dockerContainerHostname()
}

func (c *Funder) setState(ctx context.Context, containerID string) error {
	c.ContainerID = shortContainerID(containerID)

	// Wait for metrics port to be exposed.
	port, err := c.dn.waitForContainerPortExposed(ctx, containerID, funderInternalMetricsPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for metrics port to be exposed: %w", err)
	}
	c.ExternalMetricsPort = port

	return nil
}

func (c *Funder) PrivateKey() (solana.PrivateKey, error) {
	solanaKeypair, err := solana.PrivateKeyFromSolanaKeygenFile(c.dn.Spec.Funder.KeypairPath)
	if err != nil {
		return solana.PrivateKey{}, fmt.Errorf("failed to load funder keypair: %w", err)
	}
	return solanaKeypair, nil
}

func (c *Funder) GetMetricsClient() *prometheus.MetricsClient {
	return prometheus.NewMetricsClient(fmt.Sprintf("http://%s:%d/metrics", c.dn.ExternalHost, c.ExternalMetricsPort))
}
