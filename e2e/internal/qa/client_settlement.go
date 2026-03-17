package qa

import (
	"context"
	"fmt"
	"math"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Enable calls the Enable RPC to start the doublezerod reconciler.
func (c *Client) Enable(ctx context.Context) error {
	c.log.Debug("Enabling reconciler", "host", c.Host)
	resp, err := c.grpcClient.Enable(ctx, &emptypb.Empty{})
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

// SeatPay calls the SeatPay RPC to pay for a seat reservation on a device.
// The client's public IP is auto-filled. When instant is true, the --now flag is used.
func (c *Client) SeatPay(ctx context.Context, devicePubkey string, amount string, instant bool) error {
	c.log.Debug("Paying for seat", "host", c.Host, "device", devicePubkey, "amount", amount, "instant", instant)
	resp, err := c.grpcClient.SeatPay(ctx, &pb.SeatPayRequest{
		DevicePubkey: devicePubkey,
		ClientIp:     c.publicIP.To4().String(),
		Amount:       amount,
		Instant:      instant,
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

// SeatWithdraw calls the SeatWithdraw RPC to withdraw a seat reservation.
// This is a placeholder — the instant withdraw CLI flag has not been implemented yet.
func (c *Client) SeatWithdraw(ctx context.Context, devicePubkey string, instant bool) error {
	c.log.Debug("Withdrawing seat", "host", c.Host, "device", devicePubkey, "instant", instant)
	resp, err := c.grpcClient.SeatWithdraw(ctx, &pb.SeatWithdrawRequest{
		DevicePubkey: devicePubkey,
		ClientIp:     c.publicIP.To4().String(),
		Instant:      instant,
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
