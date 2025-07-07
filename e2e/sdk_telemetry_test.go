//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestE2E_SDK_Telemetry(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	// Use the hardcoded serviceability program keypair for this test, since the telemetry program
	// is built with it as an expectation, and the initialize instruction will fail if the owner
	// of the devices/links is not the matching serviceability program ID.
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

	la2DeviceAgentPrivateKey := solana.NewWallet().PrivateKey
	ny5DeviceAgentPrivateKey := solana.NewWallet().PrivateKey

	log.Info("==> Creating other devices and links onchain")
	dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		doublezero device create --code la2-dz01 --contributor co01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/30,200.12.12.12/29" --metrics-publisher ` + la2DeviceAgentPrivateKey.PublicKey().String() + `
		doublezero device create --code ny5-dz01 --contributor co01 --location ewr --exchange xewr --public-ip "207.45.21.134" --dz-prefixes "200.12.12.12/29" --metrics-publisher ` + ny5DeviceAgentPrivateKey.PublicKey().String() + `
		doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29"
		doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"

		doublezero link create --code "la2-dz01:ny5-dz01" --side-a la2-dz01 --side-z ny5-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
		doublezero link create --code "ny5-dz01:ld4-dz01" --side-a ny5-dz01 --side-z ld4-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
		doublezero link create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
	`})
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	log.Info("==> Waiting for devices and links to be created onchain")
	require.Eventually(t, func() bool {
		err := serviceabilityClient.Load(ctx)
		require.NoError(t, err)
		return len(serviceabilityClient.GetDevices()) == 4 && len(serviceabilityClient.GetLinks()) == 3
	}, 20*time.Second, 1*time.Second)

	links := map[string]*serviceability.Link{}
	for _, link := range serviceabilityClient.GetLinks() {
		links[link.Code] = &link
	}

	devices := map[string]*serviceability.Device{}
	for _, device := range serviceabilityClient.GetDevices() {
		devices[device.Code] = &device
	}

	la2Device, ok := devices["la2-dz01"]
	require.True(t, ok, "la2-dz01 device not found")
	la2DevicePK := solana.PublicKeyFromBytes(la2Device.PubKey[:])

	ny5Device, ok := devices["ny5-dz01"]
	require.True(t, ok, "ny5-dz01 device not found")
	ny5DevicePK := solana.PublicKeyFromBytes(ny5Device.PubKey[:])

	la2ToNy5Link, ok := links["la2-dz01:ny5-dz01"]
	require.True(t, ok, "la2-dz01:ny5-dz01 link not found")
	la2ToNy5LinkPK := solana.PublicKeyFromBytes(la2ToNy5Link.PubKey[:])

	rpcClient := dn.Ledger.GetRPCClient()
	err = airdropAndWait(ctx, rpcClient, la2DeviceAgentPrivateKey.PublicKey(), 100_000_000_000)
	require.NoError(t, err)

	la2AgentTelemetryClient, err := dn.Ledger.GetTelemetryClient(&la2DeviceAgentPrivateKey)
	require.NoError(t, err)

	epoch := uint64(100)
	samplingIntervalMicroseconds := uint64(1000000)

	// Check that the account does not exist yet.
	if !t.Run("try to get device latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Attempting to get device latency samples before initialized (should fail)")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		require.Nil(t, deviceLatencySamples)
		log.Info("==> Got expected account not found error when getting device latency samples account", "error", err, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to get device latency samples before initialized")
	}

	// Check that we get a not found error when trying to write samples before initialized.
	if !t.Run("try to write device latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Attempting to write device latency samples before initialized (should fail)")
		_, _, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      epoch,
			StartTimestampMicroseconds: uint64(time.Now().UnixMicro()),
			Samples:                    []uint32{1, 2, 3},
		})
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		log.Info("==> Got expected account not found error when writing device latency samples", "error", err, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to write device latency samples before initialized")
	}

	// Try to initialize before the account is created.
	if !t.Run("try to initialize device latency samples before account is created (should fail)", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(60*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Initializing device latency samples before account is created (should fail)")
		_, _, err := la2AgentTelemetryClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
			AgentPK:                      la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:               la2DevicePK,
			TargetDevicePK:               ny5DevicePK,
			LinkPK:                       la2ToNy5LinkPK,
			Epoch:                        epoch,
			SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
		})
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		log.Info("==> Got expected account not found error when initializing device latency samples before account is created", "error", err, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to initialize device latency samples before account is created")
	}

	// Create device latency samples account.
	if !t.Run("create device latency samples account", func(t *testing.T) {
		accountPK, _, res, err := la2AgentTelemetryClient.CreateDeviceLatencySamplesAccount(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NotNil(t, res)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.NoError(t, err)
		log.Info("==> Created device latency samples account", "account", accountPK)
	}) {
		t.Fatalf("failed to create device latency samples account")
	}

	// Try creating the account again and ensure it errors expectedly.
	if !t.Run("try to create device latency samples account again (should fail)", func(t *testing.T) {
		_, _, res, err := la2AgentTelemetryClient.CreateDeviceLatencySamplesAccount(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NotNil(t, res)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.ErrorIs(t, err, telemetry.ErrAccountAlreadyExists, "error: %+v", err)
		log.Info("==> Got expected account already initialized error when creating device latency samples account again", "error", err)
	}) {
		t.Fatalf("failed to create device latency samples account again")
	}

	// Initialize device latency samples account.
	if !t.Run("initialize device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(60*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Initializing device latency samples")
		sig, res, err := la2AgentTelemetryClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
			AgentPK:                      la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:               la2DevicePK,
			TargetDevicePK:               ny5DevicePK,
			LinkPK:                       la2ToNy5LinkPK,
			Epoch:                        epoch,
			SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Initialized device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to initialize device latency samples")
	}

	// Get device latency samples from account and verify that it's initialized and has no samples.
	if !t.Run("get device latency samples after initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting device latency samples from account")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Info("==> Got device latency samples from account", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeDeviceLatencySamples, deviceLatencySamples.AccountType)
		require.Equal(t, epoch, deviceLatencySamples.Epoch)
		require.Equal(t, la2DeviceAgentPrivateKey.PublicKey(), deviceLatencySamples.OriginDeviceAgentPK)
		require.Equal(t, la2DevicePK, deviceLatencySamples.OriginDevicePK)
		require.Equal(t, ny5DevicePK, deviceLatencySamples.TargetDevicePK)
		require.Equal(t, la2ToNy5LinkPK, deviceLatencySamples.LinkPK)
		require.Equal(t, samplingIntervalMicroseconds, deviceLatencySamples.SamplingIntervalMicroseconds)
		require.Equal(t, uint32(0), deviceLatencySamples.NextSampleIndex)
		require.Empty(t, deviceLatencySamples.Samples)
	}) {
		t.Fatalf("failed to get device latency samples after initialized")
	}

	// Write device latency samples.
	firstStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	firstSamples := []uint32{
		100000,
		200000,
		300000,
		400000,
		500000,
	}
	if !t.Run("write first device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Writing device latency samples")
		sig, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      epoch,
			StartTimestampMicroseconds: firstStartTimestampMicroseconds,
			Samples:                    firstSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Wrote device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to write first device latency samples")
	}

	// Get device latency samples from account and verify that it's updated.
	if !t.Run("get device latency samples after writing first", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting device latency samples from account")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Info("==> Got device latency samples from account", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeDeviceLatencySamples, deviceLatencySamples.AccountType)
		require.Equal(t, epoch, deviceLatencySamples.Epoch)
		require.Equal(t, la2DeviceAgentPrivateKey.PublicKey(), deviceLatencySamples.OriginDeviceAgentPK)
		require.Equal(t, la2DevicePK, deviceLatencySamples.OriginDevicePK)
		require.Equal(t, ny5DevicePK, deviceLatencySamples.TargetDevicePK)
		require.Equal(t, la2ToNy5LinkPK, deviceLatencySamples.LinkPK)
		require.Equal(t, samplingIntervalMicroseconds, deviceLatencySamples.SamplingIntervalMicroseconds)
		require.Equal(t, firstStartTimestampMicroseconds, deviceLatencySamples.StartTimestampMicroseconds)
		require.Equal(t, uint32(len(firstSamples)), deviceLatencySamples.NextSampleIndex)
		require.Equal(t, firstSamples, deviceLatencySamples.Samples)
	}) {
		t.Fatalf("failed to get device latency samples after writing first")
	}

	// Verify that initializing the same account again fails.
	if !t.Run("try to initialize device latency samples again (should fail)", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(60*time.Second))
		defer cancel()
		log.Info("==> Attempting to initialize device latency samples again (should fail)")
		_, res, err := la2AgentTelemetryClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
			AgentPK:                      la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:               la2DevicePK,
			TargetDevicePK:               ny5DevicePK,
			LinkPK:                       la2ToNy5LinkPK,
			Epoch:                        epoch,
			SamplingIntervalMicroseconds: 1000000,
		})
		require.ErrorIs(t, err, telemetry.ErrAccountAlreadyInitialized)
		require.Nil(t, res)
	}) {
		t.Fatalf("failed to initialize device latency samples again")
	}

	// Write more device latency samples.
	secondStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	secondSamples := []uint32{
		600000,
		700000,
		800000,
		900000,
		1000000,
	}
	if !t.Run("write second device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Writing more device latency samples")
		sig, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    secondSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Info("==> Wrote more device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	}) {
		t.Fatalf("failed to write second device latency samples")
	}

	// Get device latency samples from account and verify that it's updated.
	if !t.Run("get device latency samples after writing second", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Info("==> Getting device latency samples from account")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DeviceAgentPrivateKey.PublicKey(), la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Info("==> Got device latency samples from account", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeDeviceLatencySamples, deviceLatencySamples.AccountType)
		require.Equal(t, epoch, deviceLatencySamples.Epoch)
		require.Equal(t, la2DeviceAgentPrivateKey.PublicKey(), deviceLatencySamples.OriginDeviceAgentPK)
		require.Equal(t, la2DevicePK, deviceLatencySamples.OriginDevicePK)
		require.Equal(t, ny5DevicePK, deviceLatencySamples.TargetDevicePK)
		require.Equal(t, la2ToNy5LinkPK, deviceLatencySamples.LinkPK)
		require.Equal(t, samplingIntervalMicroseconds, deviceLatencySamples.SamplingIntervalMicroseconds)  // Remains unchanged.
		require.Equal(t, firstStartTimestampMicroseconds, deviceLatencySamples.StartTimestampMicroseconds) // Remains unchanged.
		combinedSamples := append(firstSamples, secondSamples...)
		require.Equal(t, uint32(len(combinedSamples)), deviceLatencySamples.NextSampleIndex)
		require.Equal(t, combinedSamples, deviceLatencySamples.Samples)
	}) {
		t.Fatalf("failed to get device latency samples after writing second")
	}
}

func airdropAndWait(ctx context.Context, client *solanarpc.Client, pubkey solana.PublicKey, lamports uint64) error {
	sig, err := client.RequestAirdrop(ctx, pubkey, lamports, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("airdrop request failed: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Wait for confirmation.
	for {
		resp, err := client.GetSignatureStatuses(waitCtx, true, sig)
		if err != nil {
			return fmt.Errorf("get signature status failed: %w", err)
		}
		status := resp.Value[0]
		if status != nil && (status.ConfirmationStatus == solanarpc.ConfirmationStatusConfirmed || status.ConfirmationStatus == solanarpc.ConfirmationStatusFinalized) {
			break
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for airdrop confirmation")
		case <-time.After(500 * time.Millisecond):
		}
	}

	// Wait for balance to appear.
	for {
		balanceResp, err := client.GetBalance(waitCtx, pubkey, solanarpc.CommitmentConfirmed)
		if err == nil && balanceResp.Value > 0 {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for airdrop balance")
		case <-time.After(500 * time.Millisecond):
		}
	}
}
