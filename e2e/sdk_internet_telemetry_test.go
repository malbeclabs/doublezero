//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestE2E_SDK_Telemetry_InternetLatencySamples(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t).With("test", t.Name(), "deployID", deployID)

	// Use the hardcoded serviceability program keypair for this test, since the telemetry program
	// is built with it as an expectation, and the initialize instruction will fail if the owner
	// of the exchanges is not the matching serviceability program ID.
	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create an oracle agent keypair.
	oracleAgentPK := solana.NewWallet().PrivateKey

	// Wait for exchanges to be created onchain.
	log.Info("==> Waiting for exchanges to be created onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		return len(data.Exchanges) == 8
	}, 20*time.Second, 1*time.Second)

	// Get exchanges from onchain.
	serviceabilityData, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)
	exchanges := map[string]*serviceability.Exchange{}
	for _, exchange := range serviceabilityData.Exchanges {
		exchanges[exchange.Code] = &exchange
	}

	// Get LAX exchange PK.
	laxExchange, ok := exchanges["xlax"]
	require.True(t, ok, "xlax exchange not found")
	laxExchangePK := solana.PublicKeyFromBytes(laxExchange.PubKey[:])

	// Get AMS exchange PK.
	amsExchange, ok := exchanges["xams"]
	require.True(t, ok, "xams exchange not found")
	amsExchangePK := solana.PublicKeyFromBytes(amsExchange.PubKey[:])

	// Get ledger RPC client.
	rpcClient := dn.Ledger.GetRPCClient()
	err = airdropAndWait(ctx, rpcClient, oracleAgentPK.PublicKey(), 100_000_000_000)
	require.NoError(t, err)

	telemetryClient, err := dn.Ledger.GetTelemetryClient(&oracleAgentPK)
	require.NoError(t, err)

	epoch := uint64(100)
	samplingIntervalMicroseconds := uint64(1000000)
	dataProvider1Name := "test-data-provider-1"

	// Check that the account does not exist yet.
	t.Run("try to get internet latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Attempting to get internet latency samples before initialized (should fail)")
		internetLatencySamples, err := telemetryClient.GetInternetLatencySamples(ctx, dataProvider1Name, laxExchangePK, laxExchangePK, oracleAgentPK.PublicKey(), epoch)
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		require.Nil(t, internetLatencySamples)
		log.Info("==> Got expected account not found error when getting internet latency samples PDA", "error", err, "duration", time.Since(start))
	})

	// Check that we get a not found error when trying to write samples before initialized.
	t.Run("try to write internet latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Attempting to write internet latency samples before initialized (should fail)")
		_, res, err := telemetryClient.WriteInternetLatencySamples(ctx, telemetry.WriteInternetLatencySamplesInstructionConfig{
			OriginExchangePK:           laxExchangePK,
			TargetExchangePK:           amsExchangePK,
			DataProviderName:           dataProvider1Name,
			Epoch:                      epoch,
			StartTimestampMicroseconds: uint64(time.Now().UnixMicro()),
			Samples:                    []uint32{1, 2, 3},
		})
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		require.Nil(t, res)
		log.Info("==> Got expected account not found error when writing internet latency samples", "error", err, "duration", time.Since(start))
	})

	// Initialize internet latency samples account.
	t.Run("initialize internet latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Initializing internet latency samples")
		sig, res, err := telemetryClient.InitializeInternetLatencySamples(ctx, telemetry.InitializeInternetLatencySamplesInstructionConfig{
			OriginExchangePK:             laxExchangePK,
			TargetExchangePK:             amsExchangePK,
			DataProviderName:             dataProvider1Name,
			Epoch:                        epoch,
			SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Initialized internet latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get internet latency samples from PDA and verify that it's initialized and has no samples.
	t.Run("get internet latency samples after initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting internet latency samples from PDA")
		account, err := telemetryClient.GetInternetLatencySamples(ctx, dataProvider1Name, laxExchangePK, amsExchangePK, oracleAgentPK.PublicKey(), epoch)
		require.NoError(t, err)
		log.Info("==> Got internet latency samples from PDA", "internetLatencySamples", account, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeInternetLatencySamples, account.AccountType)
		require.Equal(t, dataProvider1Name, account.DataProviderName)
		require.Equal(t, epoch, account.Epoch)
		require.Equal(t, oracleAgentPK.PublicKey(), account.OracleAgentPK)
		require.Equal(t, laxExchangePK, account.OriginExchangePK)
		require.Equal(t, amsExchangePK, account.TargetExchangePK)
		require.Equal(t, samplingIntervalMicroseconds, account.SamplingIntervalMicroseconds)
		require.Equal(t, uint32(0), account.NextSampleIndex)
		require.Empty(t, account.Samples)
	})

	// Write internet latency samples.
	firstStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	firstSamples := []uint32{
		100000,
		200000,
		300000,
		400000,
		500000,
	}

	t.Run("write first internet latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Writing internet latency samples")
		sig, res, err := telemetryClient.WriteInternetLatencySamples(ctx, telemetry.WriteInternetLatencySamplesInstructionConfig{
			OriginExchangePK:           laxExchangePK,
			TargetExchangePK:           amsExchangePK,
			DataProviderName:           dataProvider1Name,
			Epoch:                      epoch,
			StartTimestampMicroseconds: firstStartTimestampMicroseconds,
			Samples:                    firstSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Wrote internet latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get internet latency samples from PDA and verify that it's updated.
	t.Run("get internet latency samples after writing first", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting internet latency samples from PDA")
		account, err := telemetryClient.GetInternetLatencySamples(ctx, dataProvider1Name, laxExchangePK, amsExchangePK, oracleAgentPK.PublicKey(), epoch)
		require.NoError(t, err)
		log.Info("==> Got internet latency samples from PDA", "internetLatencySamples", account, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeInternetLatencySamples, account.AccountType)
		require.Equal(t, epoch, account.Epoch)
		require.Equal(t, oracleAgentPK.PublicKey(), account.OracleAgentPK)
		require.Equal(t, laxExchangePK, account.OriginExchangePK)
		require.Equal(t, amsExchangePK, account.TargetExchangePK)
		require.Equal(t, samplingIntervalMicroseconds, account.SamplingIntervalMicroseconds)
		require.Equal(t, firstStartTimestampMicroseconds, account.StartTimestampMicroseconds)
		require.Equal(t, uint32(len(firstSamples)), account.NextSampleIndex)
		require.Equal(t, firstSamples, account.Samples)
	})

	// Verify that initializing the same account again fails.
	t.Run("try to initialize internet latency samples again", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		log.Info("==> Attempting to initialize internet latency samples again (should fail)")
		_, res, err := telemetryClient.InitializeInternetLatencySamples(ctx, telemetry.InitializeInternetLatencySamplesInstructionConfig{
			OriginExchangePK:             laxExchangePK,
			TargetExchangePK:             amsExchangePK,
			DataProviderName:             dataProvider1Name,
			Epoch:                        epoch,
			SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		log.Debug("transaction error", "error", res.Meta.Err)
		require.NotNil(t, res.Meta.Err, "transaction should fail")
	})

	// Write more internet latency samples.
	secondStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	secondSamples := []uint32{
		600000,
		700000,
		800000,
		900000,
		1000000,
	}

	t.Run("write second internet latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Writing more internet latency samples")
		sig, res, err := telemetryClient.WriteInternetLatencySamples(ctx, telemetry.WriteInternetLatencySamplesInstructionConfig{
			OriginExchangePK:           laxExchangePK,
			TargetExchangePK:           amsExchangePK,
			DataProviderName:           dataProvider1Name,
			Epoch:                      epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    secondSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Wrote more internet latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get internet latency samples from PDA and verify that it's updated.
	t.Run("get internet latency samples after writing second", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting internet latency samples from PDA")
		internetLatencySamples, err := telemetryClient.GetInternetLatencySamples(ctx, dataProvider1Name, laxExchangePK, amsExchangePK, oracleAgentPK.PublicKey(), epoch)
		require.NoError(t, err)
		log.Info("==> Got internet latency samples from PDA", "internetLatencySamples", internetLatencySamples, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeInternetLatencySamples, internetLatencySamples.AccountType)
		require.Equal(t, epoch, internetLatencySamples.Epoch)
		require.Equal(t, oracleAgentPK.PublicKey(), internetLatencySamples.OracleAgentPK)
		require.Equal(t, laxExchangePK, internetLatencySamples.OriginExchangePK)
		require.Equal(t, amsExchangePK, internetLatencySamples.TargetExchangePK)
		require.Equal(t, samplingIntervalMicroseconds, internetLatencySamples.SamplingIntervalMicroseconds)  // Remains unchanged.
		require.Equal(t, firstStartTimestampMicroseconds, internetLatencySamples.StartTimestampMicroseconds) // Remains unchanged.
		combinedSamples := append(firstSamples, secondSamples...)
		require.Equal(t, uint32(len(combinedSamples)), internetLatencySamples.NextSampleIndex)
		require.Equal(t, combinedSamples, internetLatencySamples.Samples)
	})

	t.Run("write largest possible batch of samples per transaction", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Writing largest possible batch of samples per transaction")
		sig, res, err := telemetryClient.WriteInternetLatencySamples(ctx, telemetry.WriteInternetLatencySamplesInstructionConfig{
			OriginExchangePK:           laxExchangePK,
			TargetExchangePK:           amsExchangePK,
			DataProviderName:           dataProvider1Name,
			Epoch:                      epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    make([]uint32, telemetry.MaxSamplesPerBatch),
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Wrote largest possible batch of samples per transaction", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	t.Run("write largest possible batch of samples per transaction +1 (should fail)", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		log.Info("==> Writing largest possible batch of samples per transaction +1 (should fail)")
		_, _, err := telemetryClient.WriteInternetLatencySamples(ctx, telemetry.WriteInternetLatencySamplesInstructionConfig{
			OriginExchangePK:           laxExchangePK,
			TargetExchangePK:           amsExchangePK,
			DataProviderName:           dataProvider1Name,
			Epoch:                      epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    make([]uint32, telemetry.MaxSamplesPerBatch+1),
		})
		require.ErrorIs(t, err, telemetry.ErrSamplesBatchTooLarge)
	})
}
