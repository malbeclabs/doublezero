package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NATGatewaySpec defines the configuration for a NAT gateway container.
type NATGatewaySpec struct {
	// Code is a unique identifier for this NAT gateway.
	Code string

	// BehindNATNetworkIPHostID is the host ID for the gateway's IP on the private (behind-NAT) network.
	BehindNATNetworkIPHostID uint32

	// CYOANetworkIPHostID is the host ID for the gateway's IP on the public (CYOA) network.
	// This is the "public" IP that clients behind the NAT will appear as.
	CYOANetworkIPHostID uint32
}

// NATGateway represents a NAT gateway container that provides 1:1 NAT for clients
// on a private network to reach the CYOA network.
type NATGateway struct {
	dn  *Devnet
	log *slog.Logger

	Spec *NATGatewaySpec

	// BehindNATNetwork is the private network that clients behind NAT connect to.
	BehindNATNetwork *BehindNATNetwork

	ContainerID string

	// BehindNATNetworkIP is the gateway's IP on the private network (e.g., 10.255.0.1).
	BehindNATNetworkIP string

	// CYOANetworkIP is the gateway's IP on the CYOA network (e.g., 9.x.x.100).
	// Clients behind NAT will appear to have this IP.
	CYOANetworkIP string
}

// SetDevnet sets the devnet reference and logger for the NAT gateway.
// This is needed when creating the gateway outside of devnet.AddNATGateway().
func (g *NATGateway) SetDevnet(dn *Devnet, log *slog.Logger) {
	g.dn = dn
	g.log = log
}

func (g *NATGateway) dockerContainerHostname() string {
	return "nat-gateway-" + g.Spec.Code
}

func (g *NATGateway) dockerContainerName() string {
	return g.dn.Spec.DeployID + "-" + g.dockerContainerHostname()
}

// Exists checks if the NAT gateway container exists.
func (g *NATGateway) Exists(ctx context.Context) (bool, error) {
	containers, err := g.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", g.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+g.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

// StartIfNotRunning creates and starts the NAT gateway container if it's not already running.
func (g *NATGateway) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := g.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if NAT gateway exists: %w", err)
	}
	if exists {
		container, err := g.dn.dockerClient.ContainerInspect(ctx, g.dockerContainerName())
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}

		if container.State.Running {
			g.log.Info("--> NAT gateway already running", "container", shortContainerID(container.ID))
			err = g.setState(ctx, container.ID)
			if err != nil {
				return false, fmt.Errorf("failed to set NAT gateway state: %w", err)
			}
			return true, nil
		}

		g.log.Info("--> NAT gateway exists but not running, starting it")
		err = g.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to start NAT gateway: %w", err)
		}
		err = g.setState(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("failed to set NAT gateway state: %w", err)
		}
		return true, nil
	}
	return false, g.Start(ctx)
}

// Start creates and starts the NAT gateway container.
// Following device.go pattern: start with default network only, then attach
// additional networks after container starts to avoid "Address already in use" errors.
func (g *NATGateway) Start(ctx context.Context) error {
	g.log.Info("==> Starting NAT gateway")

	// Calculate the IPs for both networks.
	behindNATIPNet, err := netutil.DeriveIPFromCIDR(g.BehindNATNetwork.SubnetCIDR, g.Spec.BehindNATNetworkIPHostID)
	if err != nil {
		return fmt.Errorf("failed to calculate behind-NAT IP: %w", err)
	}
	behindNATIP := behindNATIPNet.String()

	cyoaIPNet, err := netutil.DeriveIPFromCIDR(g.dn.CYOANetwork.SubnetCIDR, g.Spec.CYOANetworkIPHostID)
	if err != nil {
		return fmt.Errorf("failed to calculate CYOA IP: %w", err)
	}
	cyoaIP := cyoaIPNet.String()

	g.log.Info("--> NAT gateway IPs", "behindNATIP", behindNATIP, "cyoaIP", cyoaIP)

	// Create the container with only the default network attached.
	// Additional networks are attached after the container starts.
	req := testcontainers.ContainerRequest{
		Image: "ubuntu:24.04",
		Name:  g.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = g.dockerContainerHostname()
		},
		Cmd: []string{
			"/bin/bash", "-c",
			// Install iptables, enable IP forwarding, disable rp_filter, and keep the container running.
			"apt-get update -qq && apt-get install -y -qq iptables iproute2 iputils-ping tcpdump > /dev/null && " +
				"echo 1 > /proc/sys/net/ipv4/ip_forward && " +
				"echo 0 > /proc/sys/net/ipv4/conf/all/rp_filter && " +
				"echo 0 > /proc/sys/net/ipv4/conf/default/rp_filter && " +
				"for i in /proc/sys/net/ipv4/conf/*/rp_filter; do echo 0 > $i 2>/dev/null || true; done && " +
				"tail -f /dev/null",
		},
		Networks: []string{
			g.dn.DefaultNetwork.Name,
		},
		Privileged: true,
		Labels:     g.dn.labels,
		WaitingFor: wait.ForExec([]string{"which", "iptables"}).
			WithStartupTimeout(2 * time.Minute).
			WithPollInterval(1 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to create NAT gateway: %w", err)
	}
	containerID := container.GetContainerID()

	// Attach the behind-NAT network with specific IP.
	err = g.dn.dockerClient.NetworkConnect(ctx, g.BehindNATNetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: behindNATIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: behindNATIP,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to attach NAT gateway to behind-NAT network: %w", err)
	}

	// Attach the CYOA network with specific IP.
	err = g.dn.dockerClient.NetworkConnect(ctx, g.dn.CYOANetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: cyoaIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: cyoaIP,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to attach NAT gateway to CYOA network: %w", err)
	}

	err = g.setState(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to set NAT gateway state: %w", err)
	}

	// Add iptables rules on the HOST to allow traffic from the behind-NAT network.
	// Docker's bridge-nf-call-iptables causes bridged traffic to be filtered by the host's
	// iptables FORWARD chain. By default, Docker only allows traffic TO/FROM a network's
	// subnet, not THROUGH it. We need to add explicit rules to allow forwarding.
	// Run this using a privileged exec that can access the host's network namespace.
	hostIptablesContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:       "ubuntu:24.04",
			Cmd:         []string{"/bin/bash", "-c", "apt-get update -qq && apt-get install -y -qq iptables > /dev/null && sleep 30"},
			Privileged:  true,
			NetworkMode: "host",
			WaitingFor:  wait.ForExec([]string{"which", "iptables"}).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		g.log.Warn("--> Failed to create host iptables helper container", "error", err)
	} else {
		defer func() {
			_ = hostIptablesContainer.Terminate(ctx)
		}()

		// Add iptables rules on the host to accept forwarded traffic from/to the behind-NAT subnet.
		// Docker uses iptables-legacy, so we need to use that as well.
		hostContainerID := hostIptablesContainer.GetContainerID()

		// Add to FORWARD chain using iptables-legacy (Docker uses legacy tables).
		forwardAcceptCmd := []string{"iptables-legacy", "-I", "FORWARD", "1", "-s", g.BehindNATNetwork.SubnetCIDR, "-j", "ACCEPT"}
		_, err = docker.Exec(ctx, g.dn.dockerClient, hostContainerID, forwardAcceptCmd)
		if err != nil {
			g.log.Warn("--> Failed to add host FORWARD rule for source (legacy)", "error", err)
		} else {
			g.log.Info("--> Added host FORWARD ACCEPT rule for source (legacy)", "subnet", g.BehindNATNetwork.SubnetCIDR)
		}

		forwardAcceptCmd2 := []string{"iptables-legacy", "-I", "FORWARD", "1", "-d", g.BehindNATNetwork.SubnetCIDR, "-j", "ACCEPT"}
		_, err = docker.Exec(ctx, g.dn.dockerClient, hostContainerID, forwardAcceptCmd2)
		if err != nil {
			g.log.Warn("--> Failed to add host FORWARD rule for dest (legacy)", "error", err)
		} else {
			g.log.Info("--> Added host FORWARD ACCEPT rule for dest (legacy)", "subnet", g.BehindNATNetwork.SubnetCIDR)
		}

		// Also try DOCKER-USER chain if it exists.
		dockerUserCmd1 := []string{"iptables-legacy", "-I", "DOCKER-USER", "1", "-s", g.BehindNATNetwork.SubnetCIDR, "-j", "ACCEPT"}
		_, err = docker.Exec(ctx, g.dn.dockerClient, hostContainerID, dockerUserCmd1)
		if err != nil {
			g.log.Debug("--> DOCKER-USER chain not available", "error", err)
		} else {
			g.log.Info("--> Added DOCKER-USER ACCEPT rule for source", "subnet", g.BehindNATNetwork.SubnetCIDR)
		}

		dockerUserCmd2 := []string{"iptables-legacy", "-I", "DOCKER-USER", "1", "-d", g.BehindNATNetwork.SubnetCIDR, "-j", "ACCEPT"}
		_, err = docker.Exec(ctx, g.dn.dockerClient, hostContainerID, dockerUserCmd2)
		if err != nil {
			g.log.Debug("--> DOCKER-USER chain not available", "error", err)
		} else {
			g.log.Info("--> Added DOCKER-USER ACCEPT rule for dest", "subnet", g.BehindNATNetwork.SubnetCIDR)
		}

		// Debug: show host's FORWARD chain rules (legacy).
		fwdRulesOut, _ := docker.Exec(ctx, g.dn.dockerClient, hostContainerID, []string{"iptables-legacy", "-L", "FORWARD", "-n", "-v", "--line-numbers"})
		g.log.Info("--> Host FORWARD chain rules (legacy)", "output", string(fwdRulesOut))

		// Check if bridge-nf-call-iptables is enabled.
		brNfOut, _ := docker.Exec(ctx, g.dn.dockerClient, hostContainerID, []string{"cat", "/proc/sys/net/bridge/bridge-nf-call-iptables"})
		g.log.Info("--> Host bridge-nf-call-iptables", "value", strings.TrimSpace(string(brNfOut)))

		// Try enabling it if not already.
		_, _ = docker.Exec(ctx, g.dn.dockerClient, hostContainerID, []string{"sh", "-c", "echo 1 > /proc/sys/net/bridge/bridge-nf-call-iptables"})
	}

	// Set up basic MASQUERADE for the entire private subnet.
	// This allows clients to reach external networks before per-client NAT rules are configured.
	// Use iptables-legacy because Docker networking uses legacy tables on Ubuntu 24.04.
	masqCmd := fmt.Sprintf("iptables-legacy -t nat -A POSTROUTING -s %s -j MASQUERADE", g.BehindNATNetwork.SubnetCIDR)
	_, err = g.Exec(ctx, strings.Split(masqCmd, " "))
	if err != nil {
		return fmt.Errorf("failed to set up MASQUERADE: %w", err)
	}

	// Allow forwarding.
	_, err = g.Exec(ctx, []string{"iptables-legacy", "-A", "FORWARD", "-j", "ACCEPT"})
	if err != nil {
		return fmt.Errorf("failed to enable forwarding: %w", err)
	}

	// Debug: print iptables rules, routing table, and IP forwarding status.
	iptablesOut, _ := g.Exec(ctx, []string{"iptables-legacy", "-t", "nat", "-L", "-n", "-v"})
	g.log.Info("--> NAT gateway iptables NAT rules", "output", string(iptablesOut))
	iptablesFilterOut, _ := g.Exec(ctx, []string{"iptables-legacy", "-L", "-n", "-v"})
	g.log.Info("--> NAT gateway iptables filter rules", "output", string(iptablesFilterOut))
	routeOut, _ := g.Exec(ctx, []string{"ip", "route", "show"})
	g.log.Info("--> NAT gateway routing table", "output", string(routeOut))
	ipAddrOut, _ := g.Exec(ctx, []string{"ip", "addr", "show"})
	g.log.Info("--> NAT gateway interfaces", "output", string(ipAddrOut))
	ipFwdOut, _ := g.Exec(ctx, []string{"cat", "/proc/sys/net/ipv4/ip_forward"})
	g.log.Info("--> NAT gateway IP forwarding status", "output", string(ipFwdOut))
	rpFilterOut, _ := g.Exec(ctx, []string{"sh", "-c", "cat /proc/sys/net/ipv4/conf/*/rp_filter"})
	g.log.Info("--> NAT gateway rp_filter values", "output", string(rpFilterOut))

	// Try to ping the ledger from NAT gateway (uses Docker DNS via DefaultNetwork).
	ledgerHostname := g.dn.Ledger.dockerContainerHostname()
	pingOut, _ := g.Exec(ctx, []string{"ping", "-c", "2", ledgerHostname})
	g.log.Info("--> NAT gateway ping to ledger", "ledgerHostname", ledgerHostname, "output", string(pingOut))

	g.log.Info("--> NAT gateway started", "container", g.ContainerID, "behindNATIP", g.BehindNATNetworkIP, "cyoaIP", g.CYOANetworkIP)

	return nil
}

func (g *NATGateway) setState(ctx context.Context, containerID string) error {
	g.ContainerID = shortContainerID(containerID)

	// Get the IPs from the container's network settings.
	container, err := g.dn.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	for networkName, networkSettings := range container.NetworkSettings.Networks {
		if networkName == g.BehindNATNetwork.Name {
			g.BehindNATNetworkIP = networkSettings.IPAddress
		}
		if networkName == g.dn.CYOANetwork.Name {
			g.CYOANetworkIP = networkSettings.IPAddress
		}
	}

	return nil
}

// ConfigureNATForClient sets up 1:1 NAT rules for a client behind the NAT gateway.
// clientPrivateIP is the client's IP on the behind-NAT network.
// The client will appear as the gateway's CYOA IP to the outside world.
func (g *NATGateway) ConfigureNATForClient(ctx context.Context, clientPrivateIP string) error {
	g.log.Info("==> Configuring NAT for client", "clientPrivateIP", clientPrivateIP, "publicIP", g.CYOANetworkIP)

	// Find the interface names for each network.
	// We need to figure out which interface is connected to which network.
	behindNATInterface, cyoaInterface, err := g.getInterfaceNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to get interface names: %w", err)
	}

	g.log.Info("--> Interface mapping", "behindNATInterface", behindNATInterface, "cyoaInterface", cyoaInterface)

	// Set up SNAT: Packets from client going out to CYOA network get source IP rewritten.
	// Insert at the beginning so it takes precedence over the general MASQUERADE rule.
	// Use iptables-legacy because Docker networking uses legacy tables on Ubuntu 24.04.
	snatCmd := fmt.Sprintf("iptables-legacy -t nat -I POSTROUTING 1 -s %s -o %s -j SNAT --to-source %s",
		clientPrivateIP, cyoaInterface, g.CYOANetworkIP)

	// Set up DNAT: Packets coming in to gateway's CYOA IP get destination rewritten to client.
	dnatCmd := fmt.Sprintf("iptables-legacy -t nat -A PREROUTING -d %s -i %s -j DNAT --to-destination %s",
		g.CYOANetworkIP, cyoaInterface, clientPrivateIP)

	// Execute the commands.
	for _, cmd := range []string{snatCmd, dnatCmd} {
		_, err := g.Exec(ctx, strings.Split(cmd, " "))
		if err != nil {
			return fmt.Errorf("failed to execute iptables command %q: %w", cmd, err)
		}
	}

	g.log.Info("--> NAT configured for client", "clientPrivateIP", clientPrivateIP, "publicIP", g.CYOANetworkIP)

	return nil
}

// getInterfaceNames returns the interface names for the behind-NAT and CYOA networks.
func (g *NATGateway) getInterfaceNames(ctx context.Context) (behindNATInterface, cyoaInterface string, err error) {
	// Get the interface that has the behind-NAT IP.
	output, err := g.Exec(ctx, []string{"ip", "-o", "addr", "show"})
	if err != nil {
		return "", "", fmt.Errorf("failed to get interface info: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, g.BehindNATNetworkIP+"/") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				behindNATInterface = fields[1]
			}
		}
		if strings.Contains(line, g.CYOANetworkIP+"/") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				cyoaInterface = fields[1]
			}
		}
	}

	if behindNATInterface == "" {
		return "", "", fmt.Errorf("could not find interface for behind-NAT IP %s", g.BehindNATNetworkIP)
	}
	if cyoaInterface == "" {
		return "", "", fmt.Errorf("could not find interface for CYOA IP %s", g.CYOANetworkIP)
	}

	return behindNATInterface, cyoaInterface, nil
}

// Exec executes a command in the NAT gateway container.
func (g *NATGateway) Exec(ctx context.Context, cmd []string, opts ...docker.ExecOption) ([]byte, error) {
	return docker.Exec(ctx, g.dn.dockerClient, g.ContainerID, cmd, opts...)
}

// AddCYOARouteOnClient adds a route for the CYOA network via the NAT gateway.
// This preserves the default route (needed for controller connectivity) while
// routing CYOA network traffic through the NAT gateway.
func (g *NATGateway) AddCYOARouteOnClient(ctx context.Context, clientContainerID string) error {
	// Add a route for the CYOA network via the NAT gateway.
	cmd := []string{"ip", "route", "add", g.dn.CYOANetwork.SubnetCIDR, "via", g.BehindNATNetworkIP}
	_, err := docker.Exec(ctx, g.dn.dockerClient, clientContainerID, cmd)
	if err != nil {
		return fmt.Errorf("failed to add CYOA route on client: %w", err)
	}
	return nil
}
