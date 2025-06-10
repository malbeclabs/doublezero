package devnet

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e_new/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
)

func (d *Devnet) GetAgentConfigViaController(ctx context.Context) (*pb.ConfigResponse, error) {
	controllerAddr := net.JoinHostPort(d.ExternalControllerHost, strconv.Itoa(d.ExternalControllerPort))
	d.log.Debug("==> Getting agent config from controller", "controllerAddr", controllerAddr, "agentPubkey", d.AgentPubkey)

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
	config, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: d.AgentPubkey})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error while fetching config: %w", err)
	}

	d.log.Debug("--> Got agent config from controller")

	return config, nil
}

func (d *Devnet) startController(ctx context.Context) error {
	d.log.Info("==> Starting controller")

	// Construct an IP address for the controller on the CYOA network subnet, the x.y.z.85 address.
	parsedIP, _, err := net.ParseCIDR(d.CYOANetworkCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse CYOA network subnet: %w", err)
	}
	ip4 := parsedIP.To4()
	d.log.Info("--> Controller IP parsed", "ip", ip4)
	if ip4 == nil {
		return fmt.Errorf("failed to parse CYOA network subnet as IPv4")
	}
	ip4[3] = 85
	ip := ip4.String()
	d.log.Info("--> Controller IP selected", "ip", ip)

	req := testcontainers.ContainerRequest{
		Image:        d.config.ControllerImage,
		Name:         d.config.DeployID + "-controller",
		ExposedPorts: []string{"7000/tcp"},
		WaitingFor:   wait.ForExposedPort(),
		Env: map[string]string{
			"DZ_LEDGER_URL": d.InternalLedgerURL,
			"DZ_PROGRAM_ID": d.ProgramID,
			"DZ_DEVICE_IP":  d.devices["ny5-dz01"].InternalCYOAIP,
		},
		Networks: []string{d.defaultNetwork.Name, d.cyoaNetwork.Name},
		NetworkAliases: map[string][]string{
			d.defaultNetwork.Name: {"controller"},
			d.cyoaNetwork.Name:    {"controller"},
		},
		// NOTE: We need to set a specific IP address for the controller on the CYOA network for
		// the component to be reachable. Otherwise, routing does not work.
		EndpointSettingsModifier: func(m map[string]*network.EndpointSettings) {
			if m[d.cyoaNetwork.Name] == nil {
				m[d.cyoaNetwork.Name] = &network.EndpointSettings{}
			}
			m[d.cyoaNetwork.Name].IPAddress = ip
			m[d.cyoaNetwork.Name].IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: ip,
			}
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

	// Get the controller's IP address.
	ip, err = d.getContainerIPOnNetwork(ctx, container, d.cyoaNetwork.Name)
	if err != nil {
		return fmt.Errorf("failed to get controller IP address: %w", err)
	}
	d.InternalControllerAddr = net.JoinHostPort(ip, "7000")

	// Get the controller's public/host-exposed port.
	port, err := container.MappedPort(ctx, "7000/tcp")
	if err != nil {
		return fmt.Errorf("failed to get controller port: %w", err)
	}
	d.ExternalControllerPort = port.Int()

	d.log.Info("--> Controller started", "container", shortContainerID(container.GetContainerID()), "internalAddrOnCYOA", d.InternalControllerAddr, "externalPort", d.ExternalControllerPort)
	return nil
}
