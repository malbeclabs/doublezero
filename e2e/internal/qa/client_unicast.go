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
)

const (
	connectUnicastTimeout     = 150 * time.Second
	unicastPingRequestTimeout = 60 * time.Second
	unicastPingProbeTimeout   = 5 * time.Second
	unicastPingMaxRetries     = 3
	unicastPingRetryDelay     = 1 * time.Second
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
	c.doubleZeroIP = nil // Clear stale IP before connecting

	err := c.DisconnectUser(ctx, true, true)
	if err != nil {
		return fmt.Errorf("failed to ensure disconnected on host %s: %w", c.Host, err)
	}

	c.log.Debug("Connecting unicast user", "host", c.Host, "device", deviceCode, "allocateAddr", c.AllocateAddr)
	ctx, cancel := context.WithTimeout(ctx, connectUnicastTimeout)
	defer cancel()
	mode := pb.ConnectUnicastRequest_IBRL
	if c.AllocateAddr {
		mode = pb.ConnectUnicastRequest_ALLOCATE_ADDR
	}
	resp, err := c.grpcClient.ConnectUnicast(ctx, &pb.ConnectUnicastRequest{
		Mode:       mode,
		DeviceCode: deviceCode,
	})
	if err != nil {
		return fmt.Errorf("failed to connect on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("connection failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Unicast user connected", "host", c.Host, "device", deviceCode)

	if waitForStatus {
		err := c.WaitForStatusUp(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for status to be up on host %s: %w", c.Host, err)
		}
	}

	return nil
}

type UnicastTestConnectivityResult struct {
	PacketsSent     uint32
	PacketsReceived uint32
}

func (c *Client) TestUnicastConnectivity(t *testing.T, ctx context.Context, targetClient *Client, srcDevice, dstDevice *Device) (*UnicastTestConnectivityResult, error) {
	sourceIP := c.DoublezeroOrPublicIP().To4().String()
	targetIP := targetClient.DoublezeroOrPublicIP().To4().String()

	// If devices not provided, try to get them from status
	clientDevice := srcDevice
	if clientDevice == nil {
		var err error
		clientDevice, err = c.getConnectedDevice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get connected device: %w", err)
		}
	}

	otherClientDevice := dstDevice
	if otherClientDevice == nil {
		var err error
		otherClientDevice, err = targetClient.getConnectedDevice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get connected device: %w", err)
		}
	}

	var iface string
	if clientDevice.ExchangeCode != otherClientDevice.ExchangeCode {
		iface = unicastInterfaceName
		c.log.Debug("Pinging", "source", sourceIP, "target", targetIP, "iface", iface, "sourceExchange", clientDevice.ExchangeCode, "targetExchange", otherClientDevice.ExchangeCode)
	} else {
		c.log.Debug("Pinging (intra-exchange routing)", "source", sourceIP, "target", targetIP, "exchange", clientDevice.ExchangeCode)
	}

	var lastResp *pb.PingResult
	var lastErr error
	for i := range unicastPingMaxRetries {
		resp, err := c.pingOnce(ctx, targetIP, sourceIP, iface)
		if err != nil {
			return nil, fmt.Errorf("failed to ping: %w", err)
		}
		lastResp = resp
		lastErr = err

		if resp.PacketsSent == 0 {
			c.log.Warn("No packets sent",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
				"attempts", i+1,
			)
		}

		// If there is any packet loss, run a traceroute and dump the output for visibility.
		if resp.PacketsSent > 0 && resp.PacketsReceived < resp.PacketsSent {
			res, err := c.TracerouteRaw(ctx, targetIP)
			if err != nil {
				return nil, fmt.Errorf("failed to traceroute: %w", err)
			}
			c.log.Debug("Packet loss detected, dumping traceroute output for visibility",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
				"attempts", i+1,
			)
			t.Logf("Traceroute for %s -> %s: %s", c.Host, targetClient.Host, res)
		}

		// Return success if we have any packets received.
		if resp.PacketsSent > 0 && resp.PacketsReceived > 0 {
			c.log.Debug("Successfully pinged",
				"sourceHost", c.Host,
				"targetHost", targetClient.Host,
				"iface", iface,
				"sourceDevice", clientDevice.Code,
				"targetDevice", otherClientDevice.Code,
				"packetsSent", resp.PacketsSent,
				"packetsReceived", resp.PacketsReceived,
				"attempts", i+1,
			)

			return &UnicastTestConnectivityResult{
				PacketsSent:     resp.PacketsSent,
				PacketsReceived: resp.PacketsReceived,
			}, nil
		}

		// Sleep for a second before retrying.
		time.Sleep(unicastPingRetryDelay)
	}

	// If we fail to ping after all retries, check if routes were uninstalled and log an error for visibility.
	installedRoutes, err := c.GetInstalledRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get installed routes: %w", err)
	}
	installedIPs := make(map[string]struct{})
	for _, route := range installedRoutes {
		installedIPs[route.DstIp] = struct{}{}
	}
	if _, ok := installedIPs[targetIP]; !ok {
		attrs := []any{
			"sourceHost", c.Host,
			"targetHost", targetClient.Host,
			"iface", iface,
			"sourceDevice", clientDevice.Code,
			"targetDevice", otherClientDevice.Code,
		}
		if lastResp != nil {
			attrs = append(attrs, "packetsSent", lastResp.PacketsSent, "packetsReceived", lastResp.PacketsReceived)
		}
		c.log.Error("Routes disappeared while pinging, failed to ping after all retries", attrs...)
	}

	// Return the last result even on failure so the caller can see what happened
	var result *UnicastTestConnectivityResult
	if lastResp != nil {
		result = &UnicastTestConnectivityResult{
			PacketsSent:     lastResp.PacketsSent,
			PacketsReceived: lastResp.PacketsReceived,
		}
	}
	return result, fmt.Errorf("failed to ping after %d retries: %w", unicastPingMaxRetries, lastErr)
}

func (c *Client) pingOnce(ctx context.Context, targetIP string, sourceIP string, iface string) (*pb.PingResult, error) {
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
	return resp, nil
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
		return "", fmt.Errorf("failed to traceroute on host %s: %w", c.Host, err)
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
	re := regexp.MustCompile(`^\s*(\d+)\.\|\-\-\s+(\S+)\s+(\d+(?:\.\d+)?)(?:%)?\s+(\d+)\b`)

	var hops []Hop
	sc := bufio.NewScanner(strings.NewReader(input))
	for sc.Scan() {
		line := sc.Text()
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("parse hop num %q: %w", m[1], err)
		}
		loss, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			return nil, fmt.Errorf("parse loss %q: %w", m[3], err)
		}
		hops = append(hops, Hop{Num: num, Loss: loss, Raw: line})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return hops, nil
}
