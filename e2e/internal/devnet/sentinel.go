package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

const (
	sentinelInternalMetricsPort = 2112
)

// SentinelSpec configures the sentinel container.
type SentinelSpec struct {
	ContainerImage             string
	KeypairPath                string // Host path to sentinel keypair JSON.
	MulticastGroupPubkeys      string // Comma-separated multicast group pubkeys.
	MulticastPublisherPollSecs int    // Poll interval in seconds (default: 5 for e2e).
	ValidatorMetadataURL       string // Validator metadata service URL override.
}

func (s *SentinelSpec) Validate() error {
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_SENTINEL_IMAGE")
	}
	if s.KeypairPath == "" {
		return fmt.Errorf("keypair path is required")
	}
	if _, err := os.Stat(s.KeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("keypair path does not exist: %s", s.KeypairPath)
	}
	if !filepath.IsAbs(s.KeypairPath) {
		return fmt.Errorf("keypair path must be an absolute path: %s", s.KeypairPath)
	}
	return nil
}

// Sentinel manages the sentinel container.
type Sentinel struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID         string
	ExternalMetricsPort int
}

// Start launches the sentinel container.
func (s *Sentinel) Start(ctx context.Context) error {
	s.log.Debug("==> Starting sentinel", "image", s.dn.Spec.Sentinel.ContainerImage)

	pollSecs := s.dn.Spec.Sentinel.MulticastPublisherPollSecs
	if pollSecs == 0 {
		pollSecs = 5
	}

	// The mock service serves both Solana JSON-RPC (for --solana-rpc) and the
	// metadata API (for --validator-metadata-url enrichment).
	solanaRPCURL := ""
	validatorMetadataURL := s.dn.Spec.Sentinel.ValidatorMetadataURL
	if s.dn.ValidatorMetadataServiceMock != nil {
		solanaRPCURL = s.dn.ValidatorMetadataServiceMock.InternalSolanaRPCURL()
		if validatorMetadataURL == "" {
			validatorMetadataURL = s.dn.ValidatorMetadataServiceMock.InternalURL()
		}
	}

	commandArgs := []string{
		"--env", s.dn.Manager.ServiceabilityProgramID,
		"--dz-rpc", s.dn.Ledger.InternalRPCURL,
		"--keypair", "/etc/sentinel/keypair.json",
		"--metrics-addr", fmt.Sprintf("0.0.0.0:%d", sentinelInternalMetricsPort),
		"--multicast-group-pubkeys", s.dn.Spec.Sentinel.MulticastGroupPubkeys,
		"--solana-rpc", solanaRPCURL,
		"--poll-interval", fmt.Sprintf("%d", pollSecs),
		"--log", "doublezero_sentinel=debug",
	}
	if validatorMetadataURL != "" {
		commandArgs = append(commandArgs, "--validator-metadata-url", validatorMetadataURL)
	}

	req := testcontainers.ContainerRequest{
		Image: s.dn.Spec.Sentinel.ContainerImage,
		Name:  s.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = s.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", sentinelInternalMetricsPort)},
		Cmd:          commandArgs,
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      s.dn.Spec.Sentinel.KeypairPath,
				ContainerFilePath: "/etc/sentinel/keypair.json",
			},
		},
		WaitingFor: tcwait.ForHTTP("/").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", sentinelInternalMetricsPort))).
			WithStartupTimeout(60 * time.Second).
			WithPollInterval(1 * time.Second),
		Networks: []string{s.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			s.dn.DefaultNetwork.Name: {"sentinel"},
		},
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: s.dn.labels,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(s.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start sentinel: %w", err)
	}

	err = s.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set sentinel state: %w", err)
	}

	s.log.Debug("--> Sentinel started", "container", s.ContainerID)
	return nil
}

func (s *Sentinel) dockerContainerHostname() string {
	return "sentinel"
}

func (s *Sentinel) dockerContainerName() string {
	return s.dn.Spec.DeployID + "-" + s.dockerContainerHostname()
}

func (s *Sentinel) setState(ctx context.Context, containerID string) error {
	s.ContainerID = shortContainerID(containerID)

	port, err := s.dn.waitForContainerPortExposed(ctx, containerID, sentinelInternalMetricsPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for metrics port to be exposed: %w", err)
	}
	s.ExternalMetricsPort = port

	return nil
}
