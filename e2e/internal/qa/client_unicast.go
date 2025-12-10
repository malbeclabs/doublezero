package qa

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/stretchr/testify/require"
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

func (c *Client) TestUnicastConnectivity(t *testing.T, ctx context.Context, targetClient *Client) (*UnicastTestConnectivityResult, error) {
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

		// If there are any losses, run a traceroute and dump the output for visibility.
		res, err := c.TracerouteRaw(ctx, targetIP)
		require.NoError(t, err)
		t.Logf("Traceroute for %s -> %s: %s", c.Host, targetClient.Host, res)
		isLossOutsideNetwork, err := isLossOutsideNetwork(res)
		if err != nil {
			return nil, fmt.Errorf("failed to check if loss is outside of network: %w", err)
		}
		if isLossOutsideNetwork {
			c.log.Warn("Packet loss detected in traceroute outside of network; ignoring for connectivity test",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
			)
		} else if resp.PacketsReceived <= resp.PacketsSent-unicastPingProbeLossThreshold {
			// If we have more than the threshold of packet loss, return an error.
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

func isLossOutsideNetwork(mtr string) (bool, error) {
	hops, err := parseMTR(mtr)
	if err != nil {
		return false, err
	}
	if len(hops) == 0 {
		return false, nil
	}

	firstLoss := hops[0].Loss > 0
	lastLoss := hops[len(hops)-1].Loss > 0

	middleLoss := false
	for i := 1; i < len(hops)-1; i++ {
		if hops[i].Loss > 0 {
			middleLoss = true
			break
		}
	}

	// outside-of-network = ONLY first hop OR ONLY last hop has loss
	if firstLoss && !lastLoss && !middleLoss {
		return true, nil
	}
	if lastLoss && !firstLoss && !middleLoss {
		return true, nil
	}

	return false, nil
}

type Hop struct {
	Num  int
	Loss float64
	Raw  string
}

func parseMTR(input string) ([]Hop, error) {
	re := regexp.MustCompile(`^\s*(\d+)\.\|\-\-\s+(\S+)\s+(\d+\.\d+|\d+)%`)
	var hops []Hop

	sc := bufio.NewScanner(strings.NewReader(input))
	for sc.Scan() {
		line := sc.Text()
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		loss, _ := strconv.ParseFloat(m[3], 64)
		hops = append(hops, Hop{Num: num, Loss: loss, Raw: line})
	}
	return hops, sc.Err()
}
