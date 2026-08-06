//go:build qa

package e2e

import (
	"context"
	"flag"
	"log/slog"
	"strconv"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

var enableSettlementTests = flag.Bool("enable-multicast-settlement-tests", false, "enable multicast settlement tests")

// TestQA_MulticastSettlement pays for a multicast seat and verifies pricing,
// tunnel-up, and the withdraw/refund accounting. It is a thin wrapper over the
// shared settlement flow in runShredSettlement; TestQA_RetransmitOnlySettlement
// mirrors it with retransmit-only device selection and a group-subscription
// assertion. Any fix to the settlement machinery belongs in runShredSettlement
// so both stay in sync.
func TestQA_MulticastSettlement(t *testing.T) {
	var onboardingEnforced bool
	runShredSettlement(t, shredSettlementParams{
		enabled:    *enableSettlementTests,
		skipReason: "Skipping: --enable-multicast-settlement-tests flag not set",

		preflightSubtestName: "reject_new_seat_outside_retransmit_only_metro",
		preflight: func(t *testing.T, ctx context.Context, log *slog.Logger, _ *qa.Test, client *qa.Client) {
			var err error
			onboardingEnforced, err = client.IsRetransmitOnlyOnboardingEnforced(ctx)
			require.NoError(t, err, "failed to read the retransmit-only onboarding flag")
			if !onboardingEnforced {
				log.Info("Retransmit-only onboarding is off; settling on the closest device")
				t.Skip("Skipping: the program config does not enforce retransmit-only onboarding")
			}
			log.Info("Retransmit-only onboarding is on; a new seat must be rejected outside a retransmit-only metro")
			assertNewSeatRejected(t, ctx, log, client)
		},

		selectSubtestName: "select_device",
		selectDevice: func(t *testing.T, ctx context.Context, log *slog.Logger, test *qa.Test, client *qa.Client) *qa.Device {
			if onboardingEnforced {
				return selectRetransmitOnlyDevice(t, ctx, log, test, client)
			}
			return selectClosestDevice(t, ctx, log, test, client)
		},

		priceLogMsg: "Found epoch price",

		extraSubtestName: "assert_subscribed_groups",
		extraAssertion: func(t *testing.T, ctx context.Context, log *slog.Logger, client *qa.Client, device *qa.Device) {
			if !onboardingEnforced {
				t.Skip("Skipping: the seat is not in a retransmit-only metro, so it carries the leader group too")
			}
			assertSubscribedGroups(t, ctx, log, client, device)
		},
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

// assertNewSeatRejected pays for a seat on the closest device outside a
// retransmit-only metro and requires the program to reject it.
func assertNewSeatRejected(t *testing.T, ctx context.Context, log *slog.Logger, client *qa.Client) {
	device, err := client.ClosestNonRetransmitOnlyDevice(ctx)
	require.NoError(t, err, "failed to find a device outside a retransmit-only metro")
	if device == nil {
		t.Skip("Skipping: every reachable metro is flagged retransmit-only, so no metro rejects a new seat")
	}

	prices, err := client.SeatPrices(ctx, device.PubKey)
	require.NoError(t, err, "failed to read the onchain seat price for device %s", device.Code)
	require.NotZero(t, prices.InstantAllocationDollars, "onchain instant-allocation price is zero for device %s", device.Code)
	amount := strconv.FormatUint(prices.InstantAllocationDollars, 10)

	log.Info("Paying for a seat outside a retransmit-only metro", "device", device.Code,
		"metro", device.ExchangeCode, "amount", amount)
	err = client.FeedSeatPay(ctx, device.PubKey, amount)
	if err == nil {
		// The settlement flow's cleanup only withdraws the device it selects, so
		// withdraw this seat here or it stays active onchain.
		if withdrawErr := client.WithdrawSeatWithRetry(ctx, device.PubKey); withdrawErr != nil {
			log.Warn("Cleanup: withdraw of the wrongly admitted seat failed; seat left active onchain",
				"device", device.Code, "error", withdrawErr)
		}
	}
	require.Error(t, err, "the program admitted a new seat on device %s in metro %s, which is not retransmit-only",
		device.Code, device.ExchangeCode)
	require.ErrorContains(t, err, "Retransmit-only onboarding enforced: cannot fund seat",
		"the payment failed for another reason than retransmit-only onboarding enforcement")
	require.ErrorContains(t, err, "with no tenure",
		"the payment failed for another reason than retransmit-only onboarding enforcement")
	log.Info("The program rejected the new seat", "device", device.Code, "metro", device.ExchangeCode)
}
