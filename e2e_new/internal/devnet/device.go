package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e_new/internal/logging"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

type Device struct {
	Container      testcontainers.Container
	InternalCYOAIP string
	ExternalPort   int
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
func (d *Devnet) StartDevice(t *testing.T, deviceCode string) (*Device, error) {
	ctx := t.Context()

	d.log.Info("==> Starting device", "deviceCode", deviceCode)

	// Construct an IP address for the device on the CYOA network subnet, the x.y.z.80 address.
	parsedIP, _, err := net.ParseCIDR(d.CYOANetworkCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CYOA network subnet: %w", err)
	}
	ip4 := parsedIP.To4()
	d.log.Info("--> Device IP parsed", "ip", ip4)
	if ip4 == nil {
		return nil, fmt.Errorf("failed to parse CYOA network subnet as IPv4")
	}
	ip4[3] = 80
	deviceIP := ip4.String()
	d.log.Info("--> Device IP selected", "ip", deviceIP)

	// Create the device container, but don't start it yet.
	req := testcontainers.ContainerRequest{
		Image:        d.config.DeviceImage,
		Name:         d.config.DeployID + "-device-" + deviceCode,
		ExposedPorts: []string{"80/tcp"},
		Env: map[string]string{
			"DZ_CONTROLLER_ADDR": d.InternalControllerAddr,
			"DZ_AGENT_PUBKEY":    d.AgentPubkey,
			"DZ_DEVICE_IP":       deviceIP,
		},
		Privileged: true,
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which
		// causes the device to fail to start.
		Resources: container.Resources{
			NanoCPUs: deviceContainerNanoCPUs,
			Memory:   deviceContainerMemory,
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          false,
		Logger:           logging.NewTestcontainersAdapter(d.log),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start device %s: %w", deviceCode, err)
	}

	// Start the device container.
	err = container.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start device %s: %w", deviceCode, err)
	}

	// Attach the device container to the CYOA network.
	// This is configured as eth1 in the startup-config.template.
	containerID := container.GetContainerID()
	err = d.config.DockerClient.NetworkConnect(ctx, d.cyoaNetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: deviceIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: deviceIP,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach device %s to CYOA network: %w", deviceCode, err)
	}

	// Wait for the device container to have status healthy.
	d.log.Info("--> Waiting for device container to be healthy", "container", shortContainerID(containerID), "name", container.Name)
	start := time.Now()
	err = d.waitContainerHealthy(ctx, containerID, 180*time.Second, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for device container to be healthy: %w", err)
	}
	d.log.Info("--> Device container is healthy", "container", shortContainerID(containerID), "name", container.Name, "duration", time.Since(start))

	// Get the device's public/host-exposed port.
	port, err := container.MappedPort(ctx, "80/tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to get device port: %w", err)
	}
	d.ExternalDevicePort = port.Int()

	// Save the device network on this devnet.
	device := Device{
		Container:      container,
		InternalCYOAIP: deviceIP,
		ExternalPort:   d.ExternalDevicePort,
	}
	d.devices[deviceCode] = device

	d.log.Info("--> Device started", "deviceCode", deviceCode, "container", shortContainerID(containerID), "internalAddrOnCYOA", deviceIP, "externalPort", d.ExternalDevicePort)
	return &device, nil
}

// Exec executes a given command/script on the device container.
func (d *Device) Exec(ctx context.Context, command []string) ([]byte, error) {
	exitCode, execReader, err := d.Container.Exec(ctx, command, tcexec.Multiplexed())
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
		return nil, fmt.Errorf("error reading command output: %w", err)
	}
	return buf, nil
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
