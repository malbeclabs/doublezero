package devnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
)

type ClientSpec struct {
	ContainerImage string
	KeypairPath    string

	// Route liveness passive/active mode flags.
	// TODO(snormore): These flags are temporary for initial rollout testing.
	// They will be superceded by a single `route-liveness-enable` flag, where false means passive-mode
	// and true means active-mode.
	RouteLivenessEnablePassive bool
	RouteLivenessEnableActive  bool

	// RouteLivenessEnable is a flag to enable or disable route liveness. False puts the system in
	// passive-mode, and true puts it in active-mode.
	// RouteLivenessEnable bool

	// RouteLivenessPeerMetrics is a flag to enable or disable per per-peer metrics for route
	// liveness (high cardinality).
	RouteLivenessPeerMetrics bool

	// RouteLivenessDebug is a flag to enable or disable debug logging for route liveness.
	RouteLivenessDebug bool

	// LatencyProbeTunnelEndpoints enables probing UserTunnelEndpoint interfaces
	// in addition to device PublicIp during latency measurements.
	LatencyProbeTunnelEndpoints bool

	// CYOANetworkIPHostID is the offset into the host portion of the subnet (must be < 2^(32 - prefixLen)).
	CYOANetworkIPHostID uint32
}

func (s *ClientSpec) Validate(cyoaNetworkSpec CYOANetworkSpec) error {
	// If the container image is not set, use the DZ_CLIENT_IMAGE environment variable.
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_CLIENT_IMAGE")
	}

	// Check that the keypair path is valid and exists.
	if s.KeypairPath == "" {
		return fmt.Errorf("keypair path is required")
	}
	if _, err := os.Stat(s.KeypairPath); os.IsNotExist(err) {
		return fmt.Errorf("keypair path %s does not exist", s.KeypairPath)
	}

	// Validate that hostID does not select the network (0) or broadcast (max) address.
	hostBits := 32 - cyoaNetworkSpec.CIDRPrefix
	maxHostID := uint32((1 << hostBits) - 1)
	if s.CYOANetworkIPHostID <= 0 || s.CYOANetworkIPHostID >= maxHostID {
		return fmt.Errorf("hostID %d is out of valid range (1 to %d)", s.CYOANetworkIPHostID, maxHostID-1)
	}

	return nil
}

type Client struct {
	dn   *Devnet
	log  *slog.Logger
	Spec *ClientSpec

	ContainerID   string
	Pubkey        string
	CYOANetworkIP string
}

func (c *Client) dockerContainerHostname() string {
	return "client-" + c.Pubkey
}

func (c *Client) dockerContainerName() string {
	return c.dn.Spec.DeployID + "-" + c.dockerContainerHostname()
}

// Exists checks if the ledger container exists.
func (c *Client) Exists(ctx context.Context) (bool, error) {
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

// StartIfNotRunning creates and starts the client container if it's not already running.
func (c *Client) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := c.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if client exists: %w", err)
	}
	if exists {
		container, err := c.dn.dockerClient.ContainerInspect(ctx, c.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		// Check if the container is running.
		if container.State.Running {
			c.log.Debug("--> Client already running", "container", shortContainerID(container.ID))

			// Set the component's state.
			err = c.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set client state: %w", err)
			}

			return false, nil
		}

		// Otherwise, start the container.
		err = c.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start client: %w", err)
		}

		// Set the component's state.
		err = c.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set client state: %w", err)
		}

		return true, nil
	}

	return false, c.Start(ctx)
}

func (c *Client) Start(ctx context.Context) error {
	c.log.Debug("==> Starting client", "image", c.Spec.ContainerImage, "cyoaNetworkIPHostID", c.Spec.CYOANetworkIPHostID)

	cyoaIP, err := netutil.DeriveIPFromCIDR(c.dn.CYOANetwork.SubnetCIDR, uint32(c.Spec.CYOANetworkIPHostID))
	if err != nil {
		return fmt.Errorf("failed to derive CYOA network IP: %w", err)
	}
	clientCYOAIP := cyoaIP.To4().String()

	// Read the client keypair.
	keypairJSON, err := os.ReadFile(c.Spec.KeypairPath)
	if err != nil {
		return fmt.Errorf("failed to read client keypair: %w", err)
	}

	// Get the client's keypair pubkey.
	pubkey, err := solana.PubkeyFromKeypairJSON(keypairJSON)
	if err != nil {
		return fmt.Errorf("failed to parse client pubkey: %w", err)
	}
	// We need to set this here because dockerContainerName and dockerContainerHostname use it.
	c.Pubkey = pubkey

	extraArgs := []string{}
	if c.Spec.RouteLivenessEnablePassive {
		extraArgs = append(extraArgs, "-route-liveness-enable-passive")
	} else {
		extraArgs = append(extraArgs, "-route-liveness-enable-passive=false")
	}
	if c.Spec.RouteLivenessEnableActive {
		extraArgs = append(extraArgs, "-route-liveness-enable-active")
	} else {
		extraArgs = append(extraArgs, "-route-liveness-enable-active=false")
	}
	if c.Spec.RouteLivenessPeerMetrics {
		extraArgs = append(extraArgs, "-route-liveness-peer-metrics")
	}
	if c.Spec.RouteLivenessDebug {
		extraArgs = append(extraArgs, "-route-liveness-debug")
	}
	if c.Spec.LatencyProbeTunnelEndpoints {
		extraArgs = append(extraArgs, "-latency-probe-tunnel-endpoints")
	}

	// Start the client container.
	req := testcontainers.ContainerRequest{
		Image: c.Spec.ContainerImage,
		Name:  c.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = c.dockerContainerHostname()
		},
		Env: map[string]string{
			"DZ_LEDGER_URL":                c.dn.Ledger.InternalRPCURL,
			"DZ_LEDGER_WS":                 c.dn.Ledger.InternalRPCWSURL,
			"DZ_SERVICEABILITY_PROGRAM_ID": c.dn.Manager.ServiceabilityProgramID,
			"DZ_CLIENT_EXTRA_ARGS":         strings.Join(extraArgs, " "),
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      c.Spec.KeypairPath,
				ContainerFilePath: containerDoublezeroKeypairPath,
			},
			{
				HostFilePath:      c.Spec.KeypairPath,
				ContainerFilePath: containerSolanaKeypairPath,
			},
		},
		Networks: []string{
			c.dn.DefaultNetwork.Name,
			c.dn.CYOANetwork.Name,
		},
		EndpointSettingsModifier: func(m map[string]*network.EndpointSettings) {
			if m[c.dn.CYOANetwork.Name] == nil {
				m[c.dn.CYOANetwork.Name] = &network.EndpointSettings{}
			}
			m[c.dn.CYOANetwork.Name].IPAddress = clientCYOAIP
			m[c.dn.CYOANetwork.Name].IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: clientCYOAIP,
			}
		},
		Privileged: true,
		Labels:     c.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}

	// Set the component's state.
	err = c.setState(ctx, container.GetContainerID())
	if err != nil {
		return fmt.Errorf("failed to set client state: %w", err)
	}

	// Fund the client account via airdrop.
	// Retry a few times to avoid rate limit failures when running tests in parallel.
	// We explicitly pass the RPC URL via -u flag to avoid a race condition where the solana CLI
	// config may not be set up yet by the entrypoint script, which would cause it to default to mainnet.
	funded := false
	var output []byte
	for attempt := range 5 {
		c.log.Debug("--> Funding client account", "clientPubkey", c.Pubkey, "attempt", attempt+1)
		output, err = c.Exec(ctx, []string{"solana", "airdrop", "-u", c.dn.Ledger.InternalRPCURL, "10", c.Pubkey}, docker.NoPrintOnError())
		if err != nil {
			outputStr := string(output)
			if strings.Contains(outputStr, "429") || strings.Contains(outputStr, "Too Many Requests") || strings.Contains(outputStr, "rate limit") {
				c.log.Debug("--> Solana airdrop request rate limited, retrying", "attempt", attempt+1)
				time.Sleep(2 * time.Second)
				continue
			}
			fmt.Println(outputStr)
			return fmt.Errorf("failed to fund client account: %w", err)
		}
		funded = true
		break
	}
	if !funded {
		fmt.Println(string(output))
		return fmt.Errorf("failed to fund client account after 5 attempts")
	}

	c.log.Debug("--> Client started", "container", c.ContainerID, "pubkey", c.Pubkey, "cyoaIP", clientCYOAIP)
	return nil
}

func (c *Client) setState(ctx context.Context, containerID string) error {
	c.ContainerID = shortContainerID(containerID)

	// Get the client's public address.
	output, err := c.Exec(ctx, []string{"solana", "address"}, docker.NoPrintOnError())
	if err != nil {
		return fmt.Errorf("failed to get client public address: %w", err)
	}
	c.Pubkey = strings.TrimSpace(string(output))

	// Wait for the client's CYOA network IP address to be assigned.
	var loggedWait bool
	var attempts int
	timeout := 10 * time.Second
	var container dockercontainer.InspectResponse
	err = poll.Until(ctx, func() (bool, error) {
		attempts++
		var err error
		container, err = c.dn.dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}
		if container.NetworkSettings.Networks[c.dn.CYOANetwork.Name] == nil {
			if !loggedWait && attempts > 1 {
				c.log.Debug("--> Waiting for client CYOA network IP to be assigned", "container", shortContainerID(container.ID), "timeout", timeout)
				loggedWait = true
			}
			return false, nil
		}
		return true, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to get client CYOA network IP: %w", err)
	}

	// Get the client's CYOA network IP address.
	if container.NetworkSettings.Networks[c.dn.CYOANetwork.Name] == nil {
		return fmt.Errorf("failed to get client CYOA network IP")
	}
	c.CYOANetworkIP = container.NetworkSettings.Networks[c.dn.CYOANetwork.Name].IPAddress

	return nil
}

func (c *Client) Exec(ctx context.Context, command []string, options ...docker.ExecOption) ([]byte, error) {
	c.log.Debug("--> Executing command", "command", command)
	output, err := docker.Exec(ctx, c.dn.dockerClient, c.ContainerID, command, options...)
	if err != nil {
		// NOTE: We return the output here because it can contain useful information on error.
		return output, fmt.Errorf("failed to execute command from client: %w", err)
	}
	return output, nil
}

func (c *Client) ExecReturnJSONList(ctx context.Context, command []string, options ...docker.ExecOption) ([]map[string]any, error) {
	list, err := docker.ExecReturnJSONList(ctx, c.dn.dockerClient, c.ContainerID, command, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command from client: %w", err)
	}

	return list, nil
}

type ClientSessionStatus string

type ClientSession struct {
	SessionStatus     ClientSessionStatus `json:"session_status"`
	LastSessionUpdate int64               `json:"last_session_update"`
}

const (
	ClientSessionStatusUp           ClientSessionStatus = "BGP Session Up"
	ClientSessionStatusDown         ClientSessionStatus = "BGP Session Down"
	ClientSessionStatusDisconnected ClientSessionStatus = "disconnected"
)

type ClientUserType string

const (
	ClientUserTypeIBRL              ClientUserType = "IBRL"
	ClientUserTypeIBRLWithAllocated ClientUserType = "IBRLWithAllocatedIP"
	ClientUserTypeEdgeFiltering     ClientUserType = "EdgeFiltering"
	ClientUserTypeMulticast         ClientUserType = "Multicast"
)

type ClientStatusResponse struct {
	TunnelName       string         `json:"tunnel_name"`
	TunnelSrc        net.IP         `json:"tunnel_src"`
	TunnelDst        net.IP         `json:"tunnel_dst"`
	DoubleZeroIP     net.IP         `json:"doublezero_ip"`
	DoubleZeroStatus ClientSession  `json:"doublezero_status"`
	UserType         ClientUserType `json:"user_type"`
}

// CLIStatusResponse represents the full response from `doublezero status --json`,
// which includes additional fields like current_device and metro that are computed
// by the CLI based on on-chain data.
type CLIStatusResponse struct {
	Response            ClientStatusResponse `json:"response"`
	CurrentDevice       string               `json:"current_device"`
	LowestLatencyDevice string               `json:"lowest_latency_device"`
	Metro               string               `json:"metro"`
	Network             string               `json:"network"`
}

// GetCLIStatus retrieves the full status output from `doublezero status --json`,
// which includes current_device, metro, and other fields computed by the CLI.
// This is useful for verifying that the CLI correctly associates tunnels with devices.
func (c *Client) GetCLIStatus(ctx context.Context) ([]CLIStatusResponse, error) {
	output, err := c.Exec(ctx, []string{"doublezero", "status", "--json"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute doublezero status --json: %w", err)
	}

	var resp []CLIStatusResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal CLI status response: %w, output: %s", err, string(output))
	}

	return resp, nil
}

func (c *Client) GetTunnelStatus(ctx context.Context) ([]ClientStatusResponse, error) {
	resp, err := docker.ExecReturnObject[[]ClientStatusResponse](ctx, c.dn.dockerClient, c.ContainerID, []string{"curl", "-s", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/status"})
	if err != nil {
		return nil, fmt.Errorf("failed to get client tunnel status: %w", err)
	}

	return resp, nil
}

func (c *Client) WaitForTunnelUp(ctx context.Context, timeout time.Duration) error {
	return c.WaitForTunnelStatus(ctx, ClientSessionStatusUp, timeout)
}

// WaitForNTunnelsUp waits for N tunnels to be in the "up" state.
func (c *Client) WaitForNTunnelsUp(ctx context.Context, n int, timeout time.Duration) error {
<<<<<<< HEAD
	c.log.Debug("==> Waiting for N tunnels to be up", "n", n, "timeout", timeout)
=======
	c.log.Info("==> Waiting for N tunnels to be up", "n", n, "timeout", timeout)
>>>>>>> 42711d2f (DNM: feat(cli): remove multiple tunnel restriction (#2725))

	attempts := 0
	start := time.Now()
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.GetTunnelStatus(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get client status: %w", err)
		}

		upCount := 0
		for _, s := range resp {
			if s.DoubleZeroStatus.SessionStatus == ClientSessionStatusUp {
				upCount++
			}
		}

		if upCount >= n {
<<<<<<< HEAD
			c.log.Debug("✅ Got expected number of tunnels up", "n", n, "upCount", upCount, "duration", time.Since(start))
=======
			c.log.Info("✅ Got expected number of tunnels up", "n", n, "upCount", upCount, "duration", time.Since(start))
>>>>>>> 42711d2f (DNM: feat(cli): remove multiple tunnel restriction (#2725))
			return true, nil
		}

		if attempts == 1 || attempts%5 == 0 {
			c.log.Debug("--> Waiting for N tunnels up", "n", n, "upCount", upCount, "response", resp, "attempts", attempts)
		}
		attempts++

		return false, nil
	}, timeout, 1*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for %d tunnels to be up: %w", n, err)
	}

	return nil
}

func (c *Client) WaitForTunnelDisconnected(ctx context.Context, timeout time.Duration) error {
	return c.WaitForTunnelStatus(ctx, ClientSessionStatusDisconnected, timeout)
}

func (c *Client) WaitForTunnelStatus(ctx context.Context, wantStatus ClientSessionStatus, timeout time.Duration) error {
	c.log.Debug("==> Waiting for client tunnel status", "wantStatus", wantStatus, "timeout", timeout)

	attempts := 0
	start := time.Now()
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.GetTunnelStatus(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get client status: %w", err)
		}

		for _, s := range resp {
			if s.DoubleZeroStatus.SessionStatus == wantStatus {
				c.log.Debug("✅ Got expected client tunnel status", "wantStatus", wantStatus, "duration", time.Since(start))
				return true, nil
			}
		}

		if attempts == 1 || attempts%5 == 0 {
			c.log.Debug("--> Waiting for client tunnel status", "wantStatus", wantStatus, "response", resp, "attempts", attempts)
		}
		attempts++

		return false, nil
	}, timeout, 1*time.Second)
	if err != nil {
		c.dumpDiagnostics()
		return fmt.Errorf("failed to wait for client tunnel status %s: %w", wantStatus, err)
	}

	return nil
}

// dumpDiagnostics prints client-side and device-side diagnostic information to help debug
// tunnel status failures. It uses a fresh context since the test context may have expired.
// Output is buffered and written in a single fmt.Fprint call so that parallel tests don't
// interleave each other's diagnostics.
func (c *Client) dumpDiagnostics() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n=== DIAGNOSTIC DUMP (deploy=%s client=%s) ===\n", c.dn.Spec.DeployID, c.Pubkey)

	// Client-side diagnostics.
	clientCommands := []struct {
		label   string
		command []string
	}{
		{"doublezero status", []string{"curl", "-s", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/status"}},
		{"ip addr show", []string{"ip", "addr", "show"}},
		{"ip route show", []string{"ip", "route", "show"}},
	}
	for _, cmd := range clientCommands {
		output, err := c.Exec(ctx, cmd.command, docker.NoPrintOnError())
		if err != nil {
			fmt.Fprintf(&buf, "\n--- Client: %s (ERROR: %v)\n", cmd.label, err)
		} else {
			fmt.Fprintf(&buf, "\n--- Client: %s\n%s", cmd.label, string(output))
		}
	}

	// Dump doublezerod container logs (stdout/stderr).
	logsReader, err := c.dn.dockerClient.ContainerLogs(ctx, c.ContainerID, dockercontainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "100",
	})
	if err != nil {
		fmt.Fprintf(&buf, "\n--- Client: doublezerod container logs (ERROR: %v)\n", err)
	} else {
		var stdout, stderr bytes.Buffer
		_, _ = stdcopy.StdCopy(&stdout, &stderr, logsReader)
		logsReader.Close()
		fmt.Fprintf(&buf, "\n--- Client: doublezerod container logs (stdout)\n%s", stdout.String())
		if stderr.Len() > 0 {
			fmt.Fprintf(&buf, "\n--- Client: doublezerod container logs (stderr)\n%s", stderr.String())
		}
	}

	// Device-side diagnostics.
	for code, device := range c.dn.Devices {
		deviceCommands := []struct {
			label   string
			command []string
		}{
			{"show running-config section Tunnel", []string{"Cli", "-p", "15", "-c", "show running-config section Tunnel"}},
			{"show ip bgp summary", []string{"Cli", "-c", "show ip bgp summary"}},
			{"show ip bgp summary vrf vrf1", []string{"Cli", "-c", "show ip bgp summary vrf vrf1"}},
			{"doublezero-agent log (last 100 lines)", []string{"tail", "-100", "/var/log/agents-latest/doublezero-agent"}},
			{"disk space /var/tmp", []string{"df", "-h", "/var/tmp"}},
		}
		for _, cmd := range deviceCommands {
			output, err := device.Exec(ctx, cmd.command)
			if err != nil {
				fmt.Fprintf(&buf, "\n--- Device %s: %s (ERROR: %v)\n", code, cmd.label, err)
			} else {
				fmt.Fprintf(&buf, "\n--- Device %s: %s\n%s", code, cmd.label, string(output))
			}
		}
	}

	// Controller-side diagnostics: query the config the controller would send to each device.
	for code, device := range c.dn.Devices {
		output, err := docker.Exec(ctx, c.dn.dockerClient, c.dn.Controller.ContainerID, []string{
			"doublezero-controller", "agent",
			"-device-pubkey", device.ID,
			"-controller-addr", "localhost",
			"-controller-port", "7000",
		})
		if err != nil {
			fmt.Fprintf(&buf, "\n--- Controller config for device %s (ERROR: %v)\n", code, err)
		} else {
			fmt.Fprintf(&buf, "\n--- Controller config for device %s\n%s", code, string(output))
		}
	}

	fmt.Fprintf(&buf, "\n=== DIAGNOSTIC DUMP END ===\n")
	fmt.Fprint(os.Stderr, buf.String())
}

func (c *Client) WaitForLatencyResults(ctx context.Context, wantDevicePK string, timeout time.Duration) error {
	c.log.Debug("==> Waiting for latency results (timeout " + timeout.String() + ")")

	attempts := 0
	start := time.Now()
	err := poll.Until(ctx, func() (bool, error) {
		results, err := c.ExecReturnJSONList(ctx, []string{"curl", "-s", "--unix-socket", "/var/run/doublezerod/doublezerod.sock", "http://doublezero/latency"})
		if err != nil {
			return false, fmt.Errorf("failed to get latency results: %w", err)
		}

		if len(results) > 0 {
			for _, result := range results {
				if result["device_pk"] == wantDevicePK && result["reachable"] == true {
					c.log.Debug("✅ Got expected latency results", "wantDevicePK", wantDevicePK, "duration", time.Since(start))
					return true, nil
				}
			}
		}

		if attempts == 1 || attempts%5 == 0 {
			c.log.Debug("--> Waiting for latency results", "wantDevicePK", wantDevicePK, "results", results, "attempts", attempts)
		}
		attempts++

		return false, nil
	}, timeout, 1*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for latency results: %w", err)
	}

	return nil
}

// WaitForPing waits until the given IP is reachable via ICMP ping from the client container.
func (c *Client) WaitForPing(ctx context.Context, ip string, timeout time.Duration) error {
	c.log.Info("==> Waiting for ping to succeed", "ip", ip, "timeout", timeout)

	start := time.Now()
	err := poll.Until(ctx, func() (bool, error) {
		_, err := c.Exec(ctx, []string{"ping", "-c", "1", "-W", "1", ip})
		if err == nil {
			c.log.Info("✅ Ping succeeded", "ip", ip, "duration", time.Since(start))
			return true, nil
		}
		return false, nil
	}, timeout, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to ping %s within %s: %w", ip, timeout, err)
	}

	return nil
}
