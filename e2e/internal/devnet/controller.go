package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
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

type ControllerSpec struct {
	ContainerImage string
}

func (s *ControllerSpec) Validate(cyoaNetworkSpec CYOANetworkSpec) error {
	// If the container image is not set, use the DZ_CONTROLLER_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_CONTROLLER_IMAGE")
	}

	return nil
}

type Controller struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID      string
	ExternalPort     int
	DefaultNetworkIP string
}

// Exists checks if the controller container exists.
func (c *Controller) Exists(ctx context.Context) (bool, error) {
	containers, err := c.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", c.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+c.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the controller container if it's not already running.
func (c *Controller) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := c.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if controller exists: %w", err)
	}
	if exists {
		container, err := c.dn.dockerClient.ContainerInspect(ctx, c.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			c.log.Info("--> Controller already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = c.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set controller state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = c.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start controller: %w", err)
		}

		// Set the component's state.
		err = c.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set controller state: %w", err)
		}

		return true, nil
	}

	return false, c.Start(ctx)
}

func (c *Controller) Start(ctx context.Context) error {
	c.log.Info("==> Starting controller", "image", c.dn.Spec.Controller.ContainerImage)

	env := map[string]string{
		"DZ_LEDGER_URL":                c.dn.Ledger.InternalRPCURL,
		"DZ_SERVICEABILITY_PROGRAM_ID": c.dn.Manager.ServiceabilityProgramID,
	}
	if c.dn.Prometheus != nil && c.dn.Prometheus.InternalURL != "" {
		env["ALLOY_PROMETHEUS_URL"] = c.dn.Prometheus.RemoteWriteURL()
	}

	req := testcontainers.ContainerRequest{
		Image: c.dn.Spec.Controller.ContainerImage,
		Name:  c.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = c.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", internalControllerPort)},
		WaitingFor:   wait.ForExposedPort(),
		Env:          env,
		Networks:     []string{c.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			c.dn.DefaultNetwork.Name: {"controller"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: dockercontainer.Resources{
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

	// Set the component's state.
	err = c.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set controller state: %w", err)
	}

	c.log.Info("--> Controller started", "container", c.ContainerID, "externalPort", c.ExternalPort)
	return nil
}

func (c *Controller) dockerContainerHostname() string {
	return "controller"
}

func (c *Controller) dockerContainerName() string {
	return c.dn.Spec.DeployID + "-" + c.dockerContainerHostname()
}

func (c *Controller) setState(ctx context.Context, containerID string) error {
	// Wait for the controller's public/host-exposed port to be exposed.
	port, err := c.dn.waitForContainerPortExposed(ctx, containerID, internalControllerPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for controller port to be exposed: %w", err)
	}

	// Get the controller's IP address on the default network.
	container, err := c.dn.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}
	if container.NetworkSettings.Networks[c.dn.DefaultNetwork.Name] == nil {
		return fmt.Errorf("failed to get controller IP")
	}
	ip := container.NetworkSettings.Networks[c.dn.DefaultNetwork.Name].IPAddress

	c.ContainerID = shortContainerID(container.ID)
	c.ExternalPort = port
	c.DefaultNetworkIP = ip

	return nil
}

func (c *Controller) GetAgentConfig(ctx context.Context, deviceAgentPubkey string) (*pb.ConfigResponse, error) {
	controllerAddr := net.JoinHostPort(c.dn.ExternalHost, strconv.Itoa(c.ExternalPort))
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
