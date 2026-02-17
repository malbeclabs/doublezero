package qa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	disconnectTimeout                = 150 * time.Second
	waitForStatusUpTimeout           = 90 * time.Second
	waitForStatusDisconnectedTimeout = 90 * time.Second
	waitForUserDeletionTimeout       = 90 * time.Second

	// NOTE: This needs to be longer than 1m since BGP can sometimes throttle activity for that
	// amount of time if too much is happening consecutively for the same peers.
	waitForRoutesTimeout = 90 * time.Second

	waitInterval = 1 * time.Second

	grpcDialTimeout    = 10 * time.Second
	grpcDialMaxRetries = 5

	UserStatusUp           = "BGP Session Up"
	UserStatusUpLegacy     = "up" // TODO: remove after all QA hosts are upgraded past v0.8.2
	UserStatusDisconnected = "disconnected"
)

// IsStatusUp checks if the session status indicates the session is up.
func IsStatusUp(status string) bool {
	return status == UserStatusUp || status == UserStatusUpLegacy
}

// IsIBRLStatus returns true if the status represents an IBRL (unicast) tunnel.
func IsIBRLStatus(s *pb.Status) bool {
	return strings.HasPrefix(s.UserType, "IBRL")
}

// FindIBRLStatus returns the first IBRL status from the given list.
// When only one status exists, it is returned regardless of type (backwards-compatible
// fallback for single-tunnel mode).
func FindIBRLStatus(statuses []*pb.Status) *pb.Status {
	if len(statuses) == 1 {
		return statuses[0]
	}
	for _, s := range statuses {
		if IsIBRLStatus(s) {
			return s
		}
	}
	return nil
}

type Device struct {
	PubKey       string
	Code         string
	ExchangeCode string
	MaxUsers     int
	UsersCount   int
	Status       serviceability.DeviceStatus
	DeviceType   serviceability.DeviceDeviceType
}

type Client struct {
	log            *slog.Logger
	grpcClient     pb.QAAgentServiceClient
	grpcConn       *grpc.ClientConn
	publicIP       net.IP
	doubleZeroIP   net.IP
	serviceability *serviceability.Client
	devices        map[string]*Device

	Host         string
	AllocateAddr bool

	// ClientIP overrides the client IP passed to the connect command.
	// When set, this value is sent in the ConnectUnicast request's client_ip field,
	// causing the CLI to use it instead of auto-detecting from the routing table.
	// This is needed in E2E tests where the container has multiple network interfaces
	// and auto-detection picks the wrong one.
	//
	// Exported as a simple configuration field (unlike publicIP which uses a setter
	// because it has a non-nil invariant enforced by SetPublicIP).
	ClientIP string
}

func NewClient(ctx context.Context, log *slog.Logger, hostname string, port int, networkConfig *config.NetworkConfig, devices map[string]*Device, allocateAddr bool) (*Client, error) {
	target := net.JoinHostPort(hostname, strconv.Itoa(port))
	grpcConn, err := newClientWithRetry(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client on host %s: %v", hostname, err)
	}

	grpcClient := pb.NewQAAgentServiceClient(grpcConn)

	resp, err := grpcClient.GetPublicIP(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP on host %s: %v", hostname, err)
	}
	publicIP := net.ParseIP(resp.PublicIp)
	if publicIP == nil || publicIP.To4() == nil {
		return nil, fmt.Errorf("invalid public IP on host %s: %v", hostname, resp.PublicIp)
	}

	log.Debug("Initializing client", "host", hostname, "publicIP", publicIP.To4().String())

	serviceabilityClient := serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	return &Client{
		log:            log,
		grpcClient:     grpcClient,
		grpcConn:       grpcConn,
		publicIP:       publicIP,
		serviceability: serviceabilityClient,
		devices:        devices,

		Host:         hostname,
		AllocateAddr: allocateAddr,
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

// SetPublicIP overrides the auto-detected public IP. This is needed in E2E tests
// where the container has multiple network interfaces and the auto-detected IP
// (from the default Docker network) differs from the CYOA network IP used for
// tunnel setup and route installation.
func (c *Client) SetPublicIP(ip net.IP) {
	if ip == nil {
		panic("SetPublicIP called with nil IP")
	}
	c.publicIP = ip
}

func (c *Client) DoublezeroOrPublicIP() net.IP {
	if c.doubleZeroIP != nil {
		return c.doubleZeroIP
	}
	return c.publicIP
}

func (c *Client) DisconnectUser(ctx context.Context, waitForStatus bool, waitForDeletion bool) error {
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to get user status on host %s: %w", c.Host, err)
	}
	// Log if any tunnel is not already disconnected.
	for _, s := range resp.Status {
		if s.SessionStatus != UserStatusDisconnected {
			c.log.Debug("Disconnecting user", "host", c.Host, "tunnel", s.TunnelName, "userType", s.UserType)
			break
		}
	}

	// Always try disconnecting, even if it looks like the user is already disconnected.
	// We do this to handle the case where the client thinks it's disconnected but the user exists
	// onchain, which can happen if the previous connect attempt timed out in the CLI but eventually
	// succeeded in the activator.
	ctx, cancel := context.WithTimeout(ctx, disconnectTimeout)
	defer cancel()
	_, err = c.grpcClient.Disconnect(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to disconnect on host %s: %w", c.Host, err)
	}

	if waitForStatus {
		err = c.WaitForStatusDisconnected(ctx)
		if err != nil {
			// Log current statuses for diagnostics using a fresh context
			// since the caller's ctx may have timed out.
			diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer diagCancel()
			diagResp, diagErr := c.grpcClient.GetStatus(diagCtx, &emptypb.Empty{})
			if diagErr == nil {
				for _, s := range diagResp.Status {
					c.log.Info("DisconnectUser timeout: current status",
						"host", c.Host,
						"tunnelName", s.TunnelName,
						"userType", s.UserType,
						"sessionStatus", s.SessionStatus,
					)
				}
			}
			return fmt.Errorf("failed to wait for status to be disconnected on host %s: %w", c.Host, err)
		}
	}

	if waitForDeletion {
		publicIP := c.publicIP.To4().String()

		data, err := getProgramDataWithRetry(ctx, c.serviceability)
		if err != nil {
			// RPC errors (e.g. 429 rate limiting) during the initial check are not fatal —
			// skip the early exit and fall through to the polling loop which will keep trying.
			c.log.Debug("Failed to check onchain user state, will poll for deletion", "host", c.Host, "error", err)
		} else {
			userFound := false
			for _, user := range data.Users {
				userClientIP := net.IP(user.ClientIp[:]).String()
				if userClientIP == publicIP {
					userFound = true
					break
				}
			}
			if !userFound {
				c.log.Debug("User already deleted onchain", "ip", publicIP)
				return nil
			}
		}

		c.log.Debug("Waiting for user to be deleted onchain", "host", c.Host)
		ctx, cancel := context.WithTimeout(ctx, waitForUserDeletionTimeout)
		defer cancel()
		err = poll.Until(ctx, func() (bool, error) {
			data, err := getProgramDataWithRetry(ctx, c.serviceability)
			if err != nil {
				// Transient RPC errors (e.g. 429 rate limiting) should not abort the poll.
				// Log and keep trying until the timeout expires.
				c.log.Debug("Transient RPC error while waiting for user deletion, will retry", "host", c.Host, "error", err)
				return false, nil
			}

			for _, user := range data.Users {
				userClientIP := net.IP(user.ClientIp[:]).String()
				if userClientIP == publicIP {
					c.log.Debug("Waiting for user to be deleted onchain", "ip", publicIP, "status", user.Status)
					return false, nil
				}
			}

			return true, nil
		}, waitForUserDeletionTimeout, waitInterval)
		if err != nil {
			return fmt.Errorf("timed out waiting for user deletion for IP %s on host %s: %w", publicIP, c.Host, err)
		}
		c.log.Debug("Confirmed user deleted onchain", "ip", publicIP)
	}

	return nil
}

// GetUserStatus returns the single tunnel status for the client.
//
// Deprecated: Use GetUserStatuses + FindIBRLStatus instead, which handle
// multi-tunnel scenarios where more than one status may be present.
func (c *Client) GetUserStatus(ctx context.Context) (*pb.Status, error) {
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get status on host %s: %w", c.Host, err)
	}
	if len(resp.Status) == 0 {
		return nil, fmt.Errorf("no user status found on host %s", c.Host)
	}
	if len(resp.Status) > 1 {
		return nil, fmt.Errorf("multiple user statuses found on host %s", c.Host)
	}
	return resp.Status[0], nil
}

// GetUserStatuses returns all tunnel statuses for the client.
// Returns an error if no statuses are found.
func (c *Client) GetUserStatuses(ctx context.Context) ([]*pb.Status, error) {
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get status on host %s: %w", c.Host, err)
	}
	if len(resp.Status) == 0 {
		return nil, fmt.Errorf("no user statuses found on host %s", c.Host)
	}
	return resp.Status, nil
}

func (c *Client) GetCurrentDevice(ctx context.Context) (*Device, error) {
	return c.getIBRLDevice(ctx, false)
}

func (c *Client) GetInstalledRoutes(ctx context.Context) ([]*pb.Route, error) {
	resp, err := c.grpcClient.GetRoutes(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get routes on host %s: %w", c.Host, err)
	}
	return resp.InstalledRoutes, nil
}

func (c *Client) GetLatency(ctx context.Context) ([]*pb.Latency, error) {
	resp, err := c.grpcClient.GetLatency(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get latency on host %s: %w", c.Host, err)
	}
	return resp.Latencies, nil
}

func (c *Client) WaitForStatusUp(ctx context.Context) error {
	c.log.Debug("Waiting for status to be up", "host", c.Host)
	status, err := c.waitForStatus(ctx, UserStatusUp, waitForStatusUpTimeout, waitInterval)
	if err != nil {
		return fmt.Errorf("failed to wait for status to be up on host %s: %w", c.Host, err)
	}

	if status.DoubleZeroIp != "" {
		c.doubleZeroIP = net.ParseIP(status.DoubleZeroIp)
	}

	c.log.Debug("Confirmed status is up", "host", c.Host, "doubleZeroIP", c.doubleZeroIP)
	return nil
}

// WaitForAllStatusesUp polls until all tunnel statuses are up and at least
// minExpected statuses exist. Sets doubleZeroIP from the IBRL status preferentially.
func (c *Client) WaitForAllStatusesUp(ctx context.Context, minExpected int) error {
	c.log.Debug("Waiting for all statuses to be up", "host", c.Host, "minExpected", minExpected)
	ctx, cancel := context.WithTimeout(ctx, waitForStatusUpTimeout)
	defer cancel()

	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
		if err != nil {
			return false, err
		}
		if len(resp.Status) < minExpected {
			c.log.Debug("Waiting for statuses", "host", c.Host, "have", len(resp.Status), "want", minExpected)
			return false, nil
		}
		for _, s := range resp.Status {
			if !IsStatusUp(s.SessionStatus) {
				c.log.Debug("Status not up yet", "host", c.Host, "tunnel", s.TunnelName, "userType", s.UserType, "status", s.SessionStatus)
				return false, nil
			}
		}
		// All statuses are up. Set doubleZeroIP from IBRL status preferentially.
		ibrl := FindIBRLStatus(resp.Status)
		if ibrl != nil && ibrl.DoubleZeroIp != "" {
			c.doubleZeroIP = net.ParseIP(ibrl.DoubleZeroIp)
		}
		return true, nil
	}, waitForStatusUpTimeout, waitInterval)
	if err != nil {
		// Log current statuses for diagnostics before returning error.
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer diagCancel()
		resp, diagErr := c.grpcClient.GetStatus(diagCtx, &emptypb.Empty{})
		if diagErr == nil {
			for _, s := range resp.Status {
				c.log.Info("WaitForAllStatusesUp timeout: current status",
					"host", c.Host,
					"tunnelName", s.TunnelName,
					"userType", s.UserType,
					"sessionStatus", s.SessionStatus,
				)
			}
		}
		return fmt.Errorf("failed to wait for all statuses to be up on host %s: %w", c.Host, err)
	}

	c.log.Debug("Confirmed all statuses are up", "host", c.Host, "doubleZeroIP", c.doubleZeroIP)
	return nil
}

func (c *Client) GetOwnerPubkey(ctx context.Context) (solana.PublicKey, error) {
	data, err := getProgramDataWithRetry(ctx, c.serviceability)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to get program data on host %s: %w", c.Host, err)
	}
	publicIP := c.publicIP.To4().String()
	for _, user := range data.Users {
		userClientIP := net.IP(user.ClientIp[:]).String()
		if userClientIP == publicIP {
			return solana.PublicKeyFromBytes(user.PubKey[:]), nil
		}
	}
	return solana.PublicKey{}, fmt.Errorf("owner pubkey not found on host %s", c.Host)
}

func (c *Client) WaitForStatusDisconnected(ctx context.Context) error {
	c.log.Debug("Waiting for status to be disconnected", "host", c.Host)
	_, err := c.waitForStatus(ctx, UserStatusDisconnected, waitForStatusDisconnectedTimeout, waitInterval)
	if err != nil {
		return fmt.Errorf("failed to wait for status to be disconnected on host %s: %w", c.Host, err)
	}
	c.log.Debug("Confirmed status is disconnected", "host", c.Host)
	return nil
}

func (c *Client) WaitForRoutes(ctx context.Context, expectedIPs []net.IP) error {
	c.log.Debug("Waiting for routes to be installed", "host", c.Host, "expectedIPs", expectedIPs)
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
	}, waitForRoutesTimeout, waitInterval)
	if err != nil {
		return fmt.Errorf("failed to wait for routes to be installed on host %s: %w", c.Host, err)
	}
	c.log.Debug("Confirmed routes installed", "host", c.Host, "expectedIPs", expectedIPs)
	return nil
}

// DumpDiagnostics collects and logs diagnostic information from the client.
// It uses a fresh context since the caller's context may have been cancelled.
// If groups is provided, multicast reports are included in the output.
func (c *Client) DumpDiagnostics(groups []*MulticastGroup) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c.log.Info("--- DIAGNOSTICS ---", "host", c.Host, "publicIP", c.publicIP)

	// Tunnel status.
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		c.log.Info("diagnostics: status error", "host", c.Host, "error", err)
	} else if len(resp.Status) == 0 {
		c.log.Info("diagnostics: no status entries", "host", c.Host)
	} else {
		for _, s := range resp.Status {
			c.log.Info("diagnostics: status",
				"host", c.Host,
				"sessionStatus", s.SessionStatus,
				"tunnelName", s.TunnelName,
				"doubleZeroIP", s.DoubleZeroIp,
				"userType", s.UserType,
				"currentDevice", s.CurrentDevice,
			)
		}
	}

	// Installed routes.
	routes, err := c.GetInstalledRoutes(ctx)
	if err != nil {
		c.log.Info("diagnostics: routes error", "host", c.Host, "error", err)
	} else {
		routeIPs := Map(routes, func(r *pb.Route) string { return r.DstIp })
		c.log.Info("diagnostics: routes", "host", c.Host, "count", len(routes), "destinations", routeIPs)
	}

	// Device latency.
	latencies, err := c.GetLatency(ctx)
	if err != nil {
		c.log.Info("diagnostics: latency error", "host", c.Host, "error", err)
	} else {
		for _, l := range latencies {
			c.log.Info("diagnostics: latency",
				"host", c.Host,
				"deviceCode", l.DeviceCode,
				"deviceIP", l.DeviceIp,
				"avgLatencyNs", l.AvgLatencyNs,
				"reachable", l.Reachable,
			)
		}
	}

	// Multicast reports.
	var validGroups []*MulticastGroup
	for _, g := range groups {
		if g != nil {
			validGroups = append(validGroups, g)
		}
	}
	if len(validGroups) > 0 {
		pbGroups := make([]*pb.MulticastGroup, len(validGroups))
		for i, g := range validGroups {
			pbGroups[i] = &pb.MulticastGroup{
				Group: g.IP.String(),
				Port:  multicastConnectivityPort,
				Iface: multicastInterfaceName,
			}
		}
		reportResp, err := c.grpcClient.MulticastReport(ctx, &pb.MulticastReportRequest{Groups: pbGroups})
		if err != nil {
			c.log.Info("diagnostics: multicast report error", "host", c.Host, "error", err)
		} else {
			for _, g := range validGroups {
				report := reportResp.Reports[g.IP.String()]
				var packetCount uint64
				if report != nil {
					packetCount = report.PacketCount
				}
				c.log.Info("diagnostics: multicast report",
					"host", c.Host,
					"groupCode", g.Code,
					"groupIP", g.IP,
					"iface", multicastInterfaceName,
					"packetCount", packetCount,
				)
			}
		}
	}

	// Onchain user and device state.
	data, err := getProgramDataWithRetry(ctx, c.serviceability)
	if err != nil {
		c.log.Info("diagnostics: onchain state error", "host", c.Host, "error", err)
	} else {
		publicIP := c.publicIP.To4().String()
		userFound := false
		for _, user := range data.Users {
			userClientIP := net.IP(user.ClientIp[:]).String()
			if userClientIP != publicIP {
				continue
			}
			userFound = true

			// Resolve device code.
			deviceCode := "unknown"
			for _, d := range data.Devices {
				if d.PubKey == user.DevicePubKey {
					deviceCode = d.Code
					break
				}
			}

			c.log.Info("diagnostics: onchain user",
				"host", c.Host,
				"status", user.Status,
				"userType", user.UserType,
				"deviceCode", deviceCode,
				"dzIP", net.IP(user.DzIp[:]).String(),
				"publishers", len(user.Publishers),
				"subscribers", len(user.Subscribers),
			)

			// Log health of the assigned device.
			for _, d := range data.Devices {
				if d.PubKey == user.DevicePubKey {
					c.log.Info("diagnostics: onchain device",
						"host", c.Host,
						"deviceCode", d.Code,
						"status", d.Status,
						"health", d.DeviceHealth,
						"users", d.UsersCount,
						"maxUsers", d.MaxUsers,
					)
					break
				}
			}
			break
		}
		if !userFound {
			c.log.Info("diagnostics: no onchain user found", "host", c.Host, "clientIP", publicIP)
		}
	}
}

func (c *Client) getConnectedDevice(ctx context.Context) (*Device, error) {
	return c.getIBRLDevice(ctx, true)
}

// getIBRLDevice returns the device for the client's IBRL tunnel. When
// requireUp is true it also verifies the session status is up.
func (c *Client) getIBRLDevice(ctx context.Context, requireUp bool) (*Device, error) {
	resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get status on host %s: %w", c.Host, err)
	}
	status := FindIBRLStatus(resp.Status)
	if status == nil {
		return nil, fmt.Errorf("no IBRL status found on host %s", c.Host)
	}
	if requireUp && !IsStatusUp(status.SessionStatus) {
		return nil, fmt.Errorf("user status is not up on host %s: %s", c.Host, status.SessionStatus)
	}
	device, ok := c.devices[status.CurrentDevice]
	if !ok {
		return nil, fmt.Errorf("device %q not found on host %s", status.CurrentDevice, c.Host)
	}
	return device, nil
}

func (c *Client) waitForStatus(ctx context.Context, wantStatus string, timeout time.Duration, interval time.Duration) (*pb.Status, error) {
	var finalStatus *pb.Status
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.grpcClient.GetStatus(ctx, &emptypb.Empty{})
		if err != nil {
			return false, err
		}
		for _, s := range resp.Status {
			if wantStatus == UserStatusUp {
				if !IsStatusUp(s.SessionStatus) {
					return false, nil
				}
			} else if s.SessionStatus != wantStatus {
				return false, nil
			}
		}
		if len(resp.Status) > 0 {
			finalStatus = resp.Status[0]
		}
		return true, nil
	}, timeout, interval)
	return finalStatus, err
}

func newClientWithRetry(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn

	baseOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithUnaryInterceptor(retryUnaryInterceptor(4, 500*time.Millisecond)),
		grpc.WithDefaultServiceConfig(`{
          "methodConfig": [{
            "name": [ { "service": "" } ],
            "retryPolicy": {
              "MaxAttempts": 4,
              "InitialBackoff": ".5s",
              "MaxBackoff": "5s",
              "BackoffMultiplier": 2.0,
              "RetryableStatusCodes": ["UNAVAILABLE","ABORTED"]
            }
          }]
        }`),
	}

	dialOpts := append(opts, baseOpts...)

	operation := func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, grpcDialTimeout)
		defer cancel()

		c, err := grpc.NewClient(target, dialOpts...)
		if err != nil {
			return err
		}

		if attemptCtx.Err() != nil {
			_ = c.Close()
			return attemptCtx.Err()
		}

		conn = c
		return nil
	}

	exp := backoff.NewExponentialBackOff()
	retryPolicy := backoff.WithMaxRetries(exp, grpcDialMaxRetries)
	retryPolicy = backoff.WithContext(retryPolicy, ctx)

	if err := backoff.Retry(operation, retryPolicy); err != nil {
		return nil, fmt.Errorf("failed to dial %s after retries: %w", target, err)
	}

	return conn, nil
}

func retryUnaryInterceptor(maxAttempts int, baseBackoff time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				backoffDelay := baseBackoff * time.Duration(attempt-1)
				select {
				case <-time.After(backoffDelay):
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			lastErr = invoker(ctx, method, req, reply, cc, opts...)
			if lastErr == nil || !isRetryable(lastErr) {
				return lastErr
			}
		}
		return lastErr
	}
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.Aborted:
			return true
		}
	}

	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	if strings.Contains(err.Error(), "connection reset by peer") {
		return true
	}
	return false
}
