//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/prometheus"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_Funder(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)

	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	extraRecipientPK := solana.NewWallet().PublicKey().String()

	minBalanceSOL := 3.0
	topUpSOL := 5.0
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:       true,
			MinBalanceSOL: minBalanceSOL,
			TopUpSOL:      topUpSOL,
			Interval:      3 * time.Second,
			ExtraRecipients: map[string]string{
				"extra-recipient": extraRecipientPK,
			},
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	rpcClient := dn.Ledger.GetRPCClient()
	funderPrivateKey, err := dn.Funder.PrivateKey()
	require.NoError(t, err)
	funderPK := funderPrivateKey.PublicKey()

	// Check that the errors metric only contains expected startup errors:
	// - "get_recipients": occurs when the serviceability program isn't ready yet
	// - "funder_account_balance_below_minimum": occurs while waiting for the manager/funder account to be funded
	metricsClient := dn.Funder.GetMetricsClient()
	require.NoError(t, metricsClient.WaitForReady(ctx, 3*time.Second))
	require.NoError(t, metricsClient.Fetch(ctx))
	errors := metricsClient.GetCounterValues("doublezero_funder_errors_total")
	require.NotNil(t, errors)
	allowedStartupErrors := map[string]bool{
		"get_recipients":                       true,
		"funder_account_balance_below_minimum": true,
	}
	for _, e := range errors {
		require.True(t, allowedStartupErrors[e.Labels["error_type"]], "unexpected error type during startup: %s", e.Labels["error_type"])
	}

	// Check the funder account balance metric. Wait for it to appear since the funder
	// only sets this gauge after successfully getting recipients, which may take a few ticks.
	var funderBalance []prometheus.LabeledValue
	require.Eventually(t, func() bool {
		if err := metricsClient.Fetch(ctx); err != nil {
			return false
		}
		funderBalance = metricsClient.GetGaugeValues("doublezero_funder_account_balance_sol")
		return funderBalance != nil
	}, 30*time.Second, 3*time.Second)
	// The funder account is the manager account, which we fund with 100 SOL during devnet setup.
	require.Greater(t, funderBalance[0].Value, 50.0)
	prevFunderBalance := funderBalance[0].Value

	// Check that the extra recipient is funded.
	requireEventuallyFunded(t, log, rpcClient, solana.MustPublicKeyFromBase58(extraRecipientPK), minBalanceSOL, "extra recipient")

	// Add a device onchain with metrics publisher pubkey.
	log.Debug("==> Creating LA device onchain")
	laDeviceMetricsPublisherWallet := solana.NewWallet()
	laDevicePK, err := dn.GetOrCreateDeviceOnchain(ctx, "la2-dz01", "lax", "xlax", laDeviceMetricsPublisherWallet.PublicKey().String(), "207.45.216.134", []string{"207.45.216.136/30"}, "default")
	require.NoError(t, err)
	log.Debug("--> LA device created", "pubkey", laDevicePK, "metricsPublisher", laDeviceMetricsPublisherWallet.PublicKey())

	// Check that the balance starts at 0.
	require.Equal(t, 0.0, getBalance(t, rpcClient, laDeviceMetricsPublisherWallet.PublicKey()))

	// Check that the metrics publisher pubkey is eventually funded.
	requireEventuallyFunded(t, log, rpcClient, laDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "LA device metrics publisher")

	// Add another device onchain with metrics publisher pubkey.
	nyDeviceMetricsPublisherWallet := solana.NewWallet()
	nyDevicePK, err := dn.GetOrCreateDeviceOnchain(ctx, "ny-dz01", "ewr", "xewr", nyDeviceMetricsPublisherWallet.PublicKey().String(), "207.45.217.134", []string{"207.45.217.136/30"}, "default")
	require.NoError(t, err)
	log.Debug("--> NY device created", "pubkey", nyDevicePK, "metricsPublisher", nyDeviceMetricsPublisherWallet.PublicKey())

	// Check that the balance is now 0 SOL.
	require.Equal(t, 0.0, getBalance(t, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey()))

	// Check that the metrics publisher pubkey is eventually funded.
	requireEventuallyFunded(t, log, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "NY device metrics publisher")

	// Check that the funder account balance is now lower.
	require.NoError(t, metricsClient.Fetch(ctx))
	funderBalance = metricsClient.GetGaugeValues("doublezero_funder_account_balance_sol")
	require.NotNil(t, funderBalance)
	require.Less(t, funderBalance[0].Value, prevFunderBalance)

	// Drain current balance from the devices onchain.
	drainWallet := solana.NewWallet()
	log.Debug("--> Draining LA device balance", "account", laDeviceMetricsPublisherWallet.PublicKey())
	drainFunds(t, rpcClient, laDeviceMetricsPublisherWallet.PrivateKey, drainWallet.PublicKey(), 0.01)
	log.Debug("--> Draining NY device balance", "account", nyDeviceMetricsPublisherWallet.PublicKey())
	drainFunds(t, rpcClient, nyDeviceMetricsPublisherWallet.PrivateKey, drainWallet.PublicKey(), 0.01)

	// Capture funder balance before devices are refunded.
	beforeFunderBalance := getBalance(t, rpcClient, funderPK)

	// Check that the devices are eventually funded again.
	requireEventuallyFunded(t, log, rpcClient, laDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "LA device metrics publisher")
	requireEventuallyFunded(t, log, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "NY device metrics publisher")

	// Wait for the funder account balance to show the top up.
	// Note: We check for at least 1*topUpSOL decrease (not 2x) because the funder may have
	// already started funding before we captured beforeFunderBalance.
	require.Eventually(t, func() bool {
		funderBalance := getBalance(t, rpcClient, funderPK)
		return funderBalance <= beforeFunderBalance-topUpSOL
	}, 90*time.Second, 5*time.Second)

	// Capture current error count before draining funder.
	require.NoError(t, metricsClient.Fetch(ctx))
	errors = metricsClient.GetCounterValues("doublezero_funder_errors_total")
	var preDrainErrorCount int
	for _, e := range errors {
		if e.Labels["error_type"] == "funder_account_balance_below_minimum" {
			preDrainErrorCount = int(e.Value)
			break
		}
	}

	// Drain the funder account balance to near 0.
	log.Debug("--> Draining funder account balance", "account", funderPK)
	drainFunds(t, rpcClient, funderPrivateKey, drainWallet.PublicKey(), 0.01)

	// Also drain a recipient so the funder will try to fund it and fail.
	// Without this, the funder has no work to do and won't report the error.
	log.Debug("--> Draining LA device balance to trigger funder attempt", "account", laDeviceMetricsPublisherWallet.PublicKey())
	drainFunds(t, rpcClient, laDeviceMetricsPublisherWallet.PrivateKey, drainWallet.PublicKey(), 0.001)

	// Check that the errors metric for "funder_account_balance_below_minimum" eventually increases,
	// which occurs when the funder account balance is drained to below the minimum.
	require.Eventually(t, func() bool {
		if err := metricsClient.Fetch(ctx); err != nil {
			log.Debug("--> Failed to fetch metrics, retrying", "error", err)
			return false
		}
		errors = metricsClient.GetCounterValues("doublezero_funder_errors_total")
		if errors == nil {
			log.Debug("--> Waiting for funder errors metric to appear", "account", funderPK)
			return false
		}
		for _, e := range errors {
			if e.Labels["error_type"] == "funder_account_balance_below_minimum" {
				if int(e.Value) > preDrainErrorCount {
					return true
				}
				log.Debug("--> Waiting for funder account balance below minimum error to increase", "account", funderPK, "prevCount", preDrainErrorCount, "currentCount", int(e.Value))
				return false
			}
		}
		log.Debug("--> Waiting for funder account balance below minimum error to appear", "account", funderPK)
		return false
	}, 90*time.Second, 5*time.Second)

	// Check that the funder account balance gauge metric is now near 0.
	require.NoError(t, metricsClient.Fetch(ctx))
	funderBalance = metricsClient.GetGaugeValues("doublezero_funder_account_balance_sol")
	require.NotNil(t, funderBalance)
	require.LessOrEqual(t, funderBalance[0].Value, 0.01)

	// Transfer the drained funds back to the funder account.
	expectedFunderBalance := drainFunds(t, rpcClient, drainWallet.PrivateKey, funderPrivateKey.PublicKey(), 0.01)

	// Check that the funder account balance is eventually back near the previous value.
	require.Eventually(t, func() bool {
		if err := metricsClient.Fetch(ctx); err != nil {
			log.Debug("--> Failed to fetch metrics, retrying", "error", err)
			return false
		}
		funderBalance = metricsClient.GetGaugeValues("doublezero_funder_account_balance_sol")
		if funderBalance == nil {
			return false
		}
		if funderBalance[0].Value > expectedFunderBalance-0.01 && funderBalance[0].Value < expectedFunderBalance+0.01 {
			return true
		}
		log.Debug("--> Waiting for funder account balance to be back near previous value", "account", funderPK, "expectedBalance", expectedFunderBalance, "currentBalance", funderBalance[0].Value)
		return false
	}, 90*time.Second, 5*time.Second)
}

func drainFunds(t *testing.T, client *solanarpc.Client, from solana.PrivateKey, to solana.PublicKey, remainingBalanceSOL float64) float64 {
	t.Helper()

	balanceSOL := getBalance(t, client, from.PublicKey())
	transferFunds(t, client, from, to, balanceSOL-remainingBalanceSOL, nil)

	return balanceSOL - remainingBalanceSOL
}

func requireEventuallyFunded(t *testing.T, log *slog.Logger, client *solanarpc.Client, account solana.PublicKey, minBalanceSOL float64, name string) {
	t.Helper()

	require.Eventually(t, func() bool {
		balance, err := client.GetBalance(t.Context(), account, solanarpc.CommitmentFinalized)
		if err != nil {
			log.Debug(fmt.Sprintf("--> Failed to get balance for %s, retrying", name), "account", account, "error", err)
			return false
		}
		balanceSOL := float64(balance.Value) / float64(solana.LAMPORTS_PER_SOL)
		if balanceSOL < minBalanceSOL {
			log.Debug(fmt.Sprintf("--> Waiting for %s to be funded", name), "account", account, "minBalance", minBalanceSOL, "balance", balanceSOL)
			return false
		}
		return true
	}, 90*time.Second, 5*time.Second)
}

func getBalance(t *testing.T, client *solanarpc.Client, account solana.PublicKey) float64 {
	t.Helper()

	balance, err := client.GetBalance(t.Context(), account, solanarpc.CommitmentFinalized)
	require.NoError(t, err)
	return float64(balance.Value) / float64(solana.LAMPORTS_PER_SOL)
}

func transferFunds(
	t *testing.T,
	client *solanarpc.Client,
	sender solana.PrivateKey,
	recipient solana.PublicKey,
	solAmount float64,
	opts *solanarpc.TransactionOpts,
) {
	t.Helper()

	if opts == nil {
		opts = &solanarpc.TransactionOpts{
			SkipPreflight:       true,
			MaxRetries:          nil,
			PreflightCommitment: solanarpc.CommitmentFinalized,
		}
	}

	recentBlockhash, err := client.GetLatestBlockhash(t.Context(), solanarpc.CommitmentFinalized)
	require.NoError(t, err)

	ix := system.NewTransferInstruction(uint64(solAmount*float64(solana.LAMPORTS_PER_SOL)), sender.PublicKey(), recipient).Build()

	tx, err := solana.NewTransaction(
		[]solana.Instruction{ix},
		recentBlockhash.Value.Blockhash,
		solana.TransactionPayer(sender.PublicKey()),
	)
	require.NoError(t, err)

	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if key.Equals(sender.PublicKey()) {
				return &sender
			}
			return nil
		},
	)
	require.NoError(t, err)

	_, err = client.SendTransactionWithOpts(
		t.Context(),
		tx,
		*opts,
	)
	require.NoError(t, err)
}
