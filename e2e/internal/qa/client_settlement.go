package qa

import (
	"context"
	"fmt"
	"math"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"google.golang.org/protobuf/types/known/emptypb"
)

// FeedEnable calls the FeedEnable RPC to start the doublezerod reconciler.
func (c *Client) FeedEnable(ctx context.Context) error {
	c.log.Debug("Enabling reconciler", "host", c.Host)
	resp, err := c.grpcClient.FeedEnable(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to enable reconciler on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("enable failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Reconciler enabled", "host", c.Host)
	return nil
}

// ClosestDevice returns the reachable device with the lowest average latency.
// It calls GetLatency and looks up the result in the client's devices map.
func (c *Client) ClosestDevice(ctx context.Context) (*Device, error) {
	latencies, err := c.GetLatency(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latency on host %s: %w", c.Host, err)
	}

	var bestLatency *pb.Latency
	var bestAvg uint64 = math.MaxUint64
	for _, l := range latencies {
		if !l.Reachable {
			continue
		}
		if l.AvgLatencyNs < bestAvg {
			bestAvg = l.AvgLatencyNs
			bestLatency = l
		}
	}
	if bestLatency == nil {
		return nil, fmt.Errorf("no reachable devices found on host %s", c.Host)
	}

	// Look up device by code in the devices map.
	device, ok := c.devices[bestLatency.DeviceCode]
	if !ok {
		return nil, fmt.Errorf("closest device %q (pk=%s) not found in devices map on host %s", bestLatency.DeviceCode, bestLatency.DevicePk, c.Host)
	}

	c.log.Debug("Determined closest device", "host", c.Host, "deviceCode", device.Code, "avgLatencyNs", bestAvg)
	return device, nil
}

// FeedSeatPrice calls the FeedSeatPrice RPC to query device seat prices.
func (c *Client) FeedSeatPrice(ctx context.Context) ([]*pb.DevicePrice, error) {
	c.log.Debug("Querying seat prices", "host", c.Host)
	resp, err := c.grpcClient.FeedSeatPrice(ctx, &pb.FeedSeatPriceRequest{
		SolanaRpcUrl:               c.SolanaRPCURL,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get seat prices on host %s: %w", c.Host, err)
	}
	c.log.Debug("Seat prices retrieved", "host", c.Host, "count", len(resp.GetPrices()))
	return resp.GetPrices(), nil
}

// FeedSeatPay calls the FeedSeatPay RPC to pay for a seat on a device.
// The client's public IP is auto-filled. Instant allocation is the default.
func (c *Client) FeedSeatPay(ctx context.Context, devicePubkey string, amount string) error {
	c.log.Debug("Paying for seat", "host", c.Host, "device", devicePubkey, "amount", amount)
	resp, err := c.grpcClient.FeedSeatPay(ctx, &pb.FeedSeatPayRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		Amount:                     amount,
		SolanaRpcUrl:               c.SolanaRPCURL,
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
	})
	if err != nil {
		return fmt.Errorf("failed to pay for seat on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		c.log.Error("Seat payment failed", "host", c.Host, "device", devicePubkey, "output", resp.GetOutput())
		return fmt.Errorf("seat payment failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Seat payment successful", "host", c.Host, "device", devicePubkey)
	return nil
}

// FeedSeatWithdraw calls the FeedSeatWithdraw RPC to withdraw a seat from a device.
// Instant withdrawal is the default.
func (c *Client) FeedSeatWithdraw(ctx context.Context, devicePubkey string) error {
	c.log.Debug("Withdrawing seat", "host", c.Host, "device", devicePubkey)
	resp, err := c.grpcClient.FeedSeatWithdraw(ctx, &pb.FeedSeatWithdrawRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		SolanaRpcUrl:               c.SolanaRPCURL,
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
	})
	if err != nil {
		return fmt.Errorf("failed to withdraw seat on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("seat withdrawal failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Seat withdrawal successful", "host", c.Host, "device", devicePubkey)
	return nil
}
