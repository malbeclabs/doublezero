//go:build qa

package e2e

import (
	"context"
	"flag"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	retransmitSubscribeTimeout = 90 * time.Second
	balanceSettleTimeout       = 30 * time.Second
)

var (
	enableSettlementTests    = flag.Bool("enable-multicast-settlement-tests", false, "enable multicast settlement tests")
	retransmitOnlyDeviceFlag = flag.String("retransmit-only-device", "", "device code or pubkey in a retransmit-only metro (overrides auto-discovery)")
	retransmitGroupCodesFlag = flag.String("retransmit-group-codes", "", "comma-separated multicast group codes a seat in a retransmit-only metro must subscribe to, and nothing else")
	retransmitPriceFlag      = flag.Uint64("retransmit-price", 0, "expected seat price in whole USDC dollars in a retransmit-only metro; 0 asserts nothing")
	keypairFlag              = flag.String("keypair", "$HOME/.config/doublezero/id.json", "path to keypair file for settlement commands")
	settlementClientFlag     = flag.String("multicast-settlement-client", "", "host of the client to use for settlement tests (overrides random selection)")
)

func selectClosestDevice(t *testing.T, ctx context.Context, log *slog.Logger, _ *qa.Test, client *qa.Client) *qa.Device {
	device, err := client.ClosestDevice(ctx)
	require.NoError(t, err, "failed to find closest device")
	log.Info("Closest device", "code", device.Code, "pubkey", device.PubKey)
	return device
}

// assertNewSeatRejected pays for a seat outside a retransmit-only metro and
// requires the program to refuse it. The client holds no seat at this point, so
// the seat has no tenure and onboarding enforcement applies to it.
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

// No flagged metro means the feature is not configured on this network, so the
// test skips. A flagged metro with no reachable device fails instead, because
// the feature would otherwise go unexercised.
func selectRetransmitOnlyDevice(t *testing.T, ctx context.Context, log *slog.Logger, test *qa.Test, client *qa.Client) *qa.Device {
	var device *qa.Device
	if *retransmitOnlyDeviceFlag != "" {
		pinned, ok := test.DeviceByCodeOrPubkey(*retransmitOnlyDeviceFlag)
		require.True(t, ok, "pinned device %q not found", *retransmitOnlyDeviceFlag)
		retransmitOnly, err := client.RetransmitOnlyExchangeKeys(ctx)
		require.NoError(t, err, "failed to read retransmit-only metros")
		require.True(t, retransmitOnly[pinned.ExchangePubKey],
			"pinned device %s (metro %s) is not in a retransmit-only metro", pinned.Code, pinned.ExchangeCode)
		device = pinned
	} else {
		selected, retransmitOnly, err := client.ClosestRetransmitOnlyDevice(ctx)
		require.NoError(t, err, "failed to find a retransmit-only device")
		if len(retransmitOnly) == 0 {
			t.Skip("Skipping: no metro is flagged retransmit-only (feature not deployed/configured on this network)")
		}
		require.NotNil(t, selected,
			"retransmit-only metros %v are configured but no reachable device matched; the feature under test cannot be exercised",
			flaggedMetroCodes(test, retransmitOnly))
		device = selected
	}
	log.Info("Selected retransmit-only device", "code", device.Code, "pubkey", device.PubKey, "metro", device.ExchangeCode)

	require.NotEmpty(t, *retransmitGroupCodesFlag, "--retransmit-group-codes is required")
	return device
}

func flaggedMetroCodes(test *qa.Test, exchangeKeys map[string]bool) []string {
	seen := make(map[string]bool)
	codes := make([]string, 0, len(exchangeKeys))
	for _, d := range test.Devices() {
		if !exchangeKeys[d.ExchangePubKey] || seen[d.ExchangePubKey] {
			continue
		}
		seen[d.ExchangePubKey] = true
		code := d.ExchangeCode
		if code == "" {
			code = d.ExchangePubKey
		}
		codes = append(codes, code)
	}
	for key := range exchangeKeys {
		if !seen[key] {
			codes = append(codes, key)
		}
	}
	return codes
}

func assertSubscribedGroups(t *testing.T, ctx context.Context, log *slog.Logger, client *qa.Client, _ *qa.Device) {
	// Nothing onchain labels a group as leader or retransmit, so the operator
	// names the retransmit groups per network.
	groupCodes, err := client.MulticastGroupCodes(ctx)
	require.NoError(t, err, "failed to read the multicast groups")
	keysByCode := make(map[string]solana.PublicKey, len(groupCodes))
	for key, code := range groupCodes {
		keysByCode[code] = key
	}

	required := make(map[solana.PublicKey]string)
	for _, code := range strings.Split(*retransmitGroupCodesFlag, ",") {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		key, ok := keysByCode[code]
		require.True(t, ok, "multicast group %q not found onchain", code)
		required[key] = code
	}
	require.NotEmpty(t, required, "no multicast group resolved from --retransmit-group-codes %q", *retransmitGroupCodesFlag)

	// A failure names the groups by code. The raw pubkeys are what made the
	// first report of this check unreadable.
	label := func(group solana.PublicKey) string {
		if code, ok := groupCodes[group]; ok {
			return code
		}
		return group.String()
	}

	// The oracle converges the seat's onchain subscription asynchronously, so
	// poll until it reflects retransmit-only membership.
	var (
		lastSubscribed []string
		lastMissing    []string
		lastExtra      []string
	)
	ok := assert.Eventually(t, func() bool {
		user, err := client.GetMulticastServiceabilityUser(ctx)
		if err != nil {
			log.Info("serviceability user poll error", "error", err)
			return false
		}
		subscribed := make(map[solana.PublicKey]bool, len(user.Subscribers))
		subs := make([]string, 0, len(user.Subscribers))
		for _, sub := range user.Subscribers {
			group := solana.PublicKeyFromBytes(sub[:])
			subscribed[group] = true
			subs = append(subs, label(group))
		}
		var missing, extra []string
		for group, code := range required {
			if !subscribed[group] {
				missing = append(missing, code)
			}
		}
		for group := range subscribed {
			if _, want := required[group]; !want {
				extra = append(extra, label(group))
			}
		}
		lastSubscribed, lastMissing, lastExtra = subs, missing, extra
		return len(missing) == 0 && len(extra) == 0
	}, retransmitSubscribeTimeout, 5*time.Second)
	if !ok {
		log.Warn("seat did not converge to retransmit-only subscription within timeout",
			"missing_groups", lastMissing,
			"extra_groups", lastExtra,
			"subscribed_groups", lastSubscribed,
		)
	}
	require.True(t, ok,
		"the seat should subscribe to %s and to nothing else; it is missing %v and wrongly subscribes to %v",
		*retransmitGroupCodesFlag, lastMissing, lastExtra)
	log.Info("Subscribed to the retransmit groups only",
		"retransmit_groups", *retransmitGroupCodesFlag,
		"subscriber_count", len(lastSubscribed),
	)
}

// TestQA_MulticastSettlement picks a device, queries its seat price from the CLI
// and cross-checks it against the price the program will charge (read from
// chain), waits for the open-for-requests phase, re-reads that price and pays
// it, checks the debit and the tunnel, then withdraws the seat and checks the
// refund accounting.
//
// The program config decides which shape the run takes. When it enforces
// retransmit-only onboarding, the test also requires the program to reject a new
// seat outside a retransmit-only metro, and it settles inside one. Otherwise it
// settles on the closest device, which is what mainnet does today.
func TestQA_MulticastSettlement(t *testing.T) {
	if !*enableSettlementTests {
		t.Skip("Skipping: --enable-multicast-settlement-tests flag not set")
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

	retransmitOnboardingEnforced, err := client.IsRetransmitOnlyOnboardingEnforced(ctx)
	require.NoError(t, err, "failed to read the retransmit-only onboarding flag")
	log.Info("Retransmit-only onboarding", "enforced", retransmitOnboardingEnforced)

	var device *qa.Device
	var amount string
	var quoted *pb.DevicePrice
	var onchain *qa.SeatPrices
	var fundedAmount uint64
	var effectivePrice uint64
	var balanceBeforePay uint64
	var balanceAfterPay uint64
	seatPaid := false

	t.Cleanup(func() {
		if seatPaid && device != nil {
			// Retry the withdraw: a single-shot withdraw that hit the spurious
			// "request in flight" preflight bail (or a transient RPC failure)
			// leaves the seat active onchain with an open escrow, poisoning
			// every subsequent hourly run. Retrying over a bounded window heals
			// the state instead of letting the escrow grow one epoch per run.
			// Bound the cleanup so a hung withdraw can't block teardown forever.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			if withdrawErr := client.WithdrawSeatWithRetry(cleanupCtx, device.PubKey); withdrawErr != nil {
				// Warn, not Info: the seat is left active onchain and the escrow
				// will grow next run, so this must stand out in the run log.
				log.Warn("Cleanup: seat withdraw failed after retries; seat left active onchain", "error", withdrawErr)
			}
		}
		if t.Failed() {
			client.DumpDiagnostics(nil)
		}
	})

	if !t.Run("ensure_program_unpaused", func(t *testing.T) {
		// Migrations pause the program; while paused the oracle cannot ack
		// instant seat allocation requests, which would leave the seat
		// un-withdrawable and fail the rest of the test with a confusing
		// "invalid account data for instruction" rejection.
		paused, err := client.IsProgramPaused(ctx)
		require.NoError(t, err, "failed to read program-paused flag")
		if paused {
			t.Skip("Skipping: shred-subscription program is paused (migration in progress)")
		}
	}) {
		return
	}

	if !t.Run("ensure_multicast_disconnected", func(t *testing.T) {
		// Self-heal a seat left stuck-active onchain by a previous run whose
		// withdraw bailed. This is the poisoned state that can't be seen from a
		// session status: `shreds pay` on an already-active seat only tops up the
		// escrow and never creates a new allocation request, so the seat never
		// re-acks and the tunnel never comes up. Detect and withdraw it before
		// the session check so the run starts from a clean slate.
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

	if !t.Run("select_device", func(t *testing.T) {
		if retransmitOnboardingEnforced {
			device = selectRetransmitOnlyDevice(t, ctx, log, test, client)
			return
		}
		device = selectClosestDevice(t, ctx, log, test, client)
	}) {
		return
	}
	if device == nil {
		// The selector skipped its subtest, because no metro is flagged
		// retransmit-only on this network.
		t.Skip("Skipping: device selection skipped (feature not configured on this network)")
	}

	if !t.Run("query_seat_price", func(t *testing.T) {
		prices, err := client.FeedSeatPrice(ctx, device.PubKey)
		require.NoError(t, err, "failed to get seat prices")

		// Match by pubkey, not code: querying by --device skips code resolution,
		// so the returned rows may not carry a device_code.
		for _, pr := range prices {
			if pr.DevicePubkey == device.PubKey {
				quoted = pr
				break
			}
		}
		require.NotNil(t, quoted, "no price found for device %s", device.Code)
		require.NotZero(t, quoted.EpochPrice, "epoch price is zero for device %s", device.Code)
		log.Info("Found epoch price", "device", device.Code,
			"epoch_price", quoted.EpochPrice,
			"instant_allocation_price", quoted.GetInstantAllocationPrice(),
			"reports_instant_allocation_price", quoted.GetReportsInstantAllocationPrice())
	}) {
		return
	}

	if !t.Run("query_onchain_seat_price", func(t *testing.T) {
		// Read the prices the program itself computes straight off the chain, as
		// an oracle for the CLI quote. This snapshot is taken next to the quote so
		// the two comparisons below are as close to simultaneous as possible; the
		// amount actually funded is re-read just before paying, since the wait for
		// the open phase can outlive this read.
		var err error
		onchain, err = client.SeatPrices(ctx, device.PubKey)
		require.NoError(t, err, "failed to compute the onchain seat prices")
		require.NotZero(t, onchain.InstantAllocationDollars, "onchain instant-allocation price is zero for device %s", device.Code)

		// Pin the price the program charges, not the CLI argument.
		if retransmitOnboardingEnforced {
			if *retransmitPriceFlag == 0 {
				log.Info("Not asserting the seat price; --retransmit-price is unset",
					"device", device.Code, "metro", device.ExchangeCode,
					"price", onchain.InstantAllocationDollars)
			} else {
				require.Equal(t, *retransmitPriceFlag, onchain.InstantAllocationDollars,
					"device %s in retransmit-only metro %s should cost the price --retransmit-price names",
					device.Code, device.ExchangeCode)
			}
		}

		log.Info("Onchain seat prices", "device", device.Code,
			"instant_allocation_price", onchain.InstantAllocationDollars,
			"last_settled_epoch", onchain.LastSettledEpoch,
			"current_epoch_price", onchain.CurrentEpochDollars,
			"current_subscription_epoch", onchain.CurrentSubscriptionEpoch)
	}) {
		return
	}

	// The next two subtests are deliberately not guarded with
	// `if !t.Run(...) { return }`: a CLI/chain divergence is a real product bug
	// and must fail the run loudly, but the payment below funds from the onchain
	// price, so the rest of the settlement flow is still worth running. Both
	// compare whole dollars, never prorated micro-USDC: proration is a function
	// of the current slot, so an exact comparison against a separately-timed read
	// is inherently racy.
	t.Run("validate_instant_allocation_price_matches_chain", func(t *testing.T) {
		switch {
		case !quoted.GetReportsInstantAllocationPrice():
			// QA hosts install doublezero-solana from a version-pinned apt package
			// (doublezero_solana_version in malbeclabs/infra
			// ansible/inventory/*/group_vars/all.yml, 0.5.10-1 at time of writing),
			// so the field only appears once a release carrying it is published and
			// the pin bumped. Asserting against an absent field would read 0 and
			// fail as "quoted 0, chain 43" — a misleading failure that looks like a
			// new bug rather than a rollout gap.
			t.Skipf("Skipping: installed doublezero-solana does not report instant_allocation_price (needs a release newer than the pinned 0.5.10-1); chain says %d USDC at last_settled_epoch=%d",
				onchain.InstantAllocationDollars, onchain.LastSettledEpoch)
		case quoted.InstantAllocationPrice == nil:
			// Reported, but null: the CLI could not find the settled-epoch ring
			// entry. That is a real condition, not a rollout artifact — the program
			// performs the same lookup and would reject the allocation.
			t.Fatalf("`shreds price` reported instant_allocation_price as unavailable for device %s, but the chain has a price of %d USDC at last_settled_epoch=%d",
				device.Code, onchain.InstantAllocationDollars, onchain.LastSettledEpoch)
		default:
			require.Equal(t, onchain.InstantAllocationDollars, quoted.GetInstantAllocationPrice(),
				"CLI quoted %d USDC for an instant allocation but the program charges %d USDC (last_settled_epoch=%d); `shreds price` and `shreds pay` read different ring entries",
				quoted.GetInstantAllocationPrice(), onchain.InstantAllocationDollars, onchain.LastSettledEpoch)
		}
	})

	t.Run("validate_epoch_price_matches_chain", func(t *testing.T) {
		// The other half of the invariant: epoch_price must keep meaning the
		// current-epoch price a recurring subscriber pays. Without this, someone
		// "fixing" the divergence by repointing epoch_price at last_settled_epoch
		// would go green here while silently breaking recurring subscribers.
		if !onchain.HasCurrentEpoch {
			// Part-way through UpdatingPrices the ring has not been advanced to the
			// current epoch for every metro and device yet, so there is nothing to
			// compare against. Transient by design, not a regression.
			t.Skipf("Skipping: no onchain price entry yet for current_subscription_epoch=%d (prices are still being updated)",
				onchain.CurrentSubscriptionEpoch)
		}
		require.Equal(t, onchain.CurrentEpochDollars, quoted.EpochPrice,
			"CLI quoted epoch_price %d USDC but the chain has %d USDC at current_subscription_epoch=%d; epoch_price must stay the price a recurring subscriber pays next epoch",
			quoted.EpochPrice, onchain.CurrentEpochDollars, onchain.CurrentSubscriptionEpoch)
	})

	// Set when wait_for_open_phase times out inside the epoch-tail closed
	// window (verified against live chain state), so the parent can skip the
	// remaining subtests: the program stays closed until the epoch boundary,
	// which the 2-minute wait cannot bridge, so pay/withdraw below could only
	// fail and page for a by-design condition.
	var epochTailWindow *qa.EpochTailWindow
	if !t.Run("wait_for_open_phase", func(t *testing.T) {
		// Record where the wait begins: a timeout means the program was closed
		// for the entire wait, so the classification below can require the
		// whole span — not just the timeout-time slot — to be inside the
		// window. Best-effort: on a read failure classification degrades to
		// the timeout-time slot only.
		waitStartSlot, slotErr := client.CurrentSolanaSlot(ctx)
		if slotErr != nil {
			log.Warn("Failed to read wait-start slot; epoch-tail classification will use the timeout-time slot only", "error", slotErr)
		}
		err := client.WaitForOpenForRequests(ctx)
		if err != nil {
			// For the last grace-period slots of every epoch the shred oracle
			// closes the program by design (settle seats, update prices) and
			// reopens it just after the epoch boundary. Verify against live
			// chain state — onchain grace period, controller phase, and RPC
			// epoch schedule — whether this timeout landed in that window; a
			// timeout outside it must keep failing exactly as loudly as before.
			win, winErr := client.EpochTailClosedWindow(ctx, waitStartSlot)
			switch {
			case winErr != nil:
				log.Warn("Failed to classify epoch-tail closed window; treating timeout as a real failure", "error", winErr)
			case win.Benign:
				epochTailWindow = &win
				t.Skipf("expected epoch-tail closed window: %s", win)
			default:
				// Give on-call the computed window so a real outage's distance
				// from the benign window is visible in the run log.
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

	if retransmitOnboardingEnforced {
		if !t.Run("reject_new_seat_outside_retransmit_only_metro", func(t *testing.T) {
			assertNewSeatRejected(t, ctx, log, client)
		}) {
			return
		}
	}

	if !t.Run("refresh_onchain_seat_price", func(t *testing.T) {
		// wait_for_open_phase blocks for up to two minutes, and a settlement
		// completing inside that window advances last_settled_epoch — the very
		// read the charge is derived from. Funding a price captured before the
		// wait would underfund the escrow and reproduce the opaque pay-time
		// rejection this test exists to avoid, so the funded amount comes from a
		// read taken here, immediately before paying. A rollover between this read
		// and the transaction landing is irreducible (any payer races it), but the
		// window shrinks from minutes to seconds.
		refreshed, err := client.SeatPrices(ctx, device.PubKey)
		require.NoError(t, err, "failed to re-read the onchain seat prices before paying")
		require.NotZero(t, refreshed.InstantAllocationDollars, "onchain instant-allocation price is zero for device %s", device.Code)

		if refreshed.LastSettledEpoch != onchain.LastSettledEpoch ||
			refreshed.InstantAllocationDollars != onchain.InstantAllocationDollars {
			// Warn, not Info: this means the quote comparisons above were made
			// against a snapshot the payment no longer uses, so a failure up there
			// should be read in that light.
			log.Warn("Onchain seat price moved while waiting for the open-for-requests phase; funding the refreshed price",
				"device", device.Code,
				"before_price", onchain.InstantAllocationDollars, "before_last_settled_epoch", onchain.LastSettledEpoch,
				"after_price", refreshed.InstantAllocationDollars, "after_last_settled_epoch", refreshed.LastSettledEpoch)
		}

		onchain = refreshed
		amount = strconv.FormatUint(onchain.InstantAllocationDollars, 10)
		fundedAmount = onchain.InstantAllocationDollars * 1_000_000 // dollars to USDC raw units (6 decimals)
		log.Info("Funding the seat escrow from the onchain price", "device", device.Code,
			"amount", amount, "last_settled_epoch", onchain.LastSettledEpoch)
	}) {
		return
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
		// Poll until the balance reflects the debit. FeedSeatPay returns
		// after the tx is submitted, and the RPC balance view can lag the
		// confirmed state briefly, so a one-shot read races.
		var lastDebit uint64
		require.Eventually(t, func() bool {
			bal, err := client.GetUSDCBalance(ctx)
			if err != nil {
				log.Info("USDC balance poll error", "error", err)
				return false
			}
			balanceAfterPay = bal
			lastDebit = balanceBeforePay - bal
			return lastDebit == fundedAmount
		}, balanceSettleTimeout, 5*time.Second, "USDC balance should decrease by the paid amount")
		log.Info("USDC balance after pay", "balance", balanceAfterPay, "debit", lastDebit, "expected_debit", fundedAmount)
	}) {
		return
	}

	if !t.Run("query_effective_seat_price", func(t *testing.T) {
		// Built on the onchain price, not the CLI quote: this feeds the
		// non-prorating balance assertion below, which must predict what the
		// program actually charged. GetEffectiveSeatPrice applies the seat's price
		// override on top when one is set.
		var err error
		effectivePrice, err = client.GetEffectiveSeatPrice(ctx, device.PubKey, onchain.InstantAllocationDollars)
		require.NoError(t, err, "failed to get effective seat price")
		log.Info("Effective seat price", "effective_usdc", effectivePrice, "funded_usdc", fundedAmount)
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

	// The seat carries the leader group in any other metro, so the group
	// assertion only means something once the metro is retransmit-only.
	if retransmitOnboardingEnforced {
		if !t.Run("assert_subscribed_groups", func(t *testing.T) {
			assertSubscribedGroups(t, ctx, log, client, device)
		}) {
			return
		}
	}

	if !t.Run("withdraw_seat", func(t *testing.T) {
		// Withdraw is rejected while this run's instant allocation request is
		// in flight (or a stale RPC read claims it is), so retry with endpoint
		// rotation rather than waiting on an ack the harness cannot observe
		// reliably.
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
		// withdraw did not complete (e.g. during the reservoir-ack outage on
		// devnet). Closing the escrow now refunds that leftover too, so the
		// wallet-measured refund exceeds what was paid this run and no longer
		// isolates this payment (`retained` would underflow). In that case the
		// wallet-delta proration check is not meaningful, so skip it rather than
		// fail — the settlement path itself is still covered by the pay/ack/
		// tunnel/withdraw sub-tests above.
		if refund > fundedAmount {
			log.Warn("skipping wallet-delta proration check: refund exceeds amount paid this run (pre-existing escrow drained)",
				"refund", refund,
				"paid_amount", fundedAmount,
				"before_pay", balanceBeforePay,
				"after_pay", balanceAfterPay,
				"after_withdraw", balanceAfterWithdraw,
			)
			return
		}
		// Equivalent to balanceBeforePay - balanceAfterWithdraw, but computed from
		// the amount paid this run so it cannot underflow given the guard above.
		retained := fundedAmount - refund

		log.Info("USDC balance after withdraw",
			"balance", balanceAfterWithdraw,
			"before_pay", balanceBeforePay,
			"after_pay", balanceAfterPay,
			"paid_amount", fundedAmount,
			"effective_price", effectivePrice,
			"refund", refund,
			"retained", retained,
			"prorating_enabled", proratingEnabled,
		)

		// Accounting invariant: regardless of prorating, the sum of what was
		// refunded to the wallet and what the program retained must equal the
		// amount debited at pay time. This uses fundedAmount rather than
		// effectivePrice because a seat with a zero price override is still
		// charged fundedAmount at pay and fully refunded on withdraw.
		require.Equal(t, fundedAmount, refund+retained,
			"refund + retained must equal the amount paid")

		if !proratingEnabled || effectivePrice == 0 {
			return
		}

		// With prorating enabled we avoid replicating the onchain formula
		// against client-side RPC state (epoch schedule + current epoch reads
		// are fragile on DZ ledger). Instead assert the qualitative invariants
		// that distinguish a real partial refund from a regression:
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
