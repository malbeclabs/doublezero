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

	"github.com/cenkalti/backoff/v4"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	disconnectTimeout                = 90 * time.Second
	waitForStatusUpTimeout           = 90 * time.Second
	waitForStatusDisconnectedTimeout = 90 * time.Second
	waitForUserDeletionTimeout       = 90 * time.Second

	// NOTE: This needs to be longer than 1m since BGP can sometimes throttle activity for that
	// amount of time if too much is happening consecutively for the same peers.
	waitForRoutesTimeout = 90 * time.Second

	waitInterval = 1 * time.Second

	grpcDialTimeout    = 10 * time.Second
	grpcDialMaxRetries = 5

	UserStatusUp           = "up"
	UserStatusDisconnected = "disconnected"
)

type Device struct {
	PubKey       string
	Code         string
	ExchangeCode string
	MaxUsers     int
	UsersCount   int
}

type Client struct {
	log            *slog.Logger
	grpcClient     pb.QAAgentServiceClient
	grpcConn       *grpc.ClientConn
	publicIP       net.IP
	serviceability *serviceability.Client
	devices        map[string]*Device

	Host string
}

func NewClient(ctx context.Context, log *slog.Logger, hostname string, port int, networkConfig *config.NetworkConfig, devices map[string]*Device) (*Client, error) {
	target := net.JoinHostPort(hostname, strconv.Itoa(port))
	grpcConn, err := newClientWithRetry(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %v", err)
	}

	grpcClient := pb.NewQAAgentServiceClient(grpcConn)

	resp, err := grpcClient.GetPublicIP(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP: %v", err)
	}
	publicIP := net.ParseIP(resp.PublicIp)
	if publicIP == nil || publicIP.To4() == nil {
		return nil, fmt.Errorf("invalid public IP: %v", resp.PublicIp)
	}

	log.Info("Initializing client", "host", hostname, "publicIP", publicIP.To4().String())

	serviceabilityClient := serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	return &Client{
		log:            log,
		grpcClient:     grpcClient,
		grpcConn:       grpcConn,
		publicIP:       publicIP,
		serviceability: serviceabilityClient,
		devices:        devices,

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

func (c *Client) DisconnectUser(ctx context.Context, waitForStatus bool, waitForDeletion bool) error {
	status, err := c.GetUserStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user status: %w", err)
	}
	if status.SessionStatus != UserStatusUp {
		c.log.Debug("User already disconnected", "host", c.Host)
	} else {
		c.log.Info("Disconnecting user", "host", c.Host)
		ctx, cancel := context.WithTimeout(ctx, disconnectTimeout)
		defer cancel()
		_, err = c.grpcClient.Disconnect(ctx, &emptypb.Empty{})
		if err != nil {
			return fmt.Errorf("failed to disconnect from host %s: %w", c.Host, err)
		}
	}

	if waitForStatus {
		err = c.WaitForStatusDisconnected(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for status to be disconnected: %w", err)
		}
	}

	if waitForDeletion {
		publicIP := c.publicIP.To4().String()

		data, err := getProgramDataWithRetry(ctx, c.serviceability)
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
		ctx, cancel := context.WithTimeout(ctx, waitForUserDeletionTimeout)
		defer cancel()
		err = poll.Until(ctx, func() (bool, error) {
			data, err := getProgramDataWithRetry(ctx, c.serviceability)
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
		}, waitForUserDeletionTimeout, waitInterval)
		if err != nil {
			return fmt.Errorf("timed out waiting for user deletion for IP %s: %w", publicIP, err)
		}
		c.log.Debug("Confirmed user deleted onchain", "ip", publicIP)
	}

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

func (c *Client) GetCurrentDevice(ctx context.Context) (*Device, error) {
	status, err := c.GetUserStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user status: %w", err)
	}
	return c.devices[status.CurrentDevice], nil
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
	err := c.waitForStatus(ctx, UserStatusUp, waitForStatusUpTimeout, waitInterval)
	if err != nil {
		return fmt.Errorf("failed to wait for status to be up: %w", err)
	}
	c.log.Debug("Confirmed status is up", "host", c.Host)
	return nil
}

func (c *Client) GetOwnerPubkey(ctx context.Context) (solana.PublicKey, error) {
	data, err := getProgramDataWithRetry(ctx, c.serviceability)
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

func (c *Client) WaitForStatusDisconnected(ctx context.Context) error {
	c.log.Debug("Waiting for status to be disconnected", "host", c.Host)
	err := c.waitForStatus(ctx, UserStatusDisconnected, waitForStatusDisconnectedTimeout, waitInterval)
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
	}, waitForRoutesTimeout, waitInterval)
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

func newClientWithRetry(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn

	operation := func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, grpcDialTimeout)
		defer cancel()

		c, err := grpc.NewClient(target, append(opts, grpc.WithBlock())...)
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
