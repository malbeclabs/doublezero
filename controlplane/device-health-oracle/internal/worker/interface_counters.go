package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// InterfaceCountersCriterion checks that a device has at least one interface
// counter record per minute over the burn-in period by querying the ClickHouse
// fact_dz_device_interface_counters table.
//
// The burn-in start times are resolved from ledger slot numbers via GetBlockTime
// and passed through the context (see BurnInTimes / ContextWithBurnInTimes).
type InterfaceCountersCriterion struct {
	checker InterfaceCountersChecker
	log     *slog.Logger
}

func NewInterfaceCountersCriterion(checker InterfaceCountersChecker, log *slog.Logger) *InterfaceCountersCriterion {
	return &InterfaceCountersCriterion{
		checker: checker,
		log:     log,
	}
}

func (c *InterfaceCountersCriterion) Name() string {
	return "interface_counters"
}

func (c *InterfaceCountersCriterion) Check(ctx context.Context, device serviceability.Device) (bool, string) {
	start, now, expectedMinutes, ok := DeviceBurnIn(ctx, device.Status)
	if !ok {
		return false, "burn-in times not available in context"
	}
	if expectedMinutes == 0 {
		return true, ""
	}

	pubkey := solana.PublicKeyFromBytes(device.PubKey[:]).String()
	minutesWithRecords, err := c.checker.InterfaceCountersCoverage(ctx, pubkey, start, now)
	if err != nil {
		c.log.Error("Failed to query interface counters coverage",
			"device", pubkey, "code", device.Code, "error", err)
		return false, fmt.Sprintf("clickhouse query failed: %v", err)
	}

	c.log.Debug("Interface counters coverage",
		"device", pubkey, "code", device.Code,
		"minutesWithRecords", minutesWithRecords,
		"expectedMinutes", expectedMinutes,
		"start", start)

	if minutesWithRecords < expectedMinutes {
		return false, fmt.Sprintf("interface counters cover %d/%d minutes", minutesWithRecords, expectedMinutes)
	}

	return true, ""
}
