package qa

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	connectMulticastTimeout             = 150 * time.Second
	leaveMulticastGroupTimeout          = 90 * time.Second
	waitForMulticastGroupCreatedTimeout = 90 * time.Second
	waitForMulticastReportTimeout       = 90 * time.Second

	multicastInterfaceName = "doublezero1"

	multicastConnectivityPort = 7000
)

type MulticastGroup struct {
	Code    string
	PK      solana.PublicKey
	IP      net.IP
	OwnerPK solana.PublicKey
	Status  serviceability.MulticastGroupStatus
}

func (c *Client) ConnectUserMulticast_Publisher_Wait(ctx context.Context, multicastGroupCodes ...string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCodes, pb.ConnectMulticastRequest_PUBLISHER, true)
}

func (c *Client) ConnectUserMulticast_Publisher_NoWait(ctx context.Context, multicastGroupCodes ...string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCodes, pb.ConnectMulticastRequest_PUBLISHER, false)
}

func (c *Client) ConnectUserMulticast_Subscriber_Wait(ctx context.Context, multicastGroupCodes ...string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCodes, pb.ConnectMulticastRequest_SUBSCRIBER, true)
}

func (c *Client) ConnectUserMulticast_Subscriber_NoWait(ctx context.Context, multicastGroupCodes ...string) error {
	return c.ConnectUserMulticast(ctx, multicastGroupCodes, pb.ConnectMulticastRequest_SUBSCRIBER, false)
}

func (c *Client) ConnectUserMulticast(ctx context.Context, multicastGroupCodes []string, mode pb.ConnectMulticastRequest_MulticastMode, waitForStatus bool) error {
	// Don't wait for daemon status — the daemon may be stuck in "BGP Session Failed" if a
	// previous disconnect failed to clean up the tunnel state. Waiting for onchain deletion
	// is sufficient; the next connect will overwrite the daemon's stale tunnel state.
	err := c.DisconnectUser(ctx, false, true)
	if err != nil {
		return fmt.Errorf("failed to ensure disconnected on host %s: %w", c.Host, err)
	}

	c.log.Debug("Connecting multicast", "host", c.Host, "multicastGroupCodes", multicastGroupCodes, "mode", mode)
	ctx, cancel := context.WithTimeout(ctx, connectMulticastTimeout)
	defer cancel()
	resp, err := c.grpcClient.ConnectMulticast(ctx, &pb.ConnectMulticastRequest{
		Mode:  mode,
		Codes: multicastGroupCodes,
	})
	if err != nil {
		return fmt.Errorf("failed to connect on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("connection failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Multicast connected", "host", c.Host, "multicastGroupCodes", multicastGroupCodes)

	return nil
}

func (c *Client) GetMulticastGroup(ctx context.Context, code string) (*MulticastGroup, error) {
	data, err := getProgramDataWithRetry(ctx, c.serviceability)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data on host %s: %w", c.Host, err)
	}
	for _, multicastGroup := range data.MulticastGroups {
		if multicastGroup.Code == code {
			return &MulticastGroup{
				Code:    code,
				PK:      solana.PublicKeyFromBytes(multicastGroup.PubKey[:]),
				OwnerPK: solana.PublicKeyFromBytes(multicastGroup.Owner[:]),
				IP:      net.IP(multicastGroup.MulticastIp[:]),
				Status:  multicastGroup.Status,
			}, nil
		}
	}
	return nil, nil
}

func (c *Client) CreateMulticastGroup(ctx context.Context, code string, maxBandwidth string) (*MulticastGroup, error) {
	c.log.Debug("Creating multicast group", "host", c.Host, "code", code, "maxBandwidth", maxBandwidth)
	resp, err := c.grpcClient.CreateMulticastGroup(ctx, &pb.CreateMulticastGroupRequest{
		Code:         code,
		MaxBandwidth: maxBandwidth,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create multicast group on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return nil, fmt.Errorf("failed to create multicast group on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Multicast group created", "host", c.Host, "code", code)

	// Wait for multicast group to be created onchain and activated.
	c.log.Debug("Waiting for multicast group to be created and activated onchain", "host", c.Host, "code", code)
	var group *MulticastGroup
	err = poll.Until(ctx, func() (bool, error) {
		group, err = c.GetMulticastGroup(ctx, code)
		if err != nil {
			return false, err
		}
		if group == nil {
			c.log.Debug("Multicast group not found, waiting for it to be created", "host", c.Host, "code", code)
			return false, nil
		}
		if group.Status != serviceability.MulticastGroupStatusActivated {
			c.log.Debug("Multicast group not activated, waiting for it to be activated", "host", c.Host, "code", code, "pubkey", group.PK, "groupIP", group.IP, "status", group.Status)
			return false, nil
		}
		return true, nil
	}, waitForMulticastGroupCreatedTimeout, waitInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for multicast group to be created and activated onchain on host %s: %w", c.Host, err)
	}
	c.log.Debug("Confirmed multicast group created and activated onchain", "host", c.Host, "code", code, "pubkey", group.PK, "ownerPK", group.OwnerPK, "groupIP", group.IP, "status", group.Status)
	return group, nil
}

func (c *Client) DeleteMulticastGroup(ctx context.Context, pubkey solana.PublicKey) error {
	c.log.Debug("Deleting multicast group", "host", c.Host, "pubkey", pubkey)
	resp, err := c.grpcClient.DeleteMulticastGroup(ctx, &pb.DeleteMulticastGroupRequest{
		Pubkey: base58.Encode(pubkey[:]),
	})
	if err != nil {
		return fmt.Errorf("failed to delete multicast group on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("failed to delete multicast group on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Multicast group deleted", "host", c.Host, "pubkey", pubkey)
	return nil
}

func (c *Client) MulticastLeave(ctx context.Context, code string) error {
	c.log.Debug("Leaving multicast group", "host", c.Host, "code", code)
	ctx, cancel := context.WithTimeout(ctx, leaveMulticastGroupTimeout)
	defer cancel()
	_, err := c.grpcClient.MulticastLeave(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to leave multicast group on host %s: %w", c.Host, err)
	}
	c.log.Debug("Left multicast group", "host", c.Host, "code", code)
	return nil
}

func (c *Client) MulticastSend(ctx context.Context, group *MulticastGroup, duration time.Duration) error {
	c.log.Debug("Sending multicast data", "host", c.Host, "code", group.Code, "groupIP", group.IP, "duration", duration)
	_, err := c.grpcClient.MulticastSend(ctx, &pb.MulticastSendRequest{
		Group:    group.IP.String(),
		Port:     multicastConnectivityPort,
		Duration: uint32(duration.Seconds()),
	})
	if err != nil {
		return fmt.Errorf("failed to send multicast data on host %s: %w", c.Host, err)
	}
	c.log.Debug("Sent multicast data", "host", c.Host, "code", group.Code)
	return nil
}

func (c *Client) MulticastJoin(ctx context.Context, groups ...*MulticastGroup) error {
	codes := make([]string, len(groups))
	pbGroups := make([]*pb.MulticastGroup, len(groups))
	for i, group := range groups {
		codes[i] = group.Code
		pbGroups[i] = &pb.MulticastGroup{
			Group: group.IP.String(),
			Port:  multicastConnectivityPort,
			Iface: multicastInterfaceName,
		}
	}
	c.log.Debug("Joining multicast groups", "host", c.Host, "codes", codes)
	_, err := c.grpcClient.MulticastJoin(ctx, &pb.MulticastJoinRequest{
		Groups: pbGroups,
	})
	if err != nil {
		return fmt.Errorf("failed to join multicast groups on host %s: %w", c.Host, err)
	}
	c.log.Debug("Joined multicast groups", "host", c.Host, "codes", codes)
	return nil
}

func (c *Client) WaitForMulticastReport(ctx context.Context, group *MulticastGroup) (*pb.MulticastReport, error) {
	reports, err := c.WaitForMulticastReports(ctx, []*MulticastGroup{group})
	if err != nil {
		return nil, err
	}
	return reports[group.IP.String()], nil
}

func (c *Client) WaitForMulticastReports(ctx context.Context, groups []*MulticastGroup) (map[string]*pb.MulticastReport, error) {
	codes := make([]string, len(groups))
	pbGroups := make([]*pb.MulticastGroup, len(groups))
	for i, group := range groups {
		codes[i] = group.Code
		pbGroups[i] = &pb.MulticastGroup{
			Group: group.IP.String(),
			Port:  multicastConnectivityPort,
			Iface: multicastInterfaceName,
		}
	}
	c.log.Debug("Waiting for multicast reports", "host", c.Host, "codes", codes)

	var reports map[string]*pb.MulticastReport
	err := poll.Until(ctx, func() (bool, error) {
		resp, err := c.grpcClient.MulticastReport(ctx, &pb.MulticastReportRequest{
			Groups: pbGroups,
		})
		if err != nil {
			return false, fmt.Errorf("failed to get multicast report on host %s: %w", c.Host, err)
		}
		if len(resp.Reports) == 0 {
			return false, nil
		}
		// Check all groups have received packets
		allReceived := true
		for _, group := range groups {
			report := resp.Reports[group.IP.String()]
			if report == nil || report.PacketCount == 0 {
				c.log.Debug("Waiting for multicast report", "host", c.Host, "code", group.Code, "groupIP", group.IP, "packetCount", 0)
				allReceived = false
			} else {
				c.log.Debug("Got multicast report", "host", c.Host, "code", group.Code, "groupIP", group.IP, "packetCount", report.PacketCount)
			}
		}
		if allReceived {
			reports = resp.Reports
		}
		return allReceived, nil
	}, waitForMulticastReportTimeout, waitInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for multicast reports on host %s: %w", c.Host, err)
	}
	c.log.Debug("Confirmed multicast reports", "host", c.Host, "codes", codes)
	return reports, nil
}

func (c *Client) AddPublisherToMulticastGroupAllowlist(ctx context.Context, code string, pubkey solana.PublicKey, clientIP string) error {
	return c.AddToMulticastGroupAllowlist(ctx, code, pb.MulticastAllowListAddRequest_PUBLISHER, pubkey, clientIP)
}

func (c *Client) AddSubscriberToMulticastGroupAllowlist(ctx context.Context, code string, pubkey solana.PublicKey, clientIP string) error {
	return c.AddToMulticastGroupAllowlist(ctx, code, pb.MulticastAllowListAddRequest_SUBSCRIBER, pubkey, clientIP)
}

func (c *Client) AddToMulticastGroupAllowlist(ctx context.Context, code string, mode pb.MulticastAllowListAddRequest_MulticastMode, pubkey solana.PublicKey, clientIP string) error {
	c.log.Debug("Adding to multicast group allowlist", "host", c.Host, "code", code, "pubkey", pubkey, "clientIP", clientIP)
	resp, err := c.grpcClient.MulticastAllowListAdd(ctx, &pb.MulticastAllowListAddRequest{
		Mode:     mode,
		Code:     code,
		Pubkey:   base58.Encode(pubkey[:]),
		ClientIp: clientIP,
	})
	if err != nil {
		return fmt.Errorf("failed to add to multicast group allowlist on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("failed to add to multicast group allowlist on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Added to multicast group allowlist", "host", c.Host, "code", code, "pubkey", pubkey, "clientIP", clientIP)
	return nil
}

// CleanupStaleTestGroups deletes all multicast groups matching the qa-test-group-* pattern.
// This cleans up leftovers from previous test runs. Individual deletion failures are logged
// but do not stop the cleanup. Returns the number of groups deleted.
func (c *Client) CleanupStaleTestGroups(ctx context.Context) (int, error) {
	data, err := getProgramDataWithRetry(ctx, c.serviceability)
	if err != nil {
		return 0, fmt.Errorf("failed to get program data: %w", err)
	}

	deleted := 0
	for _, group := range data.MulticastGroups {
		if !strings.HasPrefix(group.Code, "qa-test-group-") {
			continue
		}
		pk := solana.PublicKeyFromBytes(group.PubKey[:])
		c.log.Debug("Deleting stale test group", "code", group.Code, "pubkey", pk)
		if err := c.DeleteMulticastGroup(ctx, pk); err != nil {
			c.log.Info("Failed to delete stale test group", "code", group.Code, "error", err)
			continue
		}
		deleted++
	}
	return deleted, nil
}
