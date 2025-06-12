package devnet

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types/container"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
)

type ManagerSpec struct {
	ContainerImage string
	KeypairPath    string

	// If true, the smart contract will not be initialized on start.
	NoInitSmartContract bool
}

func (s *ManagerSpec) Validate() error {
	if s.ContainerImage == "" {
		return fmt.Errorf("containerImage is required")
	}

	if s.KeypairPath == "" {
		return fmt.Errorf("keypairPath is required")
	}

	return nil
}

type Manager struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID string
	Pubkey      string
}

func (m *Manager) Start(ctx context.Context) error {
	m.log.Info("==> Starting manager", "image", m.dn.Spec.Manager.ContainerImage)

	// Derive the manager pubkey from the manager keypair.
	managerPubkey, err := solana.PublicAddressFromKeypair(m.dn.Spec.Manager.KeypairPath)
	if err != nil {
		return fmt.Errorf("failed to get manager pubkey: %v", err)
	}

	containerManagerKeypairPath := "/etc/doublezero/manager/dz-manager-keypair.json"
	containerProgramKeypairPath := "/etc/doublezero/manager/dz-program-keypair.json"
	req := testcontainers.ContainerRequest{
		Image: m.dn.Spec.Manager.ContainerImage,
		Name:  m.dn.Spec.DeployID + "-manager",
		Env: map[string]string{
			"DZ_LEDGER_URL":           m.dn.Ledger.InternalURL,
			"DZ_LEDGER_WS":            m.dn.Ledger.InternalWSURL,
			"DZ_PROGRAM_ID":           m.dn.Ledger.ProgramID,
			"DZ_MANAGER_KEYPAIR_PATH": containerManagerKeypairPath,
			"DZ_PROGRAM_KEYPAIR_PATH": containerProgramKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      m.dn.Spec.Manager.KeypairPath,
				ContainerFilePath: containerManagerKeypairPath,
			},
			{
				HostFilePath:      m.dn.Spec.Ledger.ProgramKeypairPath,
				ContainerFilePath: containerProgramKeypairPath,
			},
		},
		Networks: []string{m.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			m.dn.DefaultNetwork.Name: {"manager"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
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

	m.ContainerID = shortContainerID(container.GetContainerID())
	m.Pubkey = managerPubkey

	m.log.Info("--> Manager started", "container", m.ContainerID, "pubkey", m.Pubkey)
	return nil
}

func (m *Manager) InitSmartContract(ctx context.Context) error {
	m.log.Info("==> Initializing smart contract")

	_, err := docker.Exec(ctx, m.dn.dockerClient, m.ContainerID, []string{"bash", "-c", `
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

	m.log.Info("--> Smart contract initialized")

	return nil
}

func (d *Devnet) getContainerIPOnNetwork(ctx context.Context, container testcontainers.Container, networkName string) (string, error) {
	containerInfo, err := d.dockerClient.ContainerInspect(ctx, container.GetContainerID())
	if err != nil {
		return "", err
	}

	network, ok := containerInfo.NetworkSettings.Networks[networkName]
	if !ok {
		return "", fmt.Errorf("container not connected to network %q", networkName)
	}

	return network.IPAddress, nil
}

func (m *Manager) Exec(ctx context.Context, command []string) ([]byte, error) {
	m.log.Debug("--> Executing command", "command", command)
	output, err := docker.Exec(ctx, m.dn.dockerClient, m.ContainerID, command)
	if err != nil {
		// NOTE: We return the output here because it can contain useful information on error.
		return output, fmt.Errorf("failed to execute command from manager: %w", err)
	}
	return output, nil
}
