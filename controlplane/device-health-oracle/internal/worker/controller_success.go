package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const defaultSlotDurationMs = 400

// ControllerSuccessCriterion checks that a device has called the controller
// at least once per minute over the burn-in period by querying the ClickHouse
// controller_grpc_getconfig_success table.
type ControllerSuccessCriterion struct {
	checker               ControllerCallChecker
	provisioningSlotCount uint64
	drainedSlotCount      uint64
	slotDurationMs        uint64
	log                   *slog.Logger
}

// NewControllerSuccessCriterion creates a new ControllerSuccessCriterion.
func NewControllerSuccessCriterion(
	checker ControllerCallChecker,
	provisioningSlotCount, drainedSlotCount, slotDurationMs uint64,
	log *slog.Logger,
) *ControllerSuccessCriterion {
	if slotDurationMs == 0 {
		slotDurationMs = defaultSlotDurationMs
	}
	return &ControllerSuccessCriterion{
		checker:               checker,
		provisioningSlotCount: provisioningSlotCount,
		drainedSlotCount:      drainedSlotCount,
		slotDurationMs:        slotDurationMs,
		log:                   log,
	}
}

func (c *ControllerSuccessCriterion) Name() string {
	return "controller_success"
}

func (c *ControllerSuccessCriterion) Check(ctx context.Context, device serviceability.Device) (bool, string) {
	burnInSlots := c.burnInSlotCount(device.Status)
	duration := time.Duration(burnInSlots*c.slotDurationMs) * time.Millisecond
	now := time.Now()
	start := now.Add(-duration)

	expectedMinutes := int64(duration.Minutes())
	if expectedMinutes == 0 {
		return true, ""
	}

	devicePubkey := solana.PublicKeyFromBytes(device.PubKey[:]).String()

	minutesWithCalls, err := c.checker.ControllerCallCoverage(ctx, devicePubkey, start, now)
	if err != nil {
		c.log.Error("Failed to query controller call coverage",
			"device", devicePubkey,
			"code", device.Code,
			"error", err)
		return false, fmt.Sprintf("clickhouse query failed: %v", err)
	}

	c.log.Debug("Controller call coverage",
		"device", devicePubkey,
		"code", device.Code,
		"minutesWithCalls", minutesWithCalls,
		"expectedMinutes", expectedMinutes,
		"burnInSlots", burnInSlots,
		"duration", duration)

	if minutesWithCalls < expectedMinutes {
		return false, fmt.Sprintf("controller calls cover %d/%d minutes", minutesWithCalls, expectedMinutes)
	}

	return true, ""
}

// burnInSlotCount returns the appropriate burn-in slot count based on the device's current status.
func (c *ControllerSuccessCriterion) burnInSlotCount(status serviceability.DeviceStatus) uint64 {
	switch status {
	case serviceability.DeviceStatusDrained:
		return c.drainedSlotCount
	default:
		return c.provisioningSlotCount
	}
}
