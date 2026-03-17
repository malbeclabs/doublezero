//go:build qa

package e2e

import (
	"flag"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

var seatAmountFlag = flag.String("seat-amount", "", "USDC amount for seat payment in settlement test")

func TestQA_MulticastSettlement(t *testing.T) {
	if *seatAmountFlag == "" {
		t.Skip("Skipping: --seat-amount flag not provided")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")

	// Use a single client for the settlement test.
	client := test.RandomClient()
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

	// Step 6: Placeholder — instant withdraw.
	t.Run("instant_withdraw", func(t *testing.T) {
		t.Skip("instant withdraw CLI flag not yet implemented in doublezero-solana")

		// When implemented, this subtest should:
		// err := client.SeatWithdraw(ctx, device.PubKey, true)
		// require.NoError(t, err, "failed to withdraw seat")
		//
		// err = client.WaitForStatusDisconnected(ctx)
		// require.NoError(t, err, "tunnel did not come down after seat withdrawal")
		//
		// TODO: Also verify onchain seat removal (query ClientSeat account and confirm it's closed).
	})
}
