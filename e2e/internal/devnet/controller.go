package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
)

const (
	internalControllerPort = 7000
)

type ControllerSpec struct {
	ContainerImage string
	ExternalHost   string
}

func (s *ControllerSpec) Validate() error {
	if s.ContainerImage == "" {
		return fmt.Errorf("containerImage is required")
	}

	if s.ExternalHost == "" {
		return fmt.Errorf("externalHost is required")
	}

	return nil
}

type Controller struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID   string
	ExternalPort  int
	CYOANetworkIP string
}

func (c *Controller) Start(ctx context.Context) error {
	c.log.Info("==> Starting controller", "image", c.dn.Spec.Controller.ContainerImage, "externalHost", c.dn.Spec.Controller.ExternalHost)

	req := testcontainers.ContainerRequest{
		Image:        c.dn.Spec.Controller.ContainerImage,
		Name:         c.dn.Spec.DeployID + "-controller",
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", internalControllerPort)},
		WaitingFor:   wait.ForExposedPort(),
		Env: map[string]string{
			"DZ_LEDGER_URL": c.dn.Ledger.InternalURL,
			"DZ_PROGRAM_ID": c.dn.Ledger.ProgramID,
		},
		Networks: []string{c.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			c.dn.DefaultNetwork.Name: {"controller"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: c.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(c.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// Get the controller's public/host-exposed port.
	port, err := container.MappedPort(ctx, nat.Port(fmt.Sprintf("%d/tcp", internalControllerPort)))
	if err != nil {
		return fmt.Errorf("failed to get controller port: %w", err)
	}

	c.ContainerID = shortContainerID(container.GetContainerID())
	c.ExternalPort = port.Int()

	c.log.Info("--> Controller started", "container", c.ContainerID, "externalPort", c.ExternalPort)
	return nil
}

func (c *Controller) GetAgentConfig(ctx context.Context, deviceAgentPubkey string) (*pb.ConfigResponse, error) {
	controllerAddr := net.JoinHostPort(c.dn.Spec.Controller.ExternalHost, strconv.Itoa(c.ExternalPort))
	c.log.Debug("==> Getting agent config from controller", "controllerAddr", controllerAddr, "agentPubkey", deviceAgentPubkey)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(controllerAddr, opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error creating controller client: %w", err)
	}
	defer conn.Close()

	agent := pb.NewControllerClient(conn)
	config, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: deviceAgentPubkey})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error while fetching config: %w", err)
	}

	c.log.Debug("--> Got agent config from controller")

	return config, nil
}

func (c *Controller) ConnectToCYOANetwork(ctx context.Context) error {
	// Construct an IP address for the controller on the device CYOA network.
	ip, err := netutil.BuildIPInCIDR(c.dn.CYOANetwork.SubnetCIDR, 85)
	if err != nil {
		return fmt.Errorf("failed to build controller IP in CYOA network subnet: %w", err)
	}
	controllerIP := ip.String()

	// Connect the controller to the device CYOA network.
	err = c.dn.dockerClient.NetworkConnect(ctx, c.dn.CYOANetwork.Name, c.ContainerID, &network.EndpointSettings{
		IPAddress: controllerIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: controllerIP,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to connect controller to CYOA network %s: %w", c.dn.CYOANetwork.Name, err)
	}

	c.CYOANetworkIP = controllerIP

	c.log.Info("--> Controller connected to CYOA network", "controllerIP", controllerIP)

	return nil
}
