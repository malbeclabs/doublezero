package qa

import (
	"context"
	"fmt"
	"time"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
)

const (
	connectUnicastTimeout     = 90 * time.Second
	unicastPingRequestTimeout = 60 * time.Second
	unicastPingProbeTimeout   = 5 * time.Second

	unicastPingProbeCount         = 5
	unicastPingProbeLossThreshold = 2

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

func (c *Client) TestUnicastConnectivity(ctx context.Context, targetClient *Client) error {
	sourceIP := c.publicIP.To4().String()
	targetIP := targetClient.publicIP.To4().String()

	clientDevice, err := c.getConnectedDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connected device: %w", err)
	}

	otherClientDevice, err := targetClient.getConnectedDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connected device: %w", err)
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
		return fmt.Errorf("failed to ping: %w", err)
	}

	if resp.PacketsSent == 0 {
		return fmt.Errorf("no packets sent from %s to %s", sourceIP, targetIP)
	}
	if resp.PacketsReceived == 0 {
		return fmt.Errorf("no packets received by %s from %s (sent=%d)", targetIP, sourceIP, resp.PacketsSent)
	}
	if resp.PacketsReceived < resp.PacketsSent {
		// If we have packet loss, check if routes were uninstalled and log an error for visibility.
		installedRoutes, err := c.GetInstalledRoutes(ctx)
		if err != nil {
			return fmt.Errorf("failed to get installed routes: %w", err)
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
			return fmt.Errorf("packet loss detected: sent=%d, received=%d from %s to %s", resp.PacketsSent, resp.PacketsReceived, sourceIP, targetIP)
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

	return nil
}
