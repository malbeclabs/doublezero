package devnet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockervolume "github.com/docker/docker/api/types/volume"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Ledger container is more memory intensive than the others.
	// The solana-test-validator runtime uses ~1.4GB baseline.
	ledgerContainerMemory = 2 * 1024 * 1024 * 1024 // 2GB

	internalLedgerRPCPort   = 8899
	internalLedgerRPCWSPort = 8900
)

type LedgerSpec struct {
	ContainerImage string
}

func (s *LedgerSpec) Validate() error {

	// If the container image is not set, use the DZ_LEDGER_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_LEDGER_IMAGE")
	}

	return nil
}

type Ledger struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID      string
	InternalRPCURL   string
	InternalRPCWSURL string
	ExternalRPCPort  int

	// InternalIPRPCURL is the RPC URL of the ledger container using the internal IP instead of
	// hostname. This is needed by the device/agent which isn't able to use docker DNS.
	InternalIPRPCURL string
}

// dockerContainerName returns the name of the deterministic activator container based on the
// deployID and component name.
func (l *Ledger) dockerContainerName() string {
	return l.dn.Spec.DeployID + "-" + l.dockerContainerHostname()
}

func (l *Ledger) dockerContainerHostname() string {
	return "ledger"
}

// Exists checks if the ledger container exists.
func (l *Ledger) Exists(ctx context.Context) (bool, error) {
	containers, err := l.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", l.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+l.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the ledger container if it's not already running.
func (l *Ledger) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := l.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if ledger exists: %w", err)
	}
	if exists {
		container, err := l.dn.dockerClient.ContainerInspect(ctx, l.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			l.log.Info("--> Ledger already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = l.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set ledger state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = l.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start ledger: %w", err)
		}

		// Set the component's state.
		err = l.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set ledger state: %w", err)
		}

		// Wait for the ledger to be healthy.
		err = waitForSolanaReady(ctx, l.log, l.dn.ExternalHost, l.ExternalRPCPort)
		if err != nil {
			return false, fmt.Errorf("failed to wait for ledger to be healthy: %w", err)
		}

		return true, nil
	}

	return false, l.Start(ctx)
}

// Start creates and starts the ledger container and attaches it to the default network.
func (l *Ledger) Start(ctx context.Context) error {
	l.log.Info("==> Starting ledger", "image", l.dn.Spec.Ledger.ContainerImage)

	volumeName := l.dn.Spec.DeployID + "-ledger"

	// Create a volume with the same labels as the container.
	// NOTE: This is a workaround to allow the volume to have our devnet labels as well as the
	// testcontainers labels.
	labels := map[string]string{
		"org.testcontainers":           "true",
		"org.testcontainers.lang":      "go",
		"org.testcontainers.sessionId": testcontainers.SessionID(),
	}
	maps.Copy(labels, l.dn.labels)
	_, err := l.dn.dockerClient.VolumeCreate(ctx, dockervolume.CreateOptions{
		Name:   volumeName,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("failed to create ledger volume: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image: l.dn.Spec.Ledger.ContainerImage,
		Name:  l.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = l.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", internalLedgerRPCPort), fmt.Sprintf("%d/tcp", internalLedgerRPCWSPort)},
		Env: map[string]string{
			"RPC_PORT": fmt.Sprintf("%d", internalLedgerRPCPort),
			"WS_PORT":  fmt.Sprintf("%d", internalLedgerRPCWSPort),
		},
		WaitingFor: wait.ForAll(
			wait.ForExec([]string{"solana", "cluster-version"}).WithExitCodeMatcher(func(code int) bool { return code == 0 }),
		).WithDeadline(60 * time.Second),
		Networks: []string{l.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			l.dn.DefaultNetwork.Name: {"ledger"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   ledgerContainerMemory,
		},
		Labels: l.dn.labels,
		Mounts: []testcontainers.ContainerMount{
			{
				Source:   testcontainers.GenericVolumeMountSource{Name: volumeName},
				Target:   "/test-ledger",
				ReadOnly: false,
			},
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(l.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	// Set the component's state.
	err = l.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set ledger state: %w", err)
	}

	// Wait for the ledger to be healthy.
	err = waitForSolanaReady(ctx, l.log, l.dn.ExternalHost, l.ExternalRPCPort)
	if err != nil {
		return fmt.Errorf("failed to wait for ledger to be healthy: %w", err)
	}

	l.log.Info("--> Ledger started", "container", l.ContainerID, "internalRPCURL", l.InternalRPCURL, "internalRPCWSURL", l.InternalRPCWSURL)
	return nil
}

func (l *Ledger) setState(ctx context.Context, containerID string) error {
	// Wait for RPC port to be exposed.
	port, err := l.dn.waitForContainerPortExposed(ctx, containerID, internalLedgerRPCPort, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for ledger RPC port to be exposed: %w", err)
	}

	container, err := l.dn.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	l.ContainerID = shortContainerID(container.ID)
	l.InternalRPCURL = fmt.Sprintf("http://%s:8899", l.dockerContainerHostname())
	l.InternalRPCWSURL = fmt.Sprintf("ws://%s:8900", l.dockerContainerHostname())
	l.InternalIPRPCURL = fmt.Sprintf("http://%s:8899", container.NetworkSettings.Networks[l.dn.DefaultNetwork.Name].IPAddress)
	l.ExternalRPCPort = port

	return nil
}

func waitForSolanaReady(ctx context.Context, log *slog.Logger, rpcHost string, rpcPort int) error {
	var loggedWait bool
	timeout := 60 * time.Second
	var attempts int
	err := poll.Until(ctx, func() (bool, error) {
		attempts++
		reqBody := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"getHealth"}`)
		req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s:%d/", rpcHost, rpcPort), reqBody)
		if err != nil {
			if !loggedWait && attempts > 1 {
				log.Debug("--> Waiting for solana to be ready", "rpcPort", rpcPort, "timeout", timeout, "error", err)
				loggedWait = true
			}
			return false, nil
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if !loggedWait && attempts > 1 {
				log.Debug("--> Waiting for solana to be ready", "rpcPort", rpcPort, "timeout", timeout, "error", err)
				loggedWait = true
			}
			return false, nil
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			if !loggedWait && attempts > 1 {
				log.Debug("--> Waiting for solana to be ready", "rpcPort", rpcPort, "timeout", timeout, "error", err)
				loggedWait = true
			}
			return false, nil
		}
		ok := strings.Contains(string(body), `"result":"ok"`)
		return ok, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to wait for solana to be ready: %w", err)
	}
	return nil
}

func (l *Ledger) GetServiceabilityClient() (*serviceability.Client, error) {
	endpoint := "http://" + net.JoinHostPort(l.dn.ExternalHost, strconv.Itoa(l.ExternalRPCPort))
	rpcClient := rpc.New(endpoint)
	programID, err := solana.PublicKeyFromBase58(l.dn.Manager.ServiceabilityProgramID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse program ID: %w", err)
	}
	client := serviceability.New(rpcClient, programID)
	return client, nil
}

func (l *Ledger) GetRPCClient() *rpc.Client {
	endpoint := "http://" + net.JoinHostPort(l.dn.ExternalHost, strconv.Itoa(l.ExternalRPCPort))
	return rpc.New(endpoint)
}

func (l *Ledger) GetTelemetryClient(signer *solana.PrivateKey) (*telemetry.Client, error) {
	endpoint := "http://" + net.JoinHostPort(l.dn.ExternalHost, strconv.Itoa(l.ExternalRPCPort))
	rpcClient := rpc.New(endpoint)
	programID, err := solana.PublicKeyFromBase58(l.dn.Manager.TelemetryProgramID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse program ID: %w", err)
	}
	client := telemetry.New(l.log, rpcClient, signer, programID)
	return client, nil
}
