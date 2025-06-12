package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/testcontainers/testcontainers-go"
)

const (

	// Device container is more CPU and memory intensive than the others.
	deviceContainerNanoCPUs = 4_000_000_000          // 4 cores
	deviceContainerMemory   = 4 * 1024 * 1024 * 1024 // 4GB
)

type DeviceSpec struct {
	ContainerImage string
	Code           string
	Pubkey         string
	CYOANetworkIP  string
}

func (s *DeviceSpec) Validate() error {
	if s.ContainerImage == "" {
		return fmt.Errorf("containerImage is required")
	}

	if s.Code == "" {
		return fmt.Errorf("code is required")
	}

	if s.Pubkey == "" {
		return fmt.Errorf("pubkey is required")
	}

	if s.CYOANetworkIP == "" {
		return fmt.Errorf("cyoaNetworkIP is required")
	}

	return nil
}

type Device struct {
	dn  *Devnet
	log *slog.Logger

	index int

	ContainerID string
}

func (d *Device) Spec() *DeviceSpec {
	return &d.dn.Spec.Devices[d.index]
}

// StartDevice starts a device container and attaches it to the management network.
//
// Interface ordering very much matters with containerized EOS. The first network
// attached is the management interface, then subsequent networks correspond to
// ethernet interfaces.
//
// Docker attaches interfaces in seemingly random order if the container is not yet started.
// If the networks end up attached in the wrong order, this test will fail as the CYOA network
// will not be attached to Ethernet1. To avoid this, we start the container with the default bridge
// network attached, then attach the CYOA network to the container.
func (d *Device) Start(ctx context.Context) error {
	spec := d.Spec()
	d.log.Info("==> Starting device", "image", spec.ContainerImage, "code", spec.Code, "pubkey", spec.Pubkey, "cyoaNetworkIP", spec.CYOANetworkIP)

	cyoaControllerAddr := net.JoinHostPort(d.dn.Controller.CYOANetworkIP, fmt.Sprintf("%d", internalControllerPort))

	// Create the device container, but don't start it yet.
	req := testcontainers.ContainerRequest{
		Image:        spec.ContainerImage,
		Name:         d.dn.Spec.DeployID + "-device-" + strconv.Itoa(d.index),
		ExposedPorts: []string{"80/tcp"},
		Env: map[string]string{
			"DZ_CONTROLLER_ADDR": cyoaControllerAddr,
			"DZ_AGENT_PUBKEY":    spec.Pubkey,
			"DZ_DEVICE_IP":       spec.CYOANetworkIP,
		},
		Privileged: true,
		Networks: []string{
			d.dn.DefaultNetwork.Name,
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which
		// causes the device to fail to start.
		Resources: container.Resources{
			NanoCPUs: deviceContainerNanoCPUs,
			Memory:   deviceContainerMemory,
		},
		Labels: d.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          false,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start device %s: %w", spec.Code, err)
	}

	// Start the device container.
	err = container.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start device %s: %w", spec.Code, err)
	}

	// Attach the device container to the CYOA network.
	// This is configured as eth1 in the startup-config.template.
	containerID := container.GetContainerID()
	err = d.dn.dockerClient.NetworkConnect(ctx, d.dn.CYOANetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: spec.CYOANetworkIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: spec.CYOANetworkIP,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to attach device %s to CYOA network: %w", spec.Code, err)
	}

	// Wait for the device container to have status healthy.
	d.log.Info("--> Waiting for device container to be healthy", "container", shortContainerID(containerID), "name", container.Name)
	start := time.Now()
	err = d.dn.waitContainerHealthy(ctx, containerID, 180*time.Second, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for device container to be healthy: %w", err)
	}
	d.log.Info("--> Device container is healthy", "container", shortContainerID(containerID), "name", container.Name, "duration", time.Since(start))

	d.ContainerID = shortContainerID(container.GetContainerID())

	d.log.Info("--> Device started", "container", d.ContainerID)
	return nil
}

// ExecCliReturnJSONObject executes a command on the device using the Cli tool and returns the
// JSON-encoded response as a map.
func DeviceExecAristaCliJSON[T any](ctx context.Context, device *Device, command string) (T, error) {
	output, err := device.Exec(ctx, []string{"bash", "-c", fmt.Sprintf("Cli -c \"%s | json\"", command)})
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to execute command: %w", err)
	}

	var result T
	err = json.Unmarshal(output, &result)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to unmarshal JSON: %w: %s", err, string(output))
	}

	return result, nil
}

func (d *Device) Exec(ctx context.Context, command []string) ([]byte, error) {
	d.log.Debug("--> Executing command", "command", command)
	output, err := docker.Exec(ctx, d.dn.dockerClient, d.ContainerID, command)
	if err != nil {
		// NOTE: We return the output here because it can contain useful information on error.
		return output, fmt.Errorf("failed to execute command from device: %w", err)
	}
	return output, nil
}
