package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// ControllerSuccessCriterion checks that a device has called the controller
// at least once per minute over the burn-in period by querying the ClickHouse
// controller_grpc_getconfig_success table.
//
// The burn-in start times are resolved from ledger slot numbers via GetBlockTime
// and passed through the context (see BurnInTimes / ContextWithBurnInTimes).
type ControllerSuccessCriterion struct {
	checker ControllerCallChecker
	log     *slog.Logger
}

// NewControllerSuccessCriterion creates a new ControllerSuccessCriterion.
func NewControllerSuccessCriterion(checker ControllerCallChecker, log *slog.Logger) *ControllerSuccessCriterion {
	return &ControllerSuccessCriterion{
		checker: checker,
		log:     log,
	}
}

func (c *ControllerSuccessCriterion) Name() string {
	return "controller_success"
}

func (c *ControllerSuccessCriterion) Check(ctx context.Context, device serviceability.Device) (bool, string) {
	start, expectedMinutes, ok := DeviceBurnIn(ctx, device.Status)
	if !ok {
		return false, "burn-in times not available in context"
	}
	if expectedMinutes == 0 {
		return true, ""
	}

	pubkey := solana.PublicKeyFromBytes(device.PubKey[:]).String()
	minutesWithCalls, err := c.checker.ControllerCallCoverage(ctx, pubkey, start, time.Now())
	if err != nil {
		c.log.Error("Failed to query controller call coverage",
			"device", pubkey, "code", device.Code, "error", err)
		return false, fmt.Sprintf("clickhouse query failed: %v", err)
	}

	c.log.Debug("Controller call coverage",
		"device", pubkey, "code", device.Code,
		"minutesWithCalls", minutesWithCalls,
		"expectedMinutes", expectedMinutes,
		"start", start)

	if minutesWithCalls < expectedMinutes {
		return false, fmt.Sprintf("controller calls cover %d/%d minutes", minutesWithCalls, expectedMinutes)
	}

	return true, ""
}
