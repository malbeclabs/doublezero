package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
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

	// CYOANetworkIPHostID is the offset into the host portion of the subnet (must be < 2^(32 - prefixLen)).
	CYOANetworkIPHostID uint32
}

func (s *ControllerSpec) Validate(cyoaNetworkSpec CYOANetworkSpec) error {
	// If the container image is not set, use the DZ_CONTROLLER_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_CONTROLLER_IMAGE")
	}

	// Check for required fields.
	if s.ExternalHost == "" {
		// If the external host is not set, use localhost, assuming the test is running in a docker container.
		s.ExternalHost = "localhost"
	}

	// Validate that hostID does not select the network (0) or broadcast (max) address.
	hostBits := 32 - cyoaNetworkSpec.CIDRPrefix
	maxHostID := uint32((1 << hostBits) - 1)
	if s.CYOANetworkIPHostID <= 0 || s.CYOANetworkIPHostID >= maxHostID {
		return fmt.Errorf("hostID %d is out of valid range (1 to %d)", s.CYOANetworkIPHostID, maxHostID-1)
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

	// Connect the controller to the device CYOA network.
	if err := c.connectToCYOANetwork(ctx); err != nil {
		return fmt.Errorf("failed to connect controller to device CYOA network: %w", err)
	}

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

func (c *Controller) connectToCYOANetwork(ctx context.Context) error {
	ip, err := netutil.DeriveIPFromCIDR(c.dn.CYOANetwork.SubnetCIDR, c.dn.Spec.Controller.CYOANetworkIPHostID)
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
