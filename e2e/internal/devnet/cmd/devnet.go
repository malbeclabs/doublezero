package devnetcmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/gomod"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
)

const (
	// Subnet CIDR prefix.
	// Provides the full last octet range for devices and clients (2-254) for testing.
	subnetCIDRPrefix = 24

	// subnetAllocatorMask is the prefix length used for Docker network IPAM.
	// Using /23 covers both the device CYOA IPs and UTE loopback IPs (which
	// live in the next /24) within a single Docker network, avoiding the need
	// for iptables hacks to allow cross-subnet bridged traffic.
	subnetAllocatorMask = 23

	defaultDeployID  = "dz-local"
	defaultDeployDir = "dev/.deploy"
)

type LocalDevnet struct {
	*devnet.Devnet

	log          *slog.Logger
	workspaceDir string
}

func NewLocalDevnet(log *slog.Logger, deployID string) (*LocalDevnet, error) {
	// Set the default logger for testcontainers.
	logging.SetTestcontainersLogger(log)

	// Initialize a docker client.
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Error("failed to create docker client", "error", err)
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Initialize a subnet allocator.
	subnetAllocator := docker.NewSubnetAllocator("9.128.0.0/9", subnetAllocatorMask, dockerClient)

	// Disable the default testcontainers behavior of automatically removing containers on exit.
	err = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	if err != nil {
		return nil, fmt.Errorf("failed to set TESTCONTAINERS_RYUK_DISABLED: %w", err)
	}

	// Find the workspace directory.
	workspaceDir, err := gomod.FindGoModDir(".", "github.com/malbeclabs/doublezero")
	if err != nil {
		return nil, fmt.Errorf("failed to find go.mod: %w", err)
	}

	// Create a deploy directory if it doesn't exist.
	deployDir := filepath.Join(workspaceDir, defaultDeployDir, deployID)
	if err := os.MkdirAll(deployDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create deploy directory: %w", err)
	}

	// Use the hardcoded serviceability program keypair for this test, since the telemetry program
	// is built with it as an expectation, and the initialize instruction will fail if the owner
	// of the devices/links is not the matching serviceability program ID.
	serviceabilityProgramKeypairPath := filepath.Join(workspaceDir, "e2e", "data", "serviceability-program-keypair.json")

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: deployDir,

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:  true,
			Interval: 10 * time.Second,
		},
	}, log, dockerClient, subnetAllocator)
	if err != nil {
		return nil, fmt.Errorf("failed to create devnet: %w", err)
	}

	return &LocalDevnet{
		Devnet:       dn,
		log:          log,
		workspaceDir: workspaceDir,
	}, nil
}

func newLogger(verbose bool) *slog.Logger {
	logWriter := os.Stdout
	logLevel := slog.LevelDebug
	if !verbose {
		logLevel = slog.LevelInfo
	}
	logger := slog.New(tint.NewHandler(logWriter, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.Kitchen,
	}))
	return logger
}
