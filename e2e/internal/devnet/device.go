package devnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/aristanetworks/goeapi"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/testcontainers/testcontainers-go"
)

//go:embed device/startup-config.tmpl
var deviceStartupConfigTemplate string

const (
	internalEAPIHTTPPort = 80
)

const (
	// Device container is more CPU and memory intensive than the others.
	deviceContainerNanoCPUs = 4_000_000_000          // 4 cores
	deviceContainerMemory   = 4 * 1024 * 1024 * 1024 // 4GB

	defaultCYOANetworkAllocatablePrefix = 29 // 8 addresses
)

type DeviceSpec struct {
	ContainerImage     string
	Code               string
	Location           string
	Exchange           string
	MetricsPublisherPK string

	ExternalHost string

	// CYOANetworkIPHostID is the offset into the host portion of the subnet (must be < 2^(32 - prefixLen)).
	CYOANetworkIPHostID uint32

	// CYOANetworkAllocatablePrefix is the prefix length of the allocatable portion of the CYOA network.
	// This is used to derive the allocatable IP addresses for the device.
	CYOANetworkAllocatablePrefix uint32

	// Agent telemetry config.
	Telemetry DeviceTelemetrySpec
}

type DeviceTelemetrySpec struct {
	Enabled bool

	// KeypairPath is the path to the telemetry keypair.
	KeypairPath string

	// TWAMPListenPort is the port on which the device will listen for TWAMP probes.
	TWAMPListenPort uint16

	// ProbeInterval is the interval at which to probe peers.
	ProbeInterval time.Duration

	// SubmissionInterval is the interval at which to submit samples.
	SubmissionInterval time.Duration

	// PeersRefreshInterval is the interval at which to refresh peers.
	PeersRefreshInterval time.Duration

	// ManagementNS is the name of the management namespace to use for ledger communication.
	// If not provided, the default namespace will be used.
	ManagementNS string

	// Verbose is whether to enable verbose logging.
	Verbose bool
}

func (s *DeviceSpec) Validate(cyoaNetworkSpec CYOANetworkSpec) error {
	// If the container image is not set, use the DZ_DEVICE_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_DEVICE_IMAGE")
	}

	// If the allocatable prefix is not set, use the default.
	if s.CYOANetworkAllocatablePrefix == 0 {
		s.CYOANetworkAllocatablePrefix = defaultCYOANetworkAllocatablePrefix
	}

	// Check for required fields.
	if s.Code == "" {
		return fmt.Errorf("code is required")
	}

	if s.Location == "" {
		return fmt.Errorf("location is required")
	}

	if s.Exchange == "" {
		return fmt.Errorf("exchange is required")
	}

	// Validate that hostID does not select the network (0) or broadcast (max) address.
	hostBits := 32 - cyoaNetworkSpec.CIDRPrefix
	maxHostID := uint32((1 << hostBits) - 1)
	if s.CYOANetworkIPHostID <= 0 || s.CYOANetworkIPHostID >= maxHostID {
		return fmt.Errorf("hostID %d is out of valid range (1 to %d)", s.CYOANetworkIPHostID, maxHostID-1)
	}

	if s.Telemetry.Enabled {
		if err := s.Telemetry.Validate(); err != nil {
			return fmt.Errorf("telemetry: %w", err)
		}
	}

	// If the external host is not set, use the default.
	localhost := os.Getenv("DIND_LOCALHOST")
	if localhost == "" {
		localhost = "localhost"
	}
	if s.ExternalHost == "" {
		s.ExternalHost = localhost
	}

	return nil
}

func (s *DeviceTelemetrySpec) Validate() error {
	if s.KeypairPath == "" {
		return fmt.Errorf("keypair path is required")
	}

	return nil
}

type Device struct {
	dn  *Devnet
	log *slog.Logger

	Spec *DeviceSpec

	// ID is the PDA derived address/pubkey of the device onchain.
	// It's the primary key of the devices dataset in the ledger.
	ID string

	ContainerID   string
	CYOANetworkIP string

	// ExternalEAPIHTTPPort is the port on which the device's EAPI HTTP server is exposed.
	ExternalEAPIHTTPPort int
}

func (d *Device) dockerContainerHostname() string {
	return "device-" + d.Spec.Code
}

func (d *Device) dockerContainerName() string {
	return d.dn.Spec.DeployID + "-" + d.dockerContainerHostname()
}

// Exists checks if the ledger container exists.
func (d *Device) Exists(ctx context.Context) (bool, error) {
	containers, err := d.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", d.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+d.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the device container if it's not already running.
func (d *Device) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := d.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if device exists: %w", err)
	}
	if exists {
		container, err := d.dn.dockerClient.ContainerInspect(ctx, d.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			d.log.Info("--> Device already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = d.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set device state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = d.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start device: %w", err)
		}

		// Set the component's state.
		err = d.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set device state: %w", err)
		}

		return true, nil
	}

	return false, d.Start(ctx)
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
	spec := d.Spec
	d.log.Info("==> Starting device", "image", spec.ContainerImage, "code", spec.Code, "cyoaNetworkIPHostID", spec.CYOANetworkIPHostID)

	ip, err := netutil.DeriveIPFromCIDR(d.dn.CYOANetwork.SubnetCIDR, uint32(spec.CYOANetworkIPHostID))
	if err != nil {
		return fmt.Errorf("failed to derive CYOA network IP: %w", err)
	}
	cyoaNetworkIP := ip.To4().String()

	// Create the device onchain.
	onchainID, err := d.dn.GetOrCreateDeviceOnchain(ctx, spec.Code, spec.Location, spec.Exchange, spec.MetricsPublisherPK, cyoaNetworkIP, []string{cyoaNetworkIP + "/" + strconv.Itoa(int(spec.CYOANetworkAllocatablePrefix))})
	if err != nil {
		return fmt.Errorf("failed to create device %s onchain: %w", spec.Code, err)
	}
	d.log.Info("--> Created device onchain", "code", spec.Code, "cyoaNetworkIP", cyoaNetworkIP, "onchainID", onchainID)

	controllerAddr := net.JoinHostPort(d.dn.Controller.DefaultNetworkIP, fmt.Sprintf("%d", internalControllerPort))

	commandArgs := []string{
		"-controller", controllerAddr,
		"-pubkey", onchainID,
	}

	// Configure telemetry/metrics publisher keypair if set.
	containerTelemetryKeypairPath := "/etc/doublezero/telemetry/keypair.json"
	var reqFiles []testcontainers.ContainerFile
	if spec.Telemetry.KeypairPath != "" {
		reqFiles = []testcontainers.ContainerFile{
			{
				HostFilePath:      spec.Telemetry.KeypairPath,
				ContainerFilePath: containerTelemetryKeypairPath,
			},
		}
	}

	env := map[string]string{}
	if spec.Telemetry.ManagementNS != "" {
		env["DZ_MANAGEMENT_NAMESPACE"] = spec.Telemetry.ManagementNS
	}

	// Create the device container, but don't start it yet.
	req := testcontainers.ContainerRequest{
		Image: spec.ContainerImage,
		Name:  d.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = d.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", internalEAPIHTTPPort)},
		Privileged:   true,
		Networks: []string{
			d.dn.DefaultNetwork.Name,
		},
		Files: reqFiles,
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which
		// causes the device to fail to start.
		Resources: dockercontainer.Resources{
			NanoCPUs: deviceContainerNanoCPUs,
			Memory:   deviceContainerMemory,
		},
		Labels: d.dn.labels,
		Env:    env,
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
	containerID := container.GetContainerID()

	// Get the container's IP address on the default network.
	defaultNetworkIP, err := container.ContainerIP(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}

	// Get default network CIDR prefix.
	inspect, err := container.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}
	if inspect.NetworkSettings.Networks[d.dn.DefaultNetwork.Name] == nil {
		return fmt.Errorf("default network not found for container %s", container.GetContainerID())
	}
	defaultNetworkCIDRPrefix := inspect.NetworkSettings.Networks[d.dn.DefaultNetwork.Name].IPPrefixLen
	defaultNetworkGateway := inspect.NetworkSettings.Networks[d.dn.DefaultNetwork.Name].Gateway

	telemetryCommandArgs := []string{
		"-ledger-rpc-url", d.dn.Ledger.InternalIPRPCURL,
		"-serviceability-program-id", d.dn.Ledger.dn.Manager.ServiceabilityProgramID,
		"-telemetry-program-id", d.dn.Ledger.dn.Manager.TelemetryProgramID,
		"-keypair", containerTelemetryKeypairPath,
		"-local-device-pubkey", onchainID,
	}
	if spec.Telemetry.TWAMPListenPort > 0 {
		telemetryCommandArgs = append(telemetryCommandArgs, "-twamp-listen-port", strconv.Itoa(int(spec.Telemetry.TWAMPListenPort)))
	}
	if spec.Telemetry.ProbeInterval > 0 {
		telemetryCommandArgs = append(telemetryCommandArgs, "-probe-interval", spec.Telemetry.ProbeInterval.String())
	}
	if spec.Telemetry.SubmissionInterval > 0 {
		telemetryCommandArgs = append(telemetryCommandArgs, "-submission-interval", spec.Telemetry.SubmissionInterval.String())
	}
	if spec.Telemetry.PeersRefreshInterval > 0 {
		telemetryCommandArgs = append(telemetryCommandArgs, "-peers-refresh-interval", spec.Telemetry.PeersRefreshInterval.String())
	}
	if spec.Telemetry.ManagementNS != "" {
		telemetryCommandArgs = append(telemetryCommandArgs, "-management-namespace", spec.Telemetry.ManagementNS)
	}
	if spec.Telemetry.Verbose {
		telemetryCommandArgs = append(telemetryCommandArgs, "-verbose")
	}

	// Render the device config from go template.
	var configContents bytes.Buffer
	startupConfigTemplate := template.Must(template.New("startup-config").Parse(deviceStartupConfigTemplate))
	err = startupConfigTemplate.Execute(&configContents, map[string]any{
		"Hostname":                 d.Spec.Code,
		"AgentCommandArgs":         strings.Join(commandArgs, " "),
		"TelemetryEnabled":         spec.Telemetry.Enabled,
		"TelemetryCommandArgs":     strings.Join(telemetryCommandArgs, " "),
		"CYOANetworkIP":            cyoaNetworkIP,
		"CYOANetworkCIDRPrefix":    strconv.Itoa(d.dn.Spec.CYOANetwork.CIDRPrefix),
		"DefaultNetworkIP":         defaultNetworkIP,
		"DefaultNetworkCIDRPrefix": strconv.Itoa(defaultNetworkCIDRPrefix),
		"DefaultNetworkGateway":    defaultNetworkGateway,
		"ManagementNS":             spec.Telemetry.ManagementNS,
	})
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}
	containerConfigPath := "/etc/doublezero/agent/startup-config"
	d.log.Info("==> Writing device config", "path", containerConfigPath)
	err = container.CopyToContainer(ctx, configContents.Bytes(), containerConfigPath, 0644)
	if err != nil {
		return fmt.Errorf("failed to write device config file: %w", err)
	}

	// Attach the device container to the CYOA network.
	// This is configured as eth1 in the startup-config.template.
	err = d.dn.dockerClient.NetworkConnect(ctx, d.dn.CYOANetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: cyoaNetworkIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: cyoaNetworkIP,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to attach device %s to CYOA network: %w", spec.Code, err)
	}

	// Wait for the device container to have status healthy.
	d.log.Info("--> Waiting for device container to be healthy", "container", shortContainerID(containerID), "name", container.Name)
	start := time.Now()
	err = d.dn.waitContainerHealthy(ctx, containerID, 300*time.Second, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for device container to be healthy: %w", err)
	}
	d.log.Info("--> Device container is healthy", "container", shortContainerID(containerID), "cyoaNetworkIP", cyoaNetworkIP, "defaultNetworkIP", defaultNetworkIP, "name", container.Name, "duration", time.Since(start))

	// Set the component's state.
	err = d.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set device state: %w", err)
	}

	d.log.Info("--> Device started", "container", d.ContainerID, "cyoaNetworkIP", cyoaNetworkIP, "defaultNetworkIP", defaultNetworkIP, "onchainID", onchainID)
	return nil
}

func (d *Device) setState(ctx context.Context, containerID string) error {
	// Get the device agent pubkey from onchain.
	onchainID, err := d.dn.GetDevicePubkeyOnchain(ctx, d.Spec.Code)
	if err != nil {
		return fmt.Errorf("failed to get device agent pubkey onchain for device %s: %w", d.Spec.Code, err)
	}

	// Wait for the device's CYOA network IP address to be assigned.
	var loggedWait bool
	timeout := 10 * time.Second
	var attempts int
	var container dockercontainer.InspectResponse
	err = pollUntil(ctx, func() (bool, error) {
		attempts++
		var err error
		container, err = d.dn.dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}
		if container.NetworkSettings.Networks[d.dn.CYOANetwork.Name] == nil {
			if !loggedWait && attempts > 1 {
				d.log.Debug("--> Waiting for device CYOA network IP to be assigned", "container", shortContainerID(container.ID), "timeout", timeout)
				loggedWait = true
			}
			return false, nil
		}
		return true, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to get device CYOA network IP: %w", err)
	}

	// Get the device's CYOA network IP address.
	if container.NetworkSettings.Networks[d.dn.CYOANetwork.Name] == nil {
		return fmt.Errorf("failed to get device CYOA network IP")
	}
	ip := container.NetworkSettings.Networks[d.dn.CYOANetwork.Name].IPAddress

	// Wait for EAPI HTTP port to be exposed.
	loggedWait = false
	timeout = 10 * time.Second
	var port int
	err = pollUntil(ctx, func() (bool, error) {
		attempts++
		var err error
		container, err = d.dn.dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}
		ports, ok := container.NetworkSettings.Ports[nat.Port(fmt.Sprintf("%d/tcp", internalEAPIHTTPPort))]
		if !ok || len(ports) == 0 {
			if !loggedWait && attempts > 1 {
				d.log.Debug("--> Waiting for EAPI HTTP port to be exposed", "container", shortContainerID(container.ID), "timeout", timeout)
				loggedWait = true
			}
			return false, nil
		}
		port, err = strconv.Atoi(ports[0].HostPort)
		if err != nil {
			return false, fmt.Errorf("failed to get RPC port: %w", err)
		}
		return true, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to wait for EAPI HTTP port to be exposed: %w", err)
	}
	d.ExternalEAPIHTTPPort = port

	d.ContainerID = shortContainerID(container.ID)
	d.ID = onchainID
	d.CYOANetworkIP = ip

	return nil
}

func (d *Device) GetEAPIHTTPClient() (*goeapi.Node, error) {
	return goeapi.Connect("http", d.Spec.ExternalHost, "admin", "admin", d.ExternalEAPIHTTPPort)
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
