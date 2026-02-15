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
	"github.com/gagliardetto/solana-go"
	controllerconfig "github.com/malbeclabs/doublezero/controlplane/controller/config"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/prometheus"
	solanautil "github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
)

//go:embed device/startup-config.tmpl
var deviceStartupConfigTemplate string

const (
	internalEAPIHTTPPort = 80
)

const (
	// Device container is more CPU and memory intensive than the others.
	deviceContainerNanoCPUs = 4_000_000_000            // 4 cores
	deviceContainerMemory   = 4.5 * 1024 * 1024 * 1024 // 4.5GB

	defaultCYOANetworkAllocatablePrefix = 29 // 8 addresses
)

type DeviceSpec struct {
	ContainerImage     string
	Code               string
	Location           string
	Exchange           string
	MetricsPublisherPK string

	// CYOANetworkIPHostID is the offset into the host portion of the subnet (must be < 2^(32 - prefixLen)).
	CYOANetworkIPHostID uint32

	// CYOANetworkAllocatablePrefix is the prefix length of the allocatable portion of the CYOA network.
	// This is used to derive the allocatable IP addresses for the device.
	CYOANetworkAllocatablePrefix uint32

	// Agent telemetry config.
	Telemetry DeviceTelemetrySpec

	// Additional docker networks to attach the device to.
	AdditionalNetworks []string

	// Interfaces is a map of interface names to types.
	Interfaces map[string]string

	// LoopbackInterfaces is a map of interface names to loopback types.
	LoopbackInterfaces map[string]string

	// UserTunnelEndpoints specifies loopback interfaces to register as user tunnel endpoints.
	// Each entry creates a Loopback interface with an IP derived from the CYOA subnet
	// and marked as a user tunnel endpoint onchain.
	UserTunnelEndpoints []UserTunnelEndpointSpec
}

// UserTunnelEndpointSpec describes a loopback interface to register as a user tunnel endpoint.
type UserTunnelEndpointSpec struct {
	// InterfaceName is the EOS interface name (e.g., "Loopback100").
	InterfaceName string

	// IPHostIDOffset is added to the device's CYOANetworkIPHostID to derive the loopback IP.
	// For example, if the device has hostID=8 and offset=1, the loopback gets hostID=9.
	IPHostIDOffset uint32
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

	// MetricsEnable is whether to enable prometheus metrics.
	MetricsEnable bool

	// MetricsAddr is the listen address for the prometheus metrics server.
	MetricsAddr string
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

	if s.Interfaces == nil {
		s.Interfaces = map[string]string{}
	}

	if s.LoopbackInterfaces == nil {
		s.LoopbackInterfaces = map[string]string{}
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
	DZPrefix      string // The dz_prefix registered onchain for this device

	// ExternalEAPIHTTPPort is the port on which the device's EAPI HTTP server is exposed.
	ExternalEAPIHTTPPort int

	// ExternalTelemetryMetricsPort is the port on which the device's telemetry metrics server is exposed.
	ExternalTelemetryMetricsPort int

	// UserTunnelEndpointIPs maps interface names to their IPs for UTE loopbacks.
	// e.g., {"Loopback100": "9.200.53.9"}
	UserTunnelEndpointIPs map[string]string
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
			d.log.Debug("--> Device already running", "container", shortContainerID(container.ID))

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
	d.log.Debug("==> Starting device", "image", spec.ContainerImage, "code", spec.Code, "cyoaNetworkIPHostID", spec.CYOANetworkIPHostID)

	ip, err := netutil.DeriveIPFromCIDR(d.dn.CYOANetwork.SubnetCIDR, uint32(spec.CYOANetworkIPHostID))
	if err != nil {
		return fmt.Errorf("failed to derive CYOA network IP: %w", err)
	}
	cyoaNetworkIP := ip.To4().String()

	// If the metrics publisher pubkey is not set, try to use the telemetry keypair.
	if spec.MetricsPublisherPK == "" && spec.Telemetry.KeypairPath != "" {
		if _, err := os.Stat(spec.Telemetry.KeypairPath); os.IsExist(err) {
			keypairJSON, err := os.ReadFile(spec.Telemetry.KeypairPath)
			if err != nil {
				return fmt.Errorf("failed to read telemetry keypair: %w", err)
			}
			spec.MetricsPublisherPK, err = solanautil.PubkeyFromKeypairJSON(keypairJSON)
			if err != nil {
				return fmt.Errorf("failed to get telemetry keypair: %w", err)
			}
		}
	}

	// Create the device onchain.
	// Use a different IP range for dz_prefixes to avoid conflicts with the public IP.
	// We derive a unique /29 subnet by taking the public IP and adding 128 to the last octet,
	// then rounding down to a /29 boundary (multiples of 8).
	// For example, if public IP is 10.237.248.8, we use 10.237.248.136/29 as dz_prefix.
	// If adding 128 would overflow the byte (last octet >= 128), we increment the third octet.
	// For example, if public IP is 9.200.53.200, we use 9.200.54.72/29 as dz_prefix.
	// This ensures each device gets a unique dz_prefix that doesn't overlap with public IPs.
	publicIP := net.ParseIP(cyoaNetworkIP)
	if publicIP == nil {
		return fmt.Errorf("failed to parse public IP: %s", cyoaNetworkIP)
	}
	publicIPBytes := publicIP.To4()
	if publicIPBytes == nil {
		return fmt.Errorf("public IP is not IPv4: %s", cyoaNetworkIP)
	}
	// Add 128 to the last octet to create separation from public IPs,
	// then round down to /29 boundary (multiple of 8)
	// Handle byte overflow by incrementing third octet when necessary
	dzPrefixBytes := make(net.IP, 4)
	copy(dzPrefixBytes, publicIPBytes)
	if publicIPBytes[3] >= 128 {
		// Overflow case: increment third octet
		dzPrefixBytes[2]++
		dzPrefixBytes[3] = ((publicIPBytes[3] - 128) / 8) * 8
	} else {
		// No overflow: just add 128
		dzPrefixBytes[3] = ((publicIPBytes[3] + 128) / 8) * 8
	}
	dzPrefix := dzPrefixBytes.String() + "/29"
	d.DZPrefix = dzPrefix

	// Compute a second dz_prefix for UTE loopback interfaces when needed.
	// This prefix lives in a different /24 than the CYOA subnet (third octet incremented by 1)
	// so the loopback IP doesn't overlap with Ethernet1's subnet on EOS.
	// The IP is globally-routable (same 9.x.x.x space) to pass the onchain is_global check.
	var uteDzPrefixBytes net.IP
	dzPrefixes := []string{dzPrefix}
	if len(spec.UserTunnelEndpoints) > 0 {
		uteDzPrefixBytes = make(net.IP, 4)
		copy(uteDzPrefixBytes, publicIPBytes)
		uteDzPrefixBytes[2]++                            // different /24 than CYOA subnet
		uteDzPrefixBytes[3] = (publicIPBytes[3] / 8) * 8 // round to /29 boundary
		uteDzPrefix := uteDzPrefixBytes.String() + "/29"
		dzPrefixes = append(dzPrefixes, uteDzPrefix)
	}

	devicePK, err := d.dn.GetOrCreateDeviceOnchain(ctx, spec.Code, spec.Location, spec.Exchange, spec.MetricsPublisherPK, cyoaNetworkIP, dzPrefixes, "mgmt")
	if err != nil {
		return fmt.Errorf("failed to create device %s onchain: %w", spec.Code, err)
	}
	d.log.Debug("--> Created device onchain", "code", spec.Code, "cyoaNetworkIP", cyoaNetworkIP, "dzPrefix", dzPrefix, "devicePK", devicePK)

	// MaxUserTunnelSlots is now a constant from config package
	d.log.Debug("--> Using MaxUserTunnelSlots constant", "maxUsers", controllerconfig.MaxUserTunnelSlots)

	// Create interfaces onchain.
	for name, ifaceType := range spec.Interfaces {
		out, err := d.dn.Manager.Exec(ctx, []string{
			"doublezero", "device", "interface", "create", spec.Code, name,
		}, docker.NoPrintOnError())
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				d.log.Debug("--> Interface already exists onchain", "code", spec.Code, "name", name, "ifaceType", ifaceType)
				continue
			}
			fmt.Println(string(out))
			return fmt.Errorf("failed to create interface %s for device %s: %w", name, spec.Code, err)
		}

		// Wait for the interface to exist onchain.
		serviceabilityClient, err := d.dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return fmt.Errorf("failed to get serviceability client: %w", err)
		}
		err = poll.Until(ctx, func() (bool, error) {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false, fmt.Errorf("failed to get program data: %w", err)
			}
			for _, device := range data.Devices {
				pk := solana.PublicKeyFromBytes(device.PubKey[:])
				if pk.String() == devicePK {
					for _, iface := range device.Interfaces {
						if iface.Name == name {
							return true, nil
						}
					}
				}
			}
			return false, nil
		}, 30*time.Second, 1*time.Second)
		if err != nil {
			return fmt.Errorf("failed to wait for interface %s to exist onchain: %w", name, err)
		}

		d.log.Debug("--> Created interface onchain", "code", spec.Code, "name", name, "ifaceType", ifaceType)
	}

	// Create loopback interfaces onchain.
	for name, loopbackType := range spec.LoopbackInterfaces {
		out, err := d.dn.Manager.Exec(ctx, []string{"doublezero", "device", "interface", "create", spec.Code, name, "--loopback-type", loopbackType}, docker.NoPrintOnError())
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				d.log.Debug("--> Loopback interface already exists onchain", "code", spec.Code, "name", name, "loopbackType", loopbackType)
				continue
			}
			fmt.Println(string(out))
			return fmt.Errorf("failed to create loopback interface %s for device %s: %w", name, spec.Code, err)
		}

		// Wait for the loopback interface to exist onchain.
		serviceabilityClient, err := d.dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return fmt.Errorf("failed to get serviceability client: %w", err)
		}
		err = poll.Until(ctx, func() (bool, error) {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false, fmt.Errorf("failed to get serviceability client: %w", err)
			}
			for _, device := range data.Devices {
				pk := solana.PublicKeyFromBytes(device.PubKey[:])
				if pk.String() == devicePK {
					for _, iface := range device.Interfaces {
						if iface.Name == name {
							return true, nil
						}
					}
				}
			}
			return false, nil
		}, 30*time.Second, 1*time.Second)
		if err != nil {
			return fmt.Errorf("failed to wait for loopback interface %s to exist onchain: %w", name, err)
		}

		d.log.Debug("--> Created loopback interface onchain", "code", spec.Code, "name", name, "loopbackType", loopbackType)
	}

	// Create user tunnel endpoint loopback interfaces onchain.
	// UTE loopback IPs are derived from the second dz_prefix (different /24 than CYOA subnet)
	// to avoid IP overlap with Ethernet1 on EOS and to pass the onchain is_global check.
	// The client adds a static route to reach this IP via the device's CYOA IP on the bridge.
	d.UserTunnelEndpointIPs = make(map[string]string)
	for _, ute := range spec.UserTunnelEndpoints {
		// Derive UTE IP from the second dz_prefix: take the base IP and add the offset.
		uteIPBytes := make(net.IP, 4)
		copy(uteIPBytes, uteDzPrefixBytes)
		uteIPBytes[3] += byte(ute.IPHostIDOffset)
		uteIPStr := uteIPBytes.String()
		ipNet := uteIPStr + "/32"

		out, err := d.dn.Manager.Exec(ctx, []string{
			"doublezero", "device", "interface", "create", spec.Code, ute.InterfaceName,
			"--ip-net", ipNet,
			"--user-tunnel-endpoint", "true",
		}, docker.NoPrintOnError())
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				d.log.Info("--> UTE interface already exists onchain", "code", spec.Code, "name", ute.InterfaceName, "ip", uteIPStr)
				d.UserTunnelEndpointIPs[ute.InterfaceName] = uteIPStr
				continue
			}
			fmt.Println(string(out))
			return fmt.Errorf("failed to create UTE interface %s for device %s: %w", ute.InterfaceName, spec.Code, err)
		}

		// Wait for the UTE interface to exist onchain.
		serviceabilityClient, err := d.dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return fmt.Errorf("failed to get serviceability client: %w", err)
		}
		err = poll.Until(ctx, func() (bool, error) {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false, fmt.Errorf("failed to get program data: %w", err)
			}
			for _, device := range data.Devices {
				pk := solana.PublicKeyFromBytes(device.PubKey[:])
				if pk.String() == devicePK {
					for _, iface := range device.Interfaces {
						if iface.Name == ute.InterfaceName {
							return true, nil
						}
					}
				}
			}
			return false, nil
		}, 30*time.Second, 1*time.Second)
		if err != nil {
			return fmt.Errorf("failed to wait for UTE interface %s to exist onchain: %w", ute.InterfaceName, err)
		}

		d.UserTunnelEndpointIPs[ute.InterfaceName] = uteIPStr
		d.log.Info("--> Created UTE interface onchain", "code", spec.Code, "name", ute.InterfaceName, "ip", uteIPStr)
	}

	controllerAddr := net.JoinHostPort(d.dn.Controller.DefaultNetworkIP, fmt.Sprintf("%d", internalControllerPort))

	commandArgs := []string{
		"-controller", controllerAddr,
		"-pubkey", devicePK,
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

	telemetryMetricsPort, err := d.InternalTelemetryMetricsPort()
	if err != nil {
		return fmt.Errorf("failed to get internal metrics port: %w", err)
	}

	exposedPorts := []string{fmt.Sprintf("%d/tcp", internalEAPIHTTPPort)}
	if spec.Telemetry.MetricsEnable {
		exposedPorts = append(exposedPorts, fmt.Sprintf("%d/tcp", telemetryMetricsPort))
	}

	// Create the device container, but don't start it yet.
	req := testcontainers.ContainerRequest{
		Image: spec.ContainerImage,
		Name:  d.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = d.dockerContainerHostname()
		},
		ExposedPorts: exposedPorts,
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
		"-local-device-pubkey", devicePK,
	}
	if spec.Telemetry.MetricsEnable {
		telemetryCommandArgs = append(telemetryCommandArgs, "-metrics-enable")
		if spec.Telemetry.MetricsAddr != "" {
			telemetryCommandArgs = append(telemetryCommandArgs, "-metrics-addr", spec.Telemetry.MetricsAddr)
		}
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
		"TelemetryMetricsEnable":   spec.Telemetry.MetricsEnable,
		"TelemetryMetricsPort":     telemetryMetricsPort,
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
	d.log.Debug("==> Writing device config", "path", containerConfigPath)
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

	for _, additionalNetwork := range spec.AdditionalNetworks {
		err = d.dn.dockerClient.NetworkConnect(ctx, additionalNetwork, containerID, nil)
		if err != nil {
			return fmt.Errorf("failed to attach device %s to additional network %s: %w", spec.Code, additionalNetwork, err)
		}
	}

	// Wait for the device container to have status healthy.
	d.log.Debug("--> Waiting for device container to be healthy", "container", shortContainerID(containerID), "name", container.Name)
	start := time.Now()
	err = d.dn.waitContainerHealthy(ctx, containerID, 300*time.Second, 2*time.Second)
	if err != nil {
		// Collect diagnostics to help debug health check timeouts.
		fmt.Printf("=== DEVICE HEALTH CHECK TIMEOUT DIAGNOSTICS [%s] ===\n", spec.Code)

		diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer diagCancel()

		// Container state and health check log.
		inspect, inspectErr := d.dn.dockerClient.ContainerInspect(diagCtx, containerID)
		if inspectErr != nil {
			fmt.Printf("  container inspect: ERROR: %v\n", inspectErr)
		} else {
			fmt.Printf("  container state: status=%s exitCode=%d error=%q startedAt=%s\n",
				inspect.State.Status, inspect.State.ExitCode, inspect.State.Error, inspect.State.StartedAt)
			if inspect.State.Health != nil {
				fmt.Printf("  health check: status=%s failingStreak=%d\n",
					inspect.State.Health.Status, inspect.State.Health.FailingStreak)
				for i, entry := range inspect.State.Health.Log {
					fmt.Printf("  health log[%d]: exitCode=%d start=%s end=%s output=%q\n",
						i, entry.ExitCode, entry.Start, entry.End, entry.Output)
				}
			} else {
				fmt.Printf("  health check: no health state available\n")
			}
		}

		// Disk space on /var/tmp (core dumps can fill this up and prevent daemon starts).
		dfOut, dfErr := docker.Exec(diagCtx, d.dn.dockerClient, containerID, []string{"df", "-h", "/var/tmp"}, docker.NoPrintOnError())
		if dfErr != nil {
			fmt.Printf("  df /var/tmp: ERROR: %v\n", dfErr)
		} else {
			fmt.Printf("  df /var/tmp:\n%s\n", string(dfOut))
		}

		// Syslog (last 50 lines).
		syslog, syslogErr := docker.Exec(diagCtx, d.dn.dockerClient, containerID, []string{"tail", "-50", "/var/log/messages"}, docker.NoPrintOnError())
		if syslogErr != nil {
			fmt.Printf("  /var/log/messages: ERROR: %v\n", syslogErr)
		} else {
			fmt.Printf("  /var/log/messages (last 50 lines):\n%s\n", string(syslog))
		}

		fmt.Printf("=== END DEVICE HEALTH CHECK TIMEOUT DIAGNOSTICS [%s] ===\n", spec.Code)

		return fmt.Errorf("failed to wait for device container to be healthy: %w", err)
	}
	d.log.Debug("--> Device container is healthy", "container", shortContainerID(containerID), "cyoaNetworkIP", cyoaNetworkIP, "defaultNetworkIP", defaultNetworkIP, "name", container.Name, "duration", time.Since(start))

	// Configure InfluxDB telemetry via EOS `monitor telemetry influx` command.
	if d.dn.InfluxDB != nil && d.dn.InfluxDB.InternalURL != "" {
		d.log.Debug("--> Configuring InfluxDB telemetry", "url", d.dn.InfluxDB.InternalURL)

		// Configure EOS monitor telemetry influx destination.
		// Note: Not using vrf management - the default VRF should have connectivity
		// to the docker network where InfluxDB runs.
		influxConfig := fmt.Sprintf(`enable
configure
monitor telemetry influx
   destination influxdb DZ_INFLUX
      url %s
      database name %s
      retention policy autogen
   tag global dzd_pubkey %s
   tag global location %s
exit
exit
write memory
`, d.dn.InfluxDB.InternalURL, d.dn.InfluxDB.Database(), devicePK, spec.Exchange)

		_, err = docker.Exec(ctx, d.dn.dockerClient, containerID, []string{"Cli", "-c", influxConfig})
		if err != nil {
			return fmt.Errorf("failed to configure InfluxDB telemetry: %w", err)
		}
		d.log.Debug("--> InfluxDB telemetry configured")
	}

	// Set the component's state.
	err = d.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set device state: %w", err)
	}

	d.log.Debug("--> Device started", "container", d.ContainerID, "cyoaNetworkIP", cyoaNetworkIP, "defaultNetworkIP", defaultNetworkIP, "devicePK", devicePK)
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
	err = poll.Until(ctx, func() (bool, error) {
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
	port, err := d.dn.waitForContainerPortExposed(ctx, containerID, internalEAPIHTTPPort, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for EAPI HTTP port to be exposed: %w", err)
	}
	d.ExternalEAPIHTTPPort = port

	if d.Spec.Telemetry.Enabled && d.Spec.Telemetry.MetricsEnable {
		// Wait for telemetry metrics port to be exposed.
		telemetryMetricsPort, err := d.InternalTelemetryMetricsPort()
		if err != nil {
			return fmt.Errorf("failed to get internal metrics port: %w", err)
		}
		port, err := d.dn.waitForContainerPortExposed(ctx, containerID, telemetryMetricsPort, 10*time.Second)
		if err != nil {
			return fmt.Errorf("failed to wait for telemetry metrics port to be exposed: %w", err)
		}
		d.ExternalTelemetryMetricsPort = port
	}

	d.ContainerID = shortContainerID(container.ID)
	d.ID = onchainID
	d.CYOANetworkIP = ip

	// MaxUserTunnelSlots is now a constant from config package
	d.log.Debug("--> Using MaxUserTunnelSlots constant", "maxUsers", controllerconfig.MaxUserTunnelSlots)

	return nil
}

func (d *Device) InternalTelemetryMetricsPort() (int, error) {
	port := 0
	if d.Spec.Telemetry.MetricsEnable && d.Spec.Telemetry.MetricsAddr != "" {
		var err error
		port, err = strconv.Atoi(strings.Split(d.Spec.Telemetry.MetricsAddr, ":")[1])
		if err != nil {
			return 0, fmt.Errorf("failed to get telemetry metrics port: %w", err)
		}
	}
	return port, nil
}

func (d *Device) GetEAPIHTTPClient() (*goeapi.Node, error) {
	return goeapi.Connect("http", d.dn.ExternalHost, "admin", "admin", d.ExternalEAPIHTTPPort)
}

func (d *Device) GetTelemetryMetricsClient() *prometheus.MetricsClient {
	return prometheus.NewMetricsClient(fmt.Sprintf("http://%s:%d/metrics", d.dn.ExternalHost, d.ExternalTelemetryMetricsPort))
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
