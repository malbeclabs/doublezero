//go:build e2e

package e2e_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_Funder(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	currentDir, err := os.Getwd()
	require.NoError(t, err)

	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

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

	// Add a device onchain with metrics publisher pubkey.
	log.Debug("==> Creating LA device onchain")
	laDeviceMetricsPublisherWallet := solana.NewWallet()
	laDevicePK, err := dn.GetOrCreateDeviceOnchain(ctx, "la2-dz01", "lax", "xlax", laDeviceMetricsPublisherWallet.PublicKey().String(), "207.45.216.134", []string{"207.45.216.136/30"})
	require.NoError(t, err)
	log.Info("--> LA device created", "pubkey", laDevicePK, "metricsPublisher", laDeviceMetricsPublisherWallet.PublicKey())

	// Check that the balance starts at 0.
	require.Equal(t, 0.0, getBalance(t, rpcClient, laDeviceMetricsPublisherWallet.PublicKey()))

	// Check that the metrics publisher pubkey is eventually funded.
	requireEventuallyFunded(t, log, rpcClient, laDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "LA device metrics publisher")

	// Add another device onchain with metrics publisher pubkey.
	nyDeviceMetricsPublisherWallet := solana.NewWallet()
	nyDevicePK, err := dn.GetOrCreateDeviceOnchain(ctx, "ny-dz01", "ewr", "xewr", nyDeviceMetricsPublisherWallet.PublicKey().String(), "207.45.217.134", []string{"207.45.217.136/30"})
	require.NoError(t, err)
	log.Info("--> NY device created", "pubkey", nyDevicePK, "metricsPublisher", nyDeviceMetricsPublisherWallet.PublicKey())

	// Check that the balance is now 0 SOL.
	require.Equal(t, 0.0, getBalance(t, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey()))

	// Check that the metrics publisher pubkey is eventually funded.
	requireEventuallyFunded(t, log, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "NY device metrics publisher")

	// Drain current balance from the devices onchain.
	drainFunds(t, rpcClient, laDeviceMetricsPublisherWallet.PrivateKey, funderPK, 0.01)
	drainFunds(t, rpcClient, nyDeviceMetricsPublisherWallet.PrivateKey, funderPK, 0.01)

	// Check that the devices are eventually funded again.
	requireEventuallyFunded(t, log, rpcClient, laDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "LA device metrics publisher")
	requireEventuallyFunded(t, log, rpcClient, nyDeviceMetricsPublisherWallet.PublicKey(), minBalanceSOL, "NY device metrics publisher")
}

func drainFunds(t *testing.T, client *solanarpc.Client, from solana.PrivateKey, to solana.PublicKey, amount float64) {
	t.Helper()

	balanceSOL := getBalance(t, client, from.PublicKey())
	transferFunds(t, client, from, to, balanceSOL-amount, nil)
}

func requireEventuallyFunded(t *testing.T, log *slog.Logger, client *solanarpc.Client, account solana.PublicKey, minBalanceSOL float64, name string) {
	t.Helper()

	require.Eventually(t, func() bool {
		balance, err := client.GetBalance(t.Context(), account, solanarpc.CommitmentFinalized)
		require.NoError(t, err)
		balanceSOL := float64(balance.Value) / float64(solana.LAMPORTS_PER_SOL)
		if balanceSOL < minBalanceSOL {
			log.Debug("--> Waiting for %s to be funded", "name", name)
			return false
		}
		return true
	}, 60*time.Second, 5*time.Second)
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
