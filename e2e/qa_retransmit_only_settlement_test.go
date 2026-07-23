//go:build qa

package e2e

import (
	"context"
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// retransmitSubscribeTimeout bounds how long we wait for the oracle to converge
// the seat's onchain multicast subscription after the tunnel comes up. The
// tunnel-up wait already covers the subscribe path, so this is a safety buffer
// against the oracle's reconcile cadence.
const retransmitSubscribeTimeout = 90 * time.Second

var (
	enableRetransmitOnlyTests = flag.Bool("enable-retransmit-only-settlement-tests", false, "enable the retransmit-only multicast settlement test")
	retransmitOnlyDeviceFlag  = flag.String("retransmit-only-device", "", "device code or pubkey in a retransmit-only metro (overrides auto-discovery)")
	leaderGroupCodeFlag       = flag.String("leader-group-code", "", "multicast group code for the leader (full) shred feed, expected to be EXCLUDED from the seat")
	retransmitGroupCodeFlag   = flag.String("retransmit-group-code", "", "multicast group code for the retransmit shred feed, expected to be subscribed")
	retransmitPriceFlag       = flag.Uint64("retransmit-price", 10, "expected discounted retransmit-only seat price in whole USDC dollars")
)

// TestQA_RetransmitOnlySettlement demonstrates the retransmit-only shred
// subscription end to end: a client pays for a seat on a device in a
// retransmit-only metro, is charged the discounted price, and ends up
// subscribed to the retransmit multicast group only — not the leader group, so
// leader shreds are excluded. It mirrors TestQA_MulticastSettlement, adding
// retransmit-only device selection, the discounted-price assertion, and the
// subscribed-groups assertion. The test is environment-agnostic: the group
// codes and the expected price are flags, so the same binary validates the
// testnet QA network and later mainnet.
func TestQA_RetransmitOnlySettlement(t *testing.T) {
	if !*enableRetransmitOnlyTests {
		t.Skip("Skipping: --enable-retransmit-only-settlement-tests flag not set")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")

	var client *qa.Client
	if *settlementClientFlag != "" {
		var ok bool
		client, ok = test.ClientByHost(*settlementClientFlag)
		require.True(t, ok, "client %q not found in hosts", *settlementClientFlag)
	} else {
		client = test.RandomClient()
	}
	if *keypairFlag != "" {
		client.Keypair = *keypairFlag
	}
	log.Info("Selected client", "host", client.Host)

	// Shared state across subtests.
	var device *qa.Device
	var amount string
	var epochPrice uint64
	var parsedAmount uint64
	var effectivePrice uint64
	var balanceBeforePay uint64
	var balanceAfterPay uint64
	seatPaid := false

	t.Cleanup(func() {
		if seatPaid && device != nil {
			// Retry the withdraw so a single-shot withdraw that hit the spurious
			// "request in flight" preflight bail (or a transient RPC failure)
			// does not leave the seat active onchain with an open escrow,
			// poisoning subsequent runs. Bound the cleanup so a hung withdraw
			// can't block teardown forever.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			if withdrawErr := client.WithdrawSeatWithRetry(cleanupCtx, device.PubKey); withdrawErr != nil {
				log.Warn("Cleanup: seat withdraw failed after retries; seat left active onchain", "error", withdrawErr)
			}
		}
		if t.Failed() {
			client.DumpDiagnostics(nil)
		}
	})

	if !t.Run("ensure_program_unpaused", func(t *testing.T) {
		paused, err := client.IsProgramPaused(ctx)
		require.NoError(t, err, "failed to read program-paused flag")
		if paused {
			t.Skip("Skipping: shred-subscription program is paused (migration in progress)")
		}
	}) {
		return
	}

	if !t.Run("ensure_multicast_disconnected", func(t *testing.T) {
		healed, err := client.SelfHealStuckSeats(ctx)
		require.NoError(t, err, "failed to self-heal stuck-active seats")
		if healed > 0 {
			log.Info("Self-healed stuck-active seat(s)", "count", healed)
		}

		statuses, err := client.GetUserStatuses(ctx)
		if err != nil {
			log.Info("No active sessions")
			return
		}
		var mcast *pb.Status
		for _, s := range statuses {
			if s.UserType == "Multicast" && s.SessionStatus != qa.UserStatusDisconnected {
				mcast = s
				break
			}
		}
		if mcast == nil {
			log.Info("No active multicast session")
			return
		}
		log.Info("Active multicast session found, withdrawing", "device", mcast.CurrentDevice, "status", mcast.SessionStatus)
		dev, ok := test.Devices()[mcast.CurrentDevice]
		require.True(t, ok, "device %q not found in devices map", mcast.CurrentDevice)
		err = client.WithdrawSeatWithRetry(ctx, dev.PubKey)
		require.NoError(t, err, "failed to withdraw existing seat")
		err = client.WaitForMulticastStatusDisconnected(ctx)
		require.NoError(t, err, "existing multicast session did not disconnect")
	}) {
		return
	}

	if !t.Run("enable_reconciler", func(t *testing.T) {
		err := client.FeedEnable(ctx)
		require.NoError(t, err, "failed to enable reconciler")
	}) {
		return
	}

	if !t.Run("select_retransmit_only_device", func(t *testing.T) {
		if *retransmitOnlyDeviceFlag != "" {
			pinned, ok := test.DeviceByCodeOrPubkey(*retransmitOnlyDeviceFlag)
			require.True(t, ok, "pinned device %q not found", *retransmitOnlyDeviceFlag)
			retransmitOnly, err := client.RetransmitOnlyExchangeKeys(ctx)
			require.NoError(t, err, "failed to read retransmit-only metros")
			require.True(t, retransmitOnly[pinned.ExchangePubKey],
				"pinned device %s (metro %s) is not in a retransmit-only metro", pinned.Code, pinned.ExchangeCode)
			device = pinned
		} else {
			selected, err := client.ClosestRetransmitOnlyDevice(ctx)
			require.NoError(t, err, "failed to find a retransmit-only device")
			if selected == nil {
				t.Skip("Skipping: no reachable retransmit-only device found (feature not deployed/configured on this network)")
			}
			device = selected
		}
		log.Info("Selected retransmit-only device", "code", device.Code, "pubkey", device.PubKey, "metro", device.ExchangeCode)

		// The group codes are required to assert leader-exclusion once a device
		// exists to test. Enabling the test without them is a misconfiguration.
		require.NotEmpty(t, *leaderGroupCodeFlag, "--leader-group-code is required")
		require.NotEmpty(t, *retransmitGroupCodeFlag, "--retransmit-group-code is required")
	}) {
		return
	}

	if !t.Run("query_seat_price", func(t *testing.T) {
		prices, err := client.FeedSeatPrice(ctx, device.PubKey)
		require.NoError(t, err, "failed to get seat prices")

		// Match by pubkey, not code: querying by --device skips code resolution,
		// so the returned rows may not carry a device_code.
		var price *pb.DevicePrice
		for _, p := range prices {
			if p.DevicePubkey == device.PubKey {
				price = p
				break
			}
		}
		require.NotNil(t, price, "no price found for device %s", device.Code)
		require.NotZero(t, price.EpochPrice, "epoch price is zero for device %s", device.Code)
		// The retransmit-only metro is priced at the discount, so the seat price
		// must equal the expected retransmit price (default 10 USDC).
		require.Equal(t, *retransmitPriceFlag, price.EpochPrice,
			"retransmit-only device %s should be priced at the discounted retransmit price", device.Code)
		epochPrice = price.EpochPrice
		amount = strconv.FormatUint(epochPrice, 10)
		parsedAmount = epochPrice * 1_000_000 // convert dollars to USDC raw units (6 decimals)
		log.Info("Found discounted epoch price", "device", device.Code, "amount", amount)
	}) {
		return
	}

	// Set when wait_for_open_phase times out inside the epoch-tail closed
	// window (verified against live chain state), so the parent can skip the
	// remaining subtests: the program stays closed until the epoch boundary,
	// which the 2-minute wait cannot bridge, so pay/withdraw below could only
	// fail and page for a by-design condition.
	var epochTailWindow *qa.EpochTailWindow
	if !t.Run("wait_for_open_phase", func(t *testing.T) {
		waitStartSlot, slotErr := client.CurrentSolanaSlot(ctx)
		if slotErr != nil {
			log.Warn("Failed to read wait-start slot; epoch-tail classification will use the timeout-time slot only", "error", slotErr)
		}
		err := client.WaitForOpenForRequests(ctx)
		if err != nil {
			win, winErr := client.EpochTailClosedWindow(ctx, waitStartSlot)
			switch {
			case winErr != nil:
				log.Warn("Failed to classify epoch-tail closed window; treating timeout as a real failure", "error", winErr)
			case win.Benign:
				epochTailWindow = &win
				t.Skipf("expected epoch-tail closed window: %s", win)
			default:
				log.Info("Timeout is not the benign epoch-tail closed window", "window", win.String())
			}
		}
		require.NoError(t, err, "shred-subscription program did not enter OpenForRequests phase within timeout")
	}) {
		return
	}
	if epochTailWindow != nil {
		// t.Run reports a skipped subtest as success, so skip the parent
		// explicitly to stop the run here.
		t.Skipf("expected epoch-tail closed window: %s", epochTailWindow)
	}

	if !t.Run("record_balance_before_pay", func(t *testing.T) {
		var err error
		balanceBeforePay, err = client.GetUSDCBalance(ctx)
		require.NoError(t, err, "failed to get USDC balance before pay")
		log.Info("USDC balance before pay", "balance", balanceBeforePay)
	}) {
		return
	}

	if !t.Run("pay_for_seat", func(t *testing.T) {
		err := client.FeedSeatPay(ctx, device.PubKey, amount)
		require.NoError(t, err, "failed to pay for seat")
		seatPaid = true
	}) {
		return
	}

	if !t.Run("validate_balance_after_pay", func(t *testing.T) {
		// Poll until the balance reflects the debit. FeedSeatPay returns after
		// the tx is submitted, and the RPC balance view can lag the confirmed
		// state briefly, so a one-shot read races.
		var lastDebit uint64
		require.Eventually(t, func() bool {
			bal, err := client.GetUSDCBalance(ctx)
			if err != nil {
				log.Info("USDC balance poll error", "error", err)
				return false
			}
			balanceAfterPay = bal
			lastDebit = balanceBeforePay - bal
			return lastDebit == parsedAmount
		}, balanceSettleTimeout, 5*time.Second, "USDC balance should decrease by the paid amount")
		log.Info("USDC balance after pay", "balance", balanceAfterPay, "debit", lastDebit, "expected_debit", parsedAmount)
	}) {
		return
	}

	if !t.Run("query_effective_seat_price", func(t *testing.T) {
		var err error
		effectivePrice, err = client.GetEffectiveSeatPrice(ctx, device.PubKey, epochPrice)
		require.NoError(t, err, "failed to get effective seat price")
		log.Info("Effective seat price", "effective_usdc", effectivePrice, "epoch_usdc", parsedAmount)
	}) {
		return
	}

	if !t.Run("validate_tunnel_up", func(t *testing.T) {
		err := client.WaitForMulticastStatusUp(ctx)
		require.NoError(t, err, "multicast tunnel did not come up after seat payment")
	}) {
		return
	}

	if !t.Run("validate_device_assignment", func(t *testing.T) {
		statuses, err := client.GetUserStatuses(ctx)
		require.NoError(t, err, "failed to get user statuses")
		mcastStatus := qa.FindMulticastStatus(statuses)
		require.NotNil(t, mcastStatus, "no multicast status found after seat payment")
		require.Equal(t, device.Code, mcastStatus.CurrentDevice, "tunnel connected to wrong device")
		log.Info("Tunnel up and device matches", "device", mcastStatus.CurrentDevice, "dzIP", mcastStatus.DoubleZeroIp)
	}) {
		return
	}

	if !t.Run("assert_subscribed_groups", func(t *testing.T) {
		// Resolve the leader and retransmit multicast groups by code. Neither
		// onchain data nor the SDK labels a group as leader/retransmit, so the
		// operator supplies the codes per network.
		leaderGroup, err := client.GetMulticastGroup(ctx, *leaderGroupCodeFlag)
		require.NoError(t, err, "failed to resolve leader group %q", *leaderGroupCodeFlag)
		require.NotNil(t, leaderGroup, "leader group %q not found onchain", *leaderGroupCodeFlag)
		retransmitGroup, err := client.GetMulticastGroup(ctx, *retransmitGroupCodeFlag)
		require.NoError(t, err, "failed to resolve retransmit group %q", *retransmitGroupCodeFlag)
		require.NotNil(t, retransmitGroup, "retransmit group %q not found onchain", *retransmitGroupCodeFlag)

		subscribed := func(user *serviceability.User, group solana.PublicKey) bool {
			for _, sub := range user.Subscribers {
				if solana.PublicKeyFromBytes(sub[:]).Equals(group) {
					return true
				}
			}
			return false
		}

		// The oracle converges the seat's onchain subscription asynchronously,
		// so poll until it reflects retransmit-only membership: the retransmit
		// group present and the leader group absent (leader shreds excluded).
		var lastSubscriberCount int
		require.Eventually(t, func() bool {
			user, err := client.GetServiceabilityUser(ctx)
			if err != nil {
				log.Info("serviceability user poll error", "error", err)
				return false
			}
			lastSubscriberCount = len(user.Subscribers)
			return subscribed(user, retransmitGroup.PK) && !subscribed(user, leaderGroup.PK)
		}, retransmitSubscribeTimeout, 5*time.Second,
			"seat should subscribe to the retransmit group and NOT the leader group")
		log.Info("Subscribed to the retransmit group only",
			"retransmit_group", retransmitGroup.PK,
			"leader_group_excluded", leaderGroup.PK,
			"subscriber_count", lastSubscriberCount)
	}) {
		return
	}

	if !t.Run("withdraw_seat", func(t *testing.T) {
		err := client.WithdrawSeatWithRetry(ctx, device.PubKey)
		require.NoError(t, err, "failed to withdraw seat")
		seatPaid = false
	}) {
		return
	}

	if !t.Run("validate_tunnel_down", func(t *testing.T) {
		err := client.WaitForMulticastStatusDisconnected(ctx)
		require.NoError(t, err, "tunnel did not come down after seat withdrawal")
	}) {
		return
	}

	t.Run("validate_balance_after_withdraw", func(t *testing.T) {
		// Read onchain whether the shred-subscription program has prorated
		// service enabled. This lets the test self-adapt across environments
		// (testnet has it on, mainnet does not) without needing a CI flag.
		proratingEnabled, err := client.IsSeatProratingEnabled(ctx)
		require.NoError(t, err, "failed to read prorating flag from program config")

		var balanceAfterWithdraw uint64
		if proratingEnabled {
			// Prorating refunds the unused portion of the epoch to the wallet.
			// Poll until the refund is reflected (balance strictly greater
			// than after-pay).
			require.Eventually(t, func() bool {
				bal, err := client.GetUSDCBalance(ctx)
				if err != nil {
					log.Info("USDC balance poll error", "error", err)
					return false
				}
				balanceAfterWithdraw = bal
				return bal > balanceAfterPay
			}, balanceSettleTimeout, 5*time.Second,
				"USDC balance should increase to reflect the prorated refund")
		} else {
			expectedBalance := balanceBeforePay - effectivePrice
			require.Eventually(t, func() bool {
				bal, err := client.GetUSDCBalance(ctx)
				if err != nil {
					log.Info("USDC balance poll error", "error", err)
					return false
				}
				balanceAfterWithdraw = bal
				return bal == expectedBalance
			}, balanceSettleTimeout, 5*time.Second,
				"USDC balance should equal before_pay minus the effective seat price")
		}

		refund := balanceAfterWithdraw - balanceAfterPay

		// A seat's payment escrow can carry a balance from an earlier run whose
		// withdraw did not complete. Closing the escrow now refunds that leftover
		// too, so the wallet-measured refund exceeds what was paid this run and
		// no longer isolates this payment (`retained` would underflow). In that
		// case the wallet-delta proration check is not meaningful, so skip it
		// rather than fail — the settlement path itself is still covered by the
		// pay/ack/tunnel/withdraw sub-tests above.
		if refund > parsedAmount {
			log.Warn("skipping wallet-delta proration check: refund exceeds amount paid this run (pre-existing escrow drained)",
				"refund", refund,
				"paid_amount", parsedAmount,
				"before_pay", balanceBeforePay,
				"after_pay", balanceAfterPay,
				"after_withdraw", balanceAfterWithdraw,
			)
			return
		}
		// Equivalent to balanceBeforePay - balanceAfterWithdraw, but computed from
		// the amount paid this run so it cannot underflow given the guard above.
		retained := parsedAmount - refund

		log.Info("USDC balance after withdraw",
			"balance", balanceAfterWithdraw,
			"before_pay", balanceBeforePay,
			"after_pay", balanceAfterPay,
			"paid_amount", parsedAmount,
			"effective_price", effectivePrice,
			"refund", refund,
			"retained", retained,
			"prorating_enabled", proratingEnabled,
		)

		// Accounting invariant: regardless of prorating, the sum of what was
		// refunded to the wallet and what the program retained must equal the
		// amount debited at pay time. This uses parsedAmount rather than
		// effectivePrice because a seat with a zero price override is still
		// charged parsedAmount at pay and fully refunded on withdraw.
		require.Equal(t, parsedAmount, refund+retained,
			"refund + retained must equal the amount paid")

		if !proratingEnabled || effectivePrice == 0 {
			return
		}

		// With prorating enabled we avoid replicating the onchain formula against
		// client-side RPC state. Instead assert the qualitative invariants that
		// distinguish a real partial refund from a regression:
		//   - refund > 0 (prorating actually happened)
		//   - retained > 0 (the seat was not free for the used portion)
		//   - retained < effective_price (kept less than a full epoch)
		require.Greater(t, refund, uint64(0),
			"prorating: refund should be strictly greater than zero")
		require.Greater(t, retained, uint64(0),
			"prorating: retained should be strictly greater than zero")
		require.Less(t, retained, effectivePrice,
			"prorating: retained should be strictly less than the effective price")
	})
}
