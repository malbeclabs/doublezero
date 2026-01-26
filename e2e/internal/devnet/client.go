package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
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

	// CYOANetworkIPHostID is the offset into the host portion of the subnet (must be < 2^(32 - prefixLen)).
	// This is ignored if BehindNATGateway is set.
	CYOANetworkIPHostID uint32

	// BehindNATGateway is an optional reference to a NAT gateway. If set, the client will connect
	// to the NAT gateway's private network instead of the CYOA network directly.
	BehindNATGateway *NATGateway

	// BehindNATNetworkIPHostID is the host ID for the client's IP on the private (behind-NAT) network.
	// Only used if BehindNATGateway is set.
	BehindNATNetworkIPHostID uint32
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

	// Clients behind NAT get their public IP from the NAT gateway, so they don't need CYOANetworkIPHostID.
	// Instead, validate the BehindNATNetworkIPHostID.
	if s.BehindNATGateway != nil {
		if s.BehindNATNetworkIPHostID <= 0 || s.BehindNATNetworkIPHostID >= 255 {
			return fmt.Errorf("BehindNATNetworkIPHostID %d is out of valid range (1 to 254)", s.BehindNATNetworkIPHostID)
		}
		return nil
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

	// PrivateIP is the client's actual IP on its connected network.
	// For clients behind NAT, this is the private IP on the BehindNATNetwork.
	// For direct clients, this equals CYOANetworkIP.
	PrivateIP string
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
			c.log.Info("--> Client already running", "container", shortContainerID(container.ID))

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
	c.log.Info("==> Starting client", "image", c.Spec.ContainerImage, "cyoaNetworkIPHostID", c.Spec.CYOANetworkIPHostID)

	// Determine network configuration based on whether client is behind NAT.
	var clientNetworkName string
	var clientNetworkIP string
	var publicIP string // The IP the client will register as (CYOANetworkIP)
	var networks []string

	if c.Spec.BehindNATGateway != nil {
		// Client is behind NAT. External traffic to the CYOA network goes through the NAT gateway.
		natGW := c.Spec.BehindNATGateway
		clientNetworkName = natGW.BehindNATNetwork.Name

		privateIP, err := netutil.DeriveIPFromCIDR(natGW.BehindNATNetwork.SubnetCIDR, c.Spec.BehindNATNetworkIPHostID)
		if err != nil {
			return fmt.Errorf("failed to derive behind-NAT network IP: %w", err)
		}
		clientNetworkIP = privateIP.To4().String()
		publicIP = natGW.CYOANetworkIP

		c.log.Info("--> Client behind NAT", "privateIP", clientNetworkIP, "publicIP", publicIP)
	} else {
		// Client connects directly to CYOA network.
		clientNetworkName = c.dn.CYOANetwork.Name

		cyoaIP, err := netutil.DeriveIPFromCIDR(c.dn.CYOANetwork.SubnetCIDR, uint32(c.Spec.CYOANetworkIPHostID))
		if err != nil {
			return fmt.Errorf("failed to derive CYOA network IP: %w", err)
		}
		clientNetworkIP = cyoaIP.To4().String()
		publicIP = clientNetworkIP
	}

	// All clients connect to DefaultNetwork (for Docker DNS) and their specific network.
	networks = []string{c.dn.DefaultNetwork.Name, clientNetworkName}

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
		Networks: networks,
		EndpointSettingsModifier: func(m map[string]*network.EndpointSettings) {
			if m[clientNetworkName] == nil {
				m[clientNetworkName] = &network.EndpointSettings{}
			}
			m[clientNetworkName].IPAddress = clientNetworkIP
			m[clientNetworkName].IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: clientNetworkIP,
			}
		},
		Privileged: true,
		Labels:     c.dn.labels,
	}

	c.CYOANetworkIP = publicIP
	c.PrivateIP = clientNetworkIP
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

	// For NAT clients, configure routing to send CYOA network traffic through the NAT gateway.
	if c.Spec.BehindNATGateway != nil {
		natGW := c.Spec.BehindNATGateway
		c.log.Info("--> Configuring NAT client routing", "natGatewayIP", natGW.BehindNATNetworkIP)

		// Add route to CYOA network via NAT gateway.
		// This ensures GRE tunnel outer packets reach the devices through NAT.
		_, err = c.Exec(ctx, []string{"ip", "route", "add", c.dn.CYOANetwork.SubnetCIDR, "via", natGW.BehindNATNetworkIP})
		if err != nil {
			return fmt.Errorf("failed to add route to CYOA network via NAT gateway: %w", err)
		}
		c.log.Info("--> NAT client routing configured", "cyoaNetwork", c.dn.CYOANetwork.SubnetCIDR, "via", natGW.BehindNATNetworkIP)
	}

	// Fund the client account via airdrop.
	// Retry a couple times to avoid the observed intermittent failures, even on the first airdrop request.
	funded := false
	var output []byte
	for range 3 {
		c.log.Info("--> Funding client account", "clientPubkey", c.Pubkey)
		output, err = c.Exec(ctx, []string{"solana", "airdrop", "10", c.Pubkey}, docker.NoPrintOnError())
		if err != nil {
			if strings.Contains(string(output), "rate limit") {
				c.log.Info("--> Solana airdrop request failed with rate limit message, retrying")
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Println(string(output))
			return fmt.Errorf("failed to fund client account: %w", err)
		}
		funded = true
		break
	}
	if !funded {
		fmt.Println(string(output))
		return fmt.Errorf("failed to fund client account after 3 attempts")
	}

	c.log.Info("--> Client started", "container", c.ContainerID, "pubkey", c.Pubkey, "publicIP", c.CYOANetworkIP, "privateIP", c.PrivateIP)
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

	// Determine which network the client is connected to.
	var clientNetworkName string
	if c.Spec.BehindNATGateway != nil {
		clientNetworkName = c.Spec.BehindNATGateway.BehindNATNetwork.Name
	} else {
		clientNetworkName = c.dn.CYOANetwork.Name
	}

	// Wait for the client's network IP address to be assigned.
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
		if container.NetworkSettings.Networks[clientNetworkName] == nil {
			if !loggedWait && attempts > 1 {
				c.log.Debug("--> Waiting for client network IP to be assigned", "container", shortContainerID(container.ID), "network", clientNetworkName, "timeout", timeout)
				loggedWait = true
			}
			return false, nil
		}
		return true, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to get client network IP: %w", err)
	}

	// Get the client's network IP address.
	if container.NetworkSettings.Networks[clientNetworkName] == nil {
		return fmt.Errorf("failed to get client network IP")
	}
	c.PrivateIP = container.NetworkSettings.Networks[clientNetworkName].IPAddress

	// Set the public IP (CYOANetworkIP).
	if c.Spec.BehindNATGateway != nil {
		// For NAT clients, the public IP is the NAT gateway's CYOA IP.
		c.CYOANetworkIP = c.Spec.BehindNATGateway.CYOANetworkIP
	} else {
		// For direct clients, the public IP equals the private IP.
		c.CYOANetworkIP = c.PrivateIP
	}

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

type ClientStatusResponse struct {
	TunnelName       string         `json:"tunnel_name"`
	TunnelSrc        net.IP         `json:"tunnel_src"`
	TunnelDst        net.IP         `json:"tunnel_dst"`
	DoubleZeroIP     net.IP         `json:"doublezero_ip"`
	DoubleZeroStatus ClientSession  `json:"doublezero_status"`
	UserType         ClientUserType `json:"user_type"`
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

func (c *Client) WaitForTunnelDisconnected(ctx context.Context, timeout time.Duration) error {
	return c.WaitForTunnelStatus(ctx, ClientSessionStatusDisconnected, timeout)
}

func (c *Client) WaitForTunnelStatus(ctx context.Context, wantStatus ClientSessionStatus, timeout time.Duration) error {
	c.log.Info("==> Waiting for client tunnel status", "wantStatus", wantStatus, "timeout", timeout)

	attempts := 0
	start := time.Now()
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.GetTunnelStatus(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to get client status: %w", err)
		}

		for _, s := range resp {
			if s.DoubleZeroStatus.SessionStatus == wantStatus {
				c.log.Info("✅ Got expected client tunnel status", "wantStatus", wantStatus, "duration", time.Since(start))
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
		return fmt.Errorf("failed to wait for client tunnel status %s: %w", wantStatus, err)
	}

	return nil
}

func (c *Client) WaitForLatencyResults(ctx context.Context, wantDevicePK string, timeout time.Duration) error {
	c.log.Info("==> Waiting for latency results (timeout " + timeout.String() + ")")

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
					c.log.Info("✅ Got expected latency results", "wantDevicePK", wantDevicePK, "duration", time.Since(start))
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
