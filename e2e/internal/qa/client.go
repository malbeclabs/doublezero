package qa

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	defaultConnectUnicastTimeout                = 90 * time.Second
	defaultConnectMulticastTimeout              = 90 * time.Second
	defaultDisconnectTimeout                    = 30 * time.Second
	defaultWaitForStatusUpTimeout               = 60 * time.Second
	defaultWaitForStatusDisconnectedTimeout     = 30 * time.Second
	defaultWaitForUserDeletionTimeout           = 60 * time.Second
	defaultPingTimeout                          = 30 * time.Second
	defaultWaitForRoutesTimeout                 = 30 * time.Second
	defaultWaitForRoutesInterval                = 1 * time.Second
	defaultLeaveMulticastGroupTimeout           = 30 * time.Second
	defaultWaitForMulticastGroupCreatedTimeout  = 60 * time.Second
	defaultWaitForMulticastGroupCreatedInterval = 1 * time.Second
	defaultWaitForMulticastReportTimeout        = 60 * time.Second
	defaultWaitForMulticastReportInterval       = 1 * time.Second

	UserStatusUp           = "up"
	UserStatusDisconnected = "disconnected"

	unicastInterfaceName   = "doublezero0"
	multicastInterfaceName = "doublezero1"

	multicastConnectivityPort = 7000
)

type Device struct {
	PubKey       string
	Code         string
	ExchangeCode string
	MaxUsers     int
	UsersCount   int
}

type MulticastGroup struct {
	Code    string
	PK      solana.PublicKey
	IP      net.IP
	OwnerPK solana.PublicKey
}

type Client struct {
	log            *slog.Logger
	grpcClient     pb.QAAgentServiceClient
	grpcConn       *grpc.ClientConn
	publicIP       net.IP
	serviceability *serviceability.Client
	devices        map[string]*Device

	connectUnicastTimeout                time.Duration
	connectMulticastTimeout              time.Duration
	disconnectTimeout                    time.Duration
	waitForStatusUpTimeout               time.Duration
	waitForStatusDisconnectedTimeout     time.Duration
	waitForUserDeletionTimeout           time.Duration
	pingTimeout                          time.Duration
	waitForRoutesTimeout                 time.Duration
	waitForRoutesInterval                time.Duration
	leaveMulticastGroupTimeout           time.Duration
	waitForMulticastGroupCreatedTimeout  time.Duration
	waitForMulticastGroupCreatedInterval time.Duration
	waitForMulticastReportTimeout        time.Duration
	waitForMulticastReportInterval       time.Duration

	Host string
}

func NewClient(ctx context.Context, log *slog.Logger, hostname string, port int, networkConfig *config.NetworkConfig, devices map[string]*Device) (*Client, error) {
	target := net.JoinHostPort(hostname, strconv.Itoa(port))
	grpcConn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %v", err)
	}

	grpcClient := pb.NewQAAgentServiceClient(grpcConn)

	resp, err := grpcClient.GetPublicIP(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP: %v", err)
	}
	log.Info("Got public IP", "host", hostname, "publicIP", resp.PublicIp)
	publicIP := net.ParseIP(resp.PublicIp)
	if publicIP == nil || publicIP.To4() == nil {
		return nil, fmt.Errorf("invalid public IP: %v", resp.PublicIp)
	}

	serviceabilityClient := serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	return &Client{
		log:            log,
		grpcClient:     grpcClient,
		grpcConn:       grpcConn,
		publicIP:       publicIP,
		serviceability: serviceabilityClient,
		devices:        devices,

		connectUnicastTimeout:                defaultConnectUnicastTimeout,
		connectMulticastTimeout:              defaultConnectMulticastTimeout,
		disconnectTimeout:                    defaultDisconnectTimeout,
		waitForStatusUpTimeout:               defaultWaitForStatusUpTimeout,
		waitForStatusDisconnectedTimeout:     defaultWaitForStatusDisconnectedTimeout,
		waitForUserDeletionTimeout:           defaultWaitForUserDeletionTimeout,
		pingTimeout:                          defaultPingTimeout,
		waitForRoutesTimeout:                 defaultWaitForRoutesTimeout,
		waitForRoutesInterval:                defaultWaitForRoutesInterval,
		leaveMulticastGroupTimeout:           defaultLeaveMulticastGroupTimeout,
		waitForMulticastGroupCreatedTimeout:  defaultWaitForMulticastGroupCreatedTimeout,
		waitForMulticastGroupCreatedInterval: defaultWaitForMulticastGroupCreatedInterval,
		waitForMulticastReportTimeout:        defaultWaitForMulticastReportTimeout,
		waitForMulticastReportInterval:       defaultWaitForMulticastReportInterval,

		Host: hostname,
	}, nil
}

func (c *Client) Close() error {
	return c.grpcConn.Close()
}

func (c *Client) SetLogger(log *slog.Logger) {
	c.log = log
}

func (c *Client) PublicIP() net.IP {
	return c.publicIP
}

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
	ctx, cancel := context.WithTimeout(ctx, c.connectUnicastTimeout)
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

func (c *Client) ConnectUserMulticast_Publisher_Wait(ctx context.Context, multicastGroupCode string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCode, pb.ConnectMulticastRequest_PUBLISHER, true)
}

func (c *Client) ConnectUserMulticast_Publisher_NoWait(ctx context.Context, multicastGroupCode string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCode, pb.ConnectMulticastRequest_PUBLISHER, false)
}

func (c *Client) ConnectUserMulticast_Subscriber_Wait(ctx context.Context, multicastGroupCode string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCode, pb.ConnectMulticastRequest_SUBSCRIBER, true)
}

func (c *Client) ConnectUserMulticast_Subscriber_NoWait(ctx context.Context, multicastGroupCode string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCode, pb.ConnectMulticastRequest_SUBSCRIBER, false)
}

func (c *Client) ConnectUserMulticast(ctx context.Context, multicastGroupCode string, mode pb.ConnectMulticastRequest_MulticastMode, waitForStatus bool) error {
	err := c.DisconnectUser(ctx, true, true)
	if err != nil {
		return fmt.Errorf("failed to ensure disconnected for %s: %w", c.Host, err)
	}

	c.log.Info("Connecting multicast publisher", "host", c.Host, "multicastGroupCode", multicastGroupCode)
	ctx, cancel := context.WithTimeout(ctx, c.connectMulticastTimeout)
	defer cancel()
	resp, err := c.grpcClient.ConnectMulticast(ctx, &pb.ConnectMulticastRequest{
		Mode: mode,
		Code: multicastGroupCode,
	})
	if err != nil {
		return fmt.Errorf("failed to connect %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("connection failed for %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Info("Multicast publisher connected", "host", c.Host, "multicastGroupCode", multicastGroupCode)

	return nil
}

func (c *Client) DisconnectUser(ctx context.Context, waitForStatus bool, waitForDeletion bool) error {
	status, err := c.GetUserStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user status: %w", err)
	}
	if status.SessionStatus != UserStatusUp {
		c.log.Debug("User already disconnected", "host", c.Host)
		return nil
	}

	c.log.Info("Disconnecting user", "host", c.Host)
	ctx, cancel := context.WithTimeout(ctx, c.disconnectTimeout)
	defer cancel()
	_, err = c.grpcClient.Disconnect(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to disconnect from host %s: %w", c.Host, err)
	}

	if waitForStatus {
		err = c.WaitForStatusDisconnected(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for status to be disconnected: %w", err)
		}
	}

	if waitForDeletion {
		publicIP := c.publicIP.To4().String()

		data, err := c.serviceability.GetProgramData(ctx)
		if err != nil {
			return fmt.Errorf("failed to get program data: %w", err)
		}
		for _, user := range data.Users {
			userClientIP := net.IP(user.ClientIp[:]).String()
			if userClientIP == publicIP {
				c.log.Debug("User already deleted onchain", "ip", publicIP)
				return nil
			}
		}

		c.log.Debug("Waiting for user to be deleted onchain", "host", c.Host)
		ctx, cancel := context.WithTimeout(ctx, c.waitForUserDeletionTimeout)
		defer cancel()
		err = poll.Until(ctx, func() (bool, error) {
			data, err := c.serviceability.GetProgramData(ctx)
			if err != nil {
				return false, err
			}

			for _, user := range data.Users {
				userClientIP := net.IP(user.ClientIp[:]).String()
				if userClientIP == publicIP {
					c.log.Debug("Waiting for user to be deleted onchain", "ip", publicIP, "status", user.Status)
					return false, nil
				}
			}

			return true, nil
		}, c.waitForUserDeletionTimeout, 2*time.Second)
		if err != nil {
			return fmt.Errorf("timed out waiting for user deletion for IP %s: %w", publicIP, err)
		}
		c.log.Debug("Confirmed user deleted onchain", "ip", publicIP)
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
	}

	c.log.Info("Pinging", "source", sourceIP, "target", targetIP, "iface", iface)
	ctx, cancel := context.WithTimeout(ctx, c.pingTimeout)
	defer cancel()
	resp, err := c.grpcClient.Ping(ctx, &pb.PingRequest{
		TargetIp:    targetIP,
		SourceIp:    sourceIP,
		PingType:    pb.PingRequest_ICMP,
		SourceIface: iface,
		Timeout:     uint32(c.pingTimeout.Seconds()),
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
		return fmt.Errorf("packet loss detected: sent=%d, received=%d from %s to %s", resp.PacketsSent, resp.PacketsReceived, sourceIP, targetIP)
	}

	c.log.Info("Successfully pinged", "source", sourceIP, "target", targetIP, "iface", iface)

	return nil
}

func (c *Client) GetUserStatus(ctx context.Context) (*pb.Status, error) {
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	if len(resp.Status) == 0 {
		return nil, fmt.Errorf("no user status found")
	}
	if len(resp.Status) > 1 {
		return nil, fmt.Errorf("multiple user statuses found")
	}
	return resp.Status[0], nil
}

func (c *Client) GetInstalledRoutes(ctx context.Context) ([]*pb.Route, error) {
	resp, err := c.grpcClient.GetRoutes(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get routes: %w", err)
	}
	return resp.InstalledRoutes, nil
}

func (c *Client) WaitForStatusUp(ctx context.Context) error {
	c.log.Debug("Waiting for status to be up", "host", c.Host)
	err := c.waitForStatus(ctx, UserStatusUp, c.waitForStatusUpTimeout, 1*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for status to be up: %w", err)
	}
	c.log.Debug("Confirmed status is up", "host", c.Host)
	return nil
}

func (c *Client) GetOwnerPubkey(ctx context.Context) (solana.PublicKey, error) {
	data, err := c.serviceability.GetProgramData(ctx)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to get program data: %w", err)
	}
	publicIP := c.publicIP.To4().String()
	for _, user := range data.Users {
		userClientIP := net.IP(user.ClientIp[:]).String()
		if userClientIP == publicIP {
			return solana.PublicKeyFromBytes(user.PubKey[:]), nil
		}
	}
	return solana.PublicKey{}, fmt.Errorf("owner pubkey not found")
}

func (c *Client) CreateMulticastGroup(ctx context.Context, code string, maxBandwidth string) (*MulticastGroup, error) {
	c.log.Info("Creating multicast group", "host", c.Host, "code", code, "maxBandwidth", maxBandwidth)
	resp, err := c.grpcClient.CreateMulticastGroup(ctx, &pb.CreateMulticastGroupRequest{
		Code:         code,
		MaxBandwidth: maxBandwidth,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create multicast group: %w", err)
	}
	if !resp.GetSuccess() {
		return nil, fmt.Errorf("failed to create multicast group: %s", resp.GetOutput())
	}
	c.log.Debug("Multicast group created", "host", c.Host, "code", code)

	// Wait for multicast group to be created onchain and activated.
	c.log.Debug("Waiting for multicast group to be created and activated onchain", "host", c.Host, "code", code)
	var group *MulticastGroup
	err = poll.Until(ctx, func() (bool, error) {
		data, err := c.serviceability.GetProgramData(ctx)
		if err != nil {
			return false, err
		}
		for _, multicastGroup := range data.MulticastGroups {
			if multicastGroup.Code == code {
				if multicastGroup.Status == serviceability.MulticastGroupStatusActivated {
					group = &MulticastGroup{
						Code:    code,
						PK:      solana.PublicKeyFromBytes(multicastGroup.PubKey[:]),
						OwnerPK: solana.PublicKeyFromBytes(multicastGroup.Owner[:]),
						IP:      net.IP(multicastGroup.MulticastIp[:]),
					}
					return true, nil
				}
				return false, nil
			}
		}
		return false, nil
	}, c.waitForMulticastGroupCreatedTimeout, c.waitForMulticastGroupCreatedInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for multicast group to be created and activated onchain: %w", err)
	}
	c.log.Debug("Confirmed multicast group created and activated onchain", "host", c.Host, "code", code)
	return group, nil
}

func (c *Client) DeleteMulticastGroup(ctx context.Context, pubkey solana.PublicKey) error {
	c.log.Info("Deleting multicast group", "host", c.Host, "pubkey", pubkey)
	resp, err := c.grpcClient.DeleteMulticastGroup(ctx, &pb.DeleteMulticastGroupRequest{
		Pubkey: base58.Encode(pubkey[:]),
	})
	if err != nil {
		return fmt.Errorf("failed to delete multicast group: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("failed to delete multicast group: %s", resp.GetOutput())
	}
	c.log.Debug("Multicast group deleted", "host", c.Host, "pubkey", pubkey)
	return nil
}

func (c *Client) MulticastLeave(ctx context.Context, code string) error {
	c.log.Info("Leaving multicast group", "host", c.Host, "code", code)
	ctx, cancel := context.WithTimeout(ctx, c.leaveMulticastGroupTimeout)
	defer cancel()
	_, err := c.grpcClient.MulticastLeave(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to leave multicast group: %w", err)
	}
	c.log.Debug("Left multicast group", "host", c.Host, "code", code)
	return nil
}

func (c *Client) MulticastSend(ctx context.Context, group *MulticastGroup, duration time.Duration) error {
	c.log.Info("Sending multicast data", "host", c.Host, "code", group.Code, "groupIP", group.IP, "duration", duration)
	_, err := c.grpcClient.MulticastSend(ctx, &pb.MulticastSendRequest{
		Group:    group.IP.String(),
		Port:     multicastConnectivityPort,
		Duration: uint32(duration.Seconds()),
	})
	if err != nil {
		return fmt.Errorf("failed to send multicast data: %w", err)
	}
	c.log.Debug("Sent multicast data", "host", c.Host, "code", group.Code)
	return nil
}

func (c *Client) MulticastJoin(ctx context.Context, group *MulticastGroup) error {
	c.log.Info("Joining multicast group", "host", c.Host, "code", group.Code, "groupIP", group.IP)
	_, err := c.grpcClient.MulticastJoin(ctx, &pb.MulticastJoinRequest{
		Groups: []*pb.MulticastGroup{
			{
				Group: group.IP.String(),
				Port:  multicastConnectivityPort,
				Iface: multicastInterfaceName,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to join multicast groups: %w", err)
	}
	c.log.Debug("Joined multicast group", "host", c.Host, "code", group.Code, "groupIP", group.IP)
	return nil
}

func (c *Client) WaitForMulticastReport(ctx context.Context, group *MulticastGroup) (*pb.MulticastReport, error) {
	c.log.Info("Waiting for multicast report", "host", c.Host, "code", group.Code, "groupIP", group.IP)
	var report *pb.MulticastReport
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.grpcClient.MulticastReport(ctx, &pb.MulticastReportRequest{
			Groups: []*pb.MulticastGroup{
				{
					Group: group.IP.String(),
					Port:  multicastConnectivityPort,
					Iface: multicastInterfaceName,
				},
			},
		})
		if err != nil {
			return false, fmt.Errorf("failed to get multicast report: %w", err)
		}
		if len(resp.Reports) == 0 {
			return false, nil
		}
		report = resp.Reports[group.IP.String()]
		if report == nil {
			return false, nil
		}
		c.log.Debug("Waiting for multicast report", "host", c.Host, "code", group.Code, "groupIP", group.IP, "packetCount", report.PacketCount)
		return report.PacketCount > 0, nil
	}, c.waitForMulticastReportTimeout, c.waitForMulticastReportInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for multicast report: %w", err)
	}
	c.log.Debug("Confirmed multicast report", "host", c.Host, "code", group.Code, "groupIP", group.IP)
	return report, nil
}

func (c *Client) AddPublisherToMulticastGroupAllowlist(ctx context.Context, code string, pubkey solana.PublicKey, clientIP string) error {
	return c.AddToMulticastGroupAllowlist(ctx, code, pb.MulticastAllowListAddRequest_PUBLISHER, pubkey, clientIP)
}

func (c *Client) AddSubscriberToMulticastGroupAllowlist(ctx context.Context, code string, pubkey solana.PublicKey, clientIP string) error {
	return c.AddToMulticastGroupAllowlist(ctx, code, pb.MulticastAllowListAddRequest_SUBSCRIBER, pubkey, clientIP)
}

func (c *Client) AddToMulticastGroupAllowlist(ctx context.Context, code string, mode pb.MulticastAllowListAddRequest_MulticastMode, pubkey solana.PublicKey, clientIP string) error {
	c.log.Info("Adding to multicast group allowlist", "host", c.Host, "code", code, "pubkey", pubkey, "clientIP", clientIP)
	resp, err := c.grpcClient.MulticastAllowListAdd(ctx, &pb.MulticastAllowListAddRequest{
		Mode:     mode,
		Code:     code,
		Pubkey:   base58.Encode(pubkey[:]),
		ClientIp: clientIP,
	})
	if err != nil {
		return fmt.Errorf("failed to add to multicast group allowlist: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("failed to add to multicast group allowlist: %s", resp.GetOutput())
	}
	c.log.Debug("Added to multicast group allowlist", "host", c.Host, "code", code, "pubkey", pubkey, "clientIP", clientIP)
	return nil
}

func (c *Client) WaitForStatusDisconnected(ctx context.Context) error {
	c.log.Debug("Waiting for status to be disconnected", "host", c.Host)
	err := c.waitForStatus(ctx, UserStatusDisconnected, c.waitForStatusDisconnectedTimeout, 1*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for status to be disconnected: %w", err)
	}
	c.log.Debug("Confirmed status is disconnected", "host", c.Host)
	return nil
}

func (c *Client) WaitForRoutes(ctx context.Context, expectedIPs []net.IP) error {
	c.log.Info("Waiting for routes to be installed", "host", c.Host, "expectedIPs", expectedIPs)
	err := poll.Until(ctx, func() (bool, error) {
		installedRoutes, err := c.GetInstalledRoutes(ctx)
		if err != nil {
			return false, err
		}
		installedIPs := make(map[string]struct{})
		for _, route := range installedRoutes {
			installedIPs[route.DstIp] = struct{}{}
		}
		c.log.Debug("Waiting for routes to be installed", "host", c.Host, "installedIPs", slices.Sorted(maps.Keys(installedIPs)), "expectedIPs", expectedIPs)
		for _, expectedIP := range expectedIPs {
			if _, ok := installedIPs[expectedIP.To4().String()]; !ok {
				return false, nil
			}
		}
		return true, nil
	}, c.waitForRoutesTimeout, c.waitForRoutesInterval)
	if err != nil {
		return fmt.Errorf("failed to wait for routes to be installed: %w", err)
	}
	c.log.Debug("Confirmed routes installed", "host", c.Host, "expectedIPs", expectedIPs)
	return nil
}

func (c *Client) getConnectedDevice(ctx context.Context) (*Device, error) {
	status, err := c.GetUserStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user status: %w", err)
	}
	if status.SessionStatus != UserStatusUp {
		return nil, fmt.Errorf("user status is not up")
	}
	return c.devices[status.CurrentDevice], nil
}

func (c *Client) waitForStatus(ctx context.Context, wantStatus string, timeout time.Duration, interval time.Duration) error {
	return poll.Until(ctx, func() (bool, error) {
		resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
		if err != nil {
			return false, err
		}
		for _, s := range resp.Status {
			if s.SessionStatus != wantStatus {
				return false, nil
			}
		}
		return true, nil
	}, timeout, interval)
}
