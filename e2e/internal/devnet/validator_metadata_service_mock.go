package devnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

const (
	validatorMetadataServiceMockInternalPort = 8080
)

// ValidatorMetadataServiceMockSpec configures the validator metadata service mock container.
type ValidatorMetadataServiceMockSpec struct {
	ContainerImage string
}

func (s *ValidatorMetadataServiceMockSpec) Validate() error {
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_VALIDATOR_METADATA_SERVICE_MOCK_IMAGE")
	}
	return nil
}

// ValidatorMetadataItem is a single validator entry for the mock.
type ValidatorMetadataItem struct {
	IP              string `json:"ip"`
	ActiveStake     int64  `json:"active_stake"`
	VoteAccount     string `json:"vote_account"`
	SoftwareClient  string `json:"software_client"`
	SoftwareVersion string `json:"software_version"`
}

// ValidatorMetadataServiceMock manages the validator metadata service mock container.
type ValidatorMetadataServiceMock struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID  string
	ExternalPort int
}

// Start launches the validator metadata service mock container.
func (d *ValidatorMetadataServiceMock) Start(ctx context.Context) error {
	d.log.Debug("==> Starting validator metadata service mock", "image", d.dn.Spec.ValidatorMetadataServiceMock.ContainerImage)

	req := testcontainers.ContainerRequest{
		Image: d.dn.Spec.ValidatorMetadataServiceMock.ContainerImage,
		Name:  d.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = d.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", validatorMetadataServiceMockInternalPort)},
		WaitingFor: tcwait.ForHTTP("/health").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", validatorMetadataServiceMockInternalPort))).
			WithStartupTimeout(30 * time.Second).
			WithPollInterval(500 * time.Millisecond),
		Networks: []string{d.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.dn.DefaultNetwork.Name: {"validator-metadata-service-mock"},
		},
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: d.dn.labels,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start validator metadata service mock: %w", err)
	}

	err = d.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set validator metadata service mock state: %w", err)
	}

	d.log.Debug("--> Validator metadata service mock started", "container", d.ContainerID)
	return nil
}

// SetValidators updates the mock's validator response data.
func (d *ValidatorMetadataServiceMock) SetValidators(ctx context.Context, validators []ValidatorMetadataItem) error {
	body, err := json.Marshal(validators)
	if err != nil {
		return fmt.Errorf("failed to marshal validator response: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d/config", d.dn.ExternalHost, d.ExternalPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update validator metadata service mock config: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("validator metadata service mock config update failed with status %d", resp2.StatusCode)
	}

	return nil
}

// InternalURL returns the URL accessible from within the Docker network.
func (d *ValidatorMetadataServiceMock) InternalURL() string {
	return fmt.Sprintf("http://%s:%d/api/v1/validators-metadata", d.dockerContainerHostname(), validatorMetadataServiceMockInternalPort)
}

// InternalSolanaRPCURL returns the Solana JSON-RPC URL accessible from within the Docker network.
func (d *ValidatorMetadataServiceMock) InternalSolanaRPCURL() string {
	return fmt.Sprintf("http://%s:%d/solana-rpc", d.dockerContainerHostname(), validatorMetadataServiceMockInternalPort)
}

// ExternalURL returns the URL accessible from the host.
func (d *ValidatorMetadataServiceMock) ExternalURL() string {
	return fmt.Sprintf("http://%s:%d/api/v1/validators-metadata", d.dn.ExternalHost, d.ExternalPort)
}

func (d *ValidatorMetadataServiceMock) dockerContainerHostname() string {
	return "validator-metadata-service-mock"
}

func (d *ValidatorMetadataServiceMock) dockerContainerName() string {
	return d.dn.Spec.DeployID + "-" + d.dockerContainerHostname()
}

func (d *ValidatorMetadataServiceMock) setState(ctx context.Context, containerID string) error {
	d.ContainerID = shortContainerID(containerID)

	port, err := d.dn.waitForContainerPortExposed(ctx, containerID, validatorMetadataServiceMockInternalPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for validator metadata service mock port: %w", err)
	}
	d.ExternalPort = port

	return nil
}
