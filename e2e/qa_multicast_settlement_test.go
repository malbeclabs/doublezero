//go:build qa

package e2e

import (
	"context"
	"flag"
	"log/slog"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

var enableSettlementTests = flag.Bool("enable-multicast-settlement-tests", false, "enable multicast settlement tests")

// TestQA_MulticastSettlement pays for a full (leader) multicast seat on the
// closest device and verifies pricing, tunnel-up, and the withdraw/refund
// accounting. It is a thin wrapper over the shared settlement flow in
// runShredSettlement; TestQA_RetransmitOnlySettlement mirrors it with
// retransmit-only device selection and a group-subscription assertion. Any fix
// to the settlement machinery belongs in runShredSettlement so both stay in
// sync.
func TestQA_MulticastSettlement(t *testing.T) {
	runShredSettlement(t, shredSettlementParams{
		enabled:           *enableSettlementTests,
		skipReason:        "Skipping: --enable-multicast-settlement-tests flag not set",
		selectSubtestName: "find_closest_device",
		selectDevice:      selectClosestDevice,
		priceLogMsg:       "Found epoch price",
	})
}

// selectClosestDevice picks the reachable device with the lowest latency,
// regardless of metro flags. It backs TestQA_MulticastSettlement.
func selectClosestDevice(t *testing.T, ctx context.Context, log *slog.Logger, _ *qa.Test, client *qa.Client) *qa.Device {
	device, err := client.ClosestDevice(ctx)
	require.NoError(t, err, "failed to find closest device")
	log.Info("Closest device", "code", device.Code, "pubkey", device.PubKey)
	return device
}
