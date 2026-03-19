//go:build qa

package e2e

import (
	"flag"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

var (
	enableSettlementTests = flag.Bool("enable-settlement-tests", false, "enable multicast settlement tests")
	seatAmountFlag        = flag.String("seat-amount", "100", "USDC amount for seat payment in settlement test")
	settlementHostFlag    = flag.String("settlement-host", "", "specific host to use for settlement test (default: random)")
)

func TestQA_MulticastSettlement(t *testing.T) {
	if !*enableSettlementTests {
		t.Skip("Skipping: --enable-settlement-tests flag not set")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")

	// Use a specific client if provided, otherwise pick a random one.
	var client *qa.Client
	if *settlementHostFlag != "" {
		client = test.GetClient(*settlementHostFlag)
		require.NotNil(t, client, "host %q not found", *settlementHostFlag)
	} else {
		client = test.RandomClient()
	}
	log.Info("Selected client", "host", client.Host)

	// Dump diagnostics on failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		client.DumpDiagnostics(nil)
	})

	// Step 1: Enable the reconciler.
	log.Info("Enabling reconciler")
	err = client.Enable(ctx)
	require.NoError(t, err, "failed to enable reconciler")

	// Step 2: Find the closest device by latency.
	log.Info("Finding closest device")
	device, err := client.ClosestDevice(ctx)
	require.NoError(t, err, "failed to find closest device")
	log.Info("Closest device", "code", device.Code, "pubkey", device.PubKey)

	// Step 3: Pay for an instant seat on the closest device.
	log.Info("Paying for instant seat", "device", device.Code, "amount", *seatAmountFlag)
	err = client.SeatPay(ctx, device.PubKey, *seatAmountFlag, true)
	require.NoError(t, err, "failed to pay for seat")

	// Step 4: Validate tunnel comes up.
	log.Info("Waiting for tunnel to come up")
	err = client.WaitForStatusUp(ctx)
	require.NoError(t, err, "tunnel did not come up after seat payment")

	// Step 5: Validate the device assignment matches.
	statuses, err := client.GetUserStatuses(ctx)
	require.NoError(t, err, "failed to get user statuses")
	ibrlStatus := qa.FindIBRLStatus(statuses)
	require.NotNil(t, ibrlStatus, "no IBRL status found after seat payment")
	require.Equal(t, device.Code, ibrlStatus.CurrentDevice, "tunnel connected to wrong device")
	log.Info("Tunnel up and device matches", "device", ibrlStatus.CurrentDevice, "dzIP", ibrlStatus.DoubleZeroIp)

	// Step 6: Instant withdraw — request immediate seat withdrawal and validate tunnel teardown.
	log.Info("Withdrawing seat instantly", "device", device.Code)
	err = client.SeatWithdraw(ctx, device.PubKey, true)
	require.NoError(t, err, "failed to withdraw seat")

	log.Info("Waiting for tunnel to come down")
	err = client.WaitForStatusDisconnected(ctx)
	require.NoError(t, err, "tunnel did not come down after seat withdrawal")
}
