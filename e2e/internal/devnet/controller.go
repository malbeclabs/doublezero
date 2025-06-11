package devnet

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
)

const (
	internalControllerPort = 7000
)

func (d *Devnet) GetAgentConfigViaController(ctx context.Context, deviceAgentPubkey string) (*pb.ConfigResponse, error) {
	controllerAddr := net.JoinHostPort(d.ExternalControllerHost, strconv.Itoa(d.ExternalControllerPort))
	d.log.Debug("==> Getting agent config from controller", "controllerAddr", controllerAddr, "agentPubkey", deviceAgentPubkey)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(controllerAddr, opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error creating controller client: %w", err)
	}
	defer conn.Close()
	defer cancel()

	agent := pb.NewControllerClient(conn)
	config, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: deviceAgentPubkey})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error while fetching config: %w", err)
	}

	d.log.Debug("--> Got agent config from controller")

	return config, nil
}

func (d *Devnet) startController(ctx context.Context) error {
	d.log.Info("==> Starting controller")

	req := testcontainers.ContainerRequest{
		Image:        d.config.ControllerImage,
		Name:         d.config.DeployID + "-controller",
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", internalControllerPort)},
		WaitingFor:   wait.ForExposedPort(),
		Env: map[string]string{
			"DZ_LEDGER_URL": d.InternalLedgerURL,
			"DZ_PROGRAM_ID": d.ProgramID,
			"DZ_DEVICE_IP":  d.devices["ny5-dz01"].InternalCYOAIP,
		},
		Networks: []string{d.defaultNetwork.Name},
		NetworkAliases: map[string][]string{
			d.defaultNetwork.Name: {"controller"},
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
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// Get the controller's public/host-exposed port.
	port, err := container.MappedPort(ctx, nat.Port(fmt.Sprintf("%d/tcp", internalControllerPort)))
	if err != nil {
		return fmt.Errorf("failed to get controller port: %w", err)
	}
	d.ExternalControllerPort = port.Int()

	d.controller = container

	d.log.Info("--> Controller started", "container", shortContainerID(container.GetContainerID()), "externalPort", d.ExternalControllerPort)
	return nil
}
