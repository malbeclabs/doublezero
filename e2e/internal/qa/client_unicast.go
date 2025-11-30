package qa

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
)

const (
	connectUnicastTimeout     = 150 * time.Second
	unicastPingRequestTimeout = 60 * time.Second
	unicastPingProbeTimeout   = 5 * time.Second
	unicastTracerouteTimeout  = 5 * time.Second

	unicastPingProbeCount         = 5
	unicastPingProbeLossThreshold = 2
	unicastTracerouteCount        = 10

	unicastInterfaceName = "doublezero0"
)

func (c *Client) ConnectUserUnicast_AnyDevice(ctx context.Context, waitForStatus bool) error {
	return c.ConnectUserUnicast(ctx, "", waitForStatus)
}

func (c *Client) ConnectUserUnicast_AnyDevice_NoWait(ctx context.Context) error {
	return c.ConnectUserUnicast(ctx, "", false)
}

func (c *Client) ConnectUserUnicast_AnyDevice_Wait(ctx context.Context) error {
	return c.ConnectUserUnicast(ctx, "", true)
}

func (c *Client) ConnectUserUnicast_NoWait(ctx context.Context, deviceCode string) error {
	return c.ConnectUserUnicast(ctx, deviceCode, false)
}

func (c *Client) ConnectUserUnicast(ctx context.Context, deviceCode string, waitForStatus bool) error {
	err := c.DisconnectUser(ctx, true, true)
	if err != nil {
		return fmt.Errorf("failed to ensure disconnected for %s: %w", c.Host, err)
	}

	c.log.Info("Connecting unicast user", "host", c.Host, "device", deviceCode)
	ctx, cancel := context.WithTimeout(ctx, connectUnicastTimeout)
	defer cancel()
	resp, err := c.grpcClient.ConnectUnicast(ctx, &pb.ConnectUnicastRequest{
		Mode:       pb.ConnectUnicastRequest_IBRL,
		DeviceCode: deviceCode,
	})
	if err != nil {
		return fmt.Errorf("failed to connect %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("connection failed for %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Info("Unicast user connected", "host", c.Host, "device", deviceCode)

	if waitForStatus {
		err := c.WaitForStatusUp(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for status to be up: %w", err)
		}
	}

	return nil
}

type UnicastTestConnectivityResult struct {
	PacketsSent     uint32
	PacketsReceived uint32
}

func (c *Client) TestUnicastConnectivity(ctx context.Context, targetClient *Client) (*UnicastTestConnectivityResult, error) {
	sourceIP := c.publicIP.To4().String()
	targetIP := targetClient.publicIP.To4().String()

	clientDevice, err := c.getConnectedDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connected device: %w", err)
	}

	otherClientDevice, err := targetClient.getConnectedDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connected device: %w", err)
	}

	var iface string
	if clientDevice.ExchangeCode != otherClientDevice.ExchangeCode {
		iface = unicastInterfaceName
		c.log.Info("Pinging", "source", sourceIP, "target", targetIP, "iface", iface, "sourceExchange", clientDevice.ExchangeCode, "targetExchange", otherClientDevice.ExchangeCode)
	} else {
		c.log.Info("Pinging (intra-exchange routing)", "source", sourceIP, "target", targetIP, "exchange", clientDevice.ExchangeCode)
	}

	ctx, cancel := context.WithTimeout(ctx, unicastPingRequestTimeout)
	defer cancel()
	resp, err := c.grpcClient.Ping(ctx, &pb.PingRequest{
		TargetIp:    targetIP,
		SourceIp:    sourceIP,
		PingType:    pb.PingRequest_ICMP,
		SourceIface: iface,
		Timeout:     uint32(unicastPingProbeTimeout.Seconds()), // per-probe timeout
		Count:       uint32(unicastPingProbeCount),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	if resp.PacketsSent == 0 {
		return nil, fmt.Errorf("no packets sent from %s to %s", sourceIP, targetIP)
	}
	if resp.PacketsReceived < resp.PacketsSent {
		// If we have packet loss, check if routes were uninstalled and log an error for visibility.
		installedRoutes, err := c.GetInstalledRoutes(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get installed routes: %w", err)
		}
		installedIPs := make(map[string]struct{})
		for _, route := range installedRoutes {
			installedIPs[route.DstIp] = struct{}{}
		}
		if _, ok := installedIPs[targetIP]; !ok {
			c.log.Error("Routes disappeared while pinging, packet loss detected",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
				"packetsSent", resp.PacketsSent,
				"packetsReceived", resp.PacketsReceived,
			)
		}

		// If we have more than the threshold of packet loss, return an error, otherwise log.
		if resp.PacketsReceived <= resp.PacketsSent-unicastPingProbeLossThreshold {
			return nil, fmt.Errorf("packet loss detected: sent=%d, received=%d from %s to %s", resp.PacketsSent, resp.PacketsReceived, sourceIP, targetIP)
		} else {
			c.log.Warn("Partial packet loss detected",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
				"packetsSent", resp.PacketsSent,
				"packetsReceived", resp.PacketsReceived,
				"probeCount", unicastPingProbeCount,
				"probeLossThreshold", unicastPingProbeLossThreshold,
			)
		}
	}

	c.log.Info("Successfully pinged",
		"sourceHost", c.Host,
		"targetHost", targetClient.Host,
		"iface", iface,
		"sourceDevice", clientDevice.Code,
		"targetDevice", otherClientDevice.Code,
		"packetsSent", resp.PacketsSent,
		"packetsReceived", resp.PacketsReceived,
	)

	return &UnicastTestConnectivityResult{
		PacketsSent:     resp.PacketsSent,
		PacketsReceived: resp.PacketsReceived,
	}, nil
}

func (c *Client) TracerouteRaw(ctx context.Context, targetIP string) (string, error) {
	sourceIP := c.publicIP.To4().String()
	output, err := c.grpcClient.TracerouteRaw(ctx, &pb.TracerouteRequest{
		TargetIp:    targetIP,
		SourceIp:    sourceIP,
		SourceIface: unicastInterfaceName,
		Timeout:     uint32(unicastTracerouteTimeout.Seconds()),
		Count:       unicastTracerouteCount,
	})
	if err != nil {
		return "", fmt.Errorf("failed to traceroute: %w", err)
	}
	return strings.Join(output.Output, "\n"), nil
}
