package devnet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	defaultContainerNanoCPUs = 1_000_000_000      // 1 core
	defaultContainerMemory   = 1024 * 1024 * 1024 // 1GB

	// Device container is more CPU and memory intensive than the others.
	deviceContainerNanoCPUs = 4_000_000_000          // 4 cores
	deviceContainerMemory   = 4 * 1024 * 1024 * 1024 // 4GB

	// Ledger container is more memory intensive than the others.
	ledgerContainerMemory = 2 * 1024 * 1024 * 1024 // 2GB
)

type Devnet struct {
	log    *slog.Logger
	config DevnetConfig

	InternalLedgerURL   string
	InternalLedgerWSURL string

	ExternalControllerPort int
	ExternalControllerHost string
	ExternalDevicePort     int

	ProgramID     string
	ManagerPubkey string

	defaultNetwork *testcontainers.DockerNetwork

	controller testcontainers.Container
	manager    testcontainers.Container
	devices    map[string]Device
	clients    map[string]Client
}

func New(config DevnetConfig) (*Devnet, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Configure the logger.
	log := config.Logger.With("deployID", config.DeployID)

	// Derive the program ID from the program keypair.
	programID, err := solana.PublicAddressFromKeypair(config.ProgramKeypairPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get program pubkey: %v", err)
	}

	log.Info("--> Devnet config", "programID", programID)

	managerPubkey, err := solana.PublicAddressFromKeypair(config.ManagerKeypairPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager pubkey: %v", err)
	}

	return &Devnet{
		log:    log,
		config: config,

		ProgramID:     programID,
		ManagerPubkey: managerPubkey,

		InternalLedgerURL:   "http://ledger:8899",
		InternalLedgerWSURL: "ws://ledger:8900",

		// This is set after the controller container is started, because the host-exposed port is
		// random.
		ExternalControllerPort: 0,

		// This references the host that the controller is accessible from outside of the docker networks.
		// We are assuming that this is always localhost for now, but if at some point that assumption
		// is no longer true, we can pass this in via config.
		ExternalControllerHost: "localhost",

		// This is set after the device container is started, because the host-exposed port is random.
		ExternalDevicePort: 0,

		devices: make(map[string]Device, 0),
		clients: make(map[string]Client, 0),
	}, nil
}

func (d *Devnet) Close() {}

func (d *Devnet) CreateDefaultNetwork(ctx context.Context) (*testcontainers.DockerNetwork, error) {
	d.log.Info("==> Creating default network")

	// Create the default network.
	network, err := tcnetwork.New(ctx,
		tcnetwork.WithDriver("bridge"),
		tcnetwork.WithAttachable(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create default network: %w", err)
	}

	d.defaultNetwork = network

	d.log.Info("--> Default network created", "network", network.Name)

	return network, nil
}

// StartControlPlane spins up the control plane for the devnet and initializes the smart contract.
// This includes the ledger, manager, controller, and activator, each on the control plane network.
// The DZ program smart contract is deployed and initialized on the ledger.
func (d *Devnet) StartControlPlane(ctx context.Context) error {
	d.log.Info("==> Starting control plane")

	// Create the default/open network.
	if _, err := d.CreateDefaultNetwork(ctx); err != nil {
		return fmt.Errorf("failed to create default network: %w", err)
	}

	// Start the ledger.
	// The ledger is a Solana validator that runs the DZ program smart contract.
	if err := d.startLedger(ctx); err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	// Start the manager.
	// The manager is a container used for initializing the smart contract on the DZ ledger.
	// It contains a script that will initialize the smart contract and seed with locations
	// and exchanges.
	err := d.startManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	// Execute the init-smartcontract script on the manager.
	if err := d.initSmartContract(ctx); err != nil {
		return fmt.Errorf("failed to initialize smart contract: %w", err)
	}

	// Start the controller.
	if err := d.startController(ctx); err != nil {
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// Start the activator.
	if err := d.startActivator(ctx); err != nil {
		return fmt.Errorf("failed to start activator: %w", err)
	}

	d.log.Info("--> Control plane started")
	return nil
}

// startLedger starts a ledger container and attaches it to the control plane network.
func (d *Devnet) startLedger(ctx context.Context) error {
	d.log.Info("==> Starting ledger")

	containerProgramKeypairPath := "/etc/doublezero/ledger/dz-program-keypair.json"
	req := testcontainers.ContainerRequest{
		Image:        d.config.LedgerImage,
		Name:         d.config.DeployID + "-ledger",
		ExposedPorts: []string{"8899/tcp", "8900/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForExec([]string{"solana", "cluster-version"}).WithExitCodeMatcher(func(code int) bool { return code == 0 }),
			wait.ForHTTP("/").WithPort("8899/tcp").WithMethod("POST").
				WithHeaders(map[string]string{"Content-Type": "application/json"}).
				WithBody(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"getHealth"}`)).
				WithResponseMatcher(func(body io.Reader) bool {
					bodyBytes, _ := io.ReadAll(body)
					return strings.Contains(string(bodyBytes), `"result":"ok"`)
				}),
		).WithDeadline(1000 * time.Second),
		Env: map[string]string{
			"DZ_PROGRAM_KEYPAIR_PATH": containerProgramKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      d.config.ProgramKeypairPath,
				ContainerFilePath: containerProgramKeypairPath,
			},
		},
		Networks: []string{d.defaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.defaultNetwork.Name: {"ledger"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   ledgerContainerMemory,
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	d.log.Info("--> Ledger started", "container", shortContainerID(container.GetContainerID()))
	return nil
}

func (d *Devnet) startManager(ctx context.Context) error {
	d.log.Info("==> Starting manager")

	containerManagerKeypairPath := "/etc/doublezero/manager/dz-manager-keypair.json"
	containerProgramKeypairPath := "/etc/doublezero/manager/dz-program-keypair.json"
	req := testcontainers.ContainerRequest{
		Image: d.config.ManagerImage,
		Name:  d.config.DeployID + "-manager",
		Env: map[string]string{
			"DZ_LEDGER_URL":           d.InternalLedgerURL,
			"DZ_LEDGER_WS":            d.InternalLedgerWSURL,
			"DZ_PROGRAM_ID":           d.ProgramID,
			"DZ_MANAGER_KEYPAIR_PATH": containerManagerKeypairPath,
			"DZ_PROGRAM_KEYPAIR_PATH": containerProgramKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      d.config.ManagerKeypairPath,
				ContainerFilePath: containerManagerKeypairPath,
			},
			{
				HostFilePath:      d.config.ProgramKeypairPath,
				ContainerFilePath: containerProgramKeypairPath,
			},
		},
		Networks: []string{d.defaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.defaultNetwork.Name: {"manager"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	d.manager = container

	d.log.Info("--> Manager started", "container", shortContainerID(container.GetContainerID()))
	return nil
}

func (d *Devnet) initSmartContract(ctx context.Context) error {
	d.log.Info("==> Initializing smart contract")

	_, err := d.ManagerExec(ctx, []string{"bash", "-c", `
		set -e

		# Fund the manager account with some SOL if the balance is 0.
		echo "==> Checking manager account balance"
		solana balance --keypair $DZ_MANAGER_KEYPAIR_PATH
		if solana balance --keypair $DZ_MANAGER_KEYPAIR_PATH | grep -q "^0 SOL$"; then
			echo "==> Manager account balance is 0 SOL, funding with 1000 SOL"
			solana airdrop 100 $(solana-keygen pubkey $DZ_MANAGER_KEYPAIR_PATH)
		fi
		echo

		echo "==> Initializing smart contract"
		doublezero init
		echo

		# Populate global configuration onchain.
		echo "==> Populating global configuration onchain"
		echo doublezero global-config set --local-asn 65000 --remote-asn 65342 --tunnel-tunnel-block 172.16.0.0/16 --device-tunnel-block 169.254.0.0/16 --multicastgroup-block 233.84.178.0/24
		doublezero global-config set --local-asn 65000 --remote-asn 65342 --tunnel-tunnel-block 172.16.0.0/16 --device-tunnel-block 169.254.0.0/16 --multicastgroup-block 233.84.178.0/24
		echo "--> Global configuration onchain:"
		doublezero global-config get
		echo

		# Populate location information onchain.
		echo "==> Populating location information onchain"
		doublezero location create --code lax --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
		doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
		doublezero location create --code lhr --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
		doublezero location create --code fra --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
		doublezero location create --code sin --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
		doublezero location create --code tyo --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
		doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
		doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309
		echo "--> Location information onchain:"
		doublezero location list

		# Populate exchange information onchain.
		echo "==> Populating exchange information onchain"
		doublezero exchange create --code xlax --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
		doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
		doublezero exchange create --code xlhr --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
		doublezero exchange create --code xfra --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
		doublezero exchange create --code xsin --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
		doublezero exchange create --code xtyo --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
		doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
		doublezero exchange create --code xams --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309
		echo "--> Exchange information onchain:"
		doublezero exchange list

		echo "--> Smart contract initialized"
	`})
	if err != nil {
		return fmt.Errorf("failed to execute script initializing smart contract: %w", err)
	}

	d.log.Info("--> Smart contract initialized")

	return nil
}

func (d *Devnet) startActivator(ctx context.Context) error {
	d.log.Info("==> Starting activator")

	containerManagerKeypairPath := "/etc/doublezero/activator/dz-manager-keypair.json"
	req := testcontainers.ContainerRequest{
		Image: d.config.ActivatorImage,
		Name:  d.config.DeployID + "-activator",
		Env: map[string]string{
			"DZ_LEDGER_URL":           d.InternalLedgerURL,
			"DZ_LEDGER_WS":            d.InternalLedgerWSURL,
			"DZ_PROGRAM_ID":           d.ProgramID,
			"DZ_MANAGER_KEYPAIR_PATH": containerManagerKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      d.config.ManagerKeypairPath,
				ContainerFilePath: containerManagerKeypairPath,
			},
		},
		Networks: []string{d.defaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.defaultNetwork.Name: {"activator"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start activator: %w", err)
	}

	d.log.Info("--> Activator started", "container", shortContainerID(container.GetContainerID()))
	return nil
}

// ManagerExec executes a given command/script on the manager container.
func (d *Devnet) ManagerExec(ctx context.Context, command []string) ([]byte, error) {
	exitCode, execReader, err := d.manager.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				fmt.Println(string(buf))
			}
		}
		return buf, fmt.Errorf("failed to execute command: %w", err)
	}
	if exitCode != 0 {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				fmt.Println(string(buf))
			}
		}
		return buf, fmt.Errorf("command failed with exit code %d", exitCode)
	}

	buf, err := io.ReadAll(execReader)
	if err != nil {
		return buf, fmt.Errorf("error reading command output: %w", err)
	}
	return buf, nil
}

func (d *Devnet) getContainerIPOnNetwork(ctx context.Context, container testcontainers.Container, networkName string) (string, error) {
	containerInfo, err := d.config.DockerClient.ContainerInspect(ctx, container.GetContainerID())
	if err != nil {
		return "", err
	}

	network, ok := containerInfo.NetworkSettings.Networks[networkName]
	if !ok {
		return "", fmt.Errorf("container not connected to network %q", networkName)
	}

	return network.IPAddress, nil
}

func (d *Devnet) waitContainerHealthy(ctx context.Context, containerID string, timeout time.Duration, delay time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(delay)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for container to be healthy")
		case <-ticker.C:
			inspect, err := d.config.DockerClient.ContainerInspect(waitCtx, containerID)
			if err != nil {
				return fmt.Errorf("failed to inspect container: %w", err)
			}
			if inspect.State.Health.Status == "healthy" {
				return nil
			}
			d.log.Debug("--> Waiting for container to be healthy", "container", shortContainerID(containerID), "status", inspect.State.Health.Status)
		}
	}
}

func shortContainerID(id string) string {
	return id[:12]
}
