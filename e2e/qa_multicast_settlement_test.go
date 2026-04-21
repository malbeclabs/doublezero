//go:build qa

package e2e

import (
	"context"
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/stretchr/testify/require"
)

// balanceSettleTimeout bounds how long we wait for a USDC balance change to
// become visible after a settlement transaction is submitted. 30s covers
// the lag between FeedSeatPay/FeedSeatWithdraw returning and the balance
// RPC reflecting the debit/credit.
const balanceSettleTimeout = 30 * time.Second

var (
	enableSettlementTests = flag.Bool("enable-multicast-settlement-tests", false, "enable multicast settlement tests")
	keypairFlag           = flag.String("keypair", "$HOME/.config/doublezero/id.json", "path to keypair file for settlement commands")
	settlementClientFlag  = flag.String("multicast-settlement-client", "", "host of the client to use for settlement tests (overrides random selection)")
)

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
			cleanupCtx := context.Background()
			if withdrawErr := client.FeedSeatWithdraw(cleanupCtx, device.PubKey); withdrawErr != nil {
				log.Info("Cleanup: seat withdraw failed (may already be withdrawn)", "error", withdrawErr)
			}
		}
		if t.Failed() {
			client.DumpDiagnostics(nil)
		}
	})

	if !t.Run("ensure_multicast_disconnected", func(t *testing.T) {
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
		err = client.FeedSeatWithdraw(ctx, dev.PubKey)
		require.NoError(t, err, "failed to withdraw existing seat")
		err = client.WaitForStatusDisconnected(ctx)
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

	if !t.Run("find_closest_device", func(t *testing.T) {
		var err error
		device, err = client.ClosestDevice(ctx)
		require.NoError(t, err, "failed to find closest device")
		log.Info("Closest device", "code", device.Code, "pubkey", device.PubKey)
	}) {
		return
	}

	if !t.Run("query_seat_price", func(t *testing.T) {
		prices, err := client.FeedSeatPrice(ctx)
		require.NoError(t, err, "failed to get seat prices")

		var price *pb.DevicePrice
		for _, p := range prices {
			if p.DeviceCode == device.Code {
				price = p
				break
			}
		}
		require.NotNil(t, price, "no price found for device %s", device.Code)
		require.NotZero(t, price.EpochPrice, "epoch price is zero for device %s", device.Code)
		epochPrice = price.EpochPrice
		amount = strconv.FormatUint(epochPrice, 10)
		parsedAmount = epochPrice * 1_000_000 // convert dollars to USDC raw units (6 decimals)
		log.Info("Found epoch price", "device", device.Code, "amount", amount)
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

	if !t.Run("withdraw_seat", func(t *testing.T) {
		err := client.FeedSeatWithdraw(ctx, device.PubKey)
		require.NoError(t, err, "failed to withdraw seat")
		seatPaid = false
	}) {
		return
	}

	if !t.Run("validate_tunnel_down", func(t *testing.T) {
		err := client.WaitForStatusDisconnected(ctx)
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
		retained := balanceBeforePay - balanceAfterWithdraw

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
