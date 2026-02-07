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
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestE2E_SDK_Telemetry_DeviceLatencySamples(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t).With("test", t.Name(), "deployID", deployID)

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

	log.Debug("==> Creating other devices and links onchain")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		doublezero device create --code la2-dz01 --contributor co01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/30,200.12.12.12/29" --metrics-publisher ` + la2DeviceAgentPrivateKey.PublicKey().String() + ` --mgmt-vrf mgmt
		doublezero device create --code ny5-dz01 --contributor co01 --location ewr --exchange xewr --public-ip "207.45.21.134" --dz-prefixes "200.12.12.12/29" --metrics-publisher ` + ny5DeviceAgentPrivateKey.PublicKey().String() + ` --mgmt-vrf mgmt
		doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.80/29" --mgmt-vrf mgmt
		doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.96/29" --mgmt-vrf mgmt

		doublezero device update --pubkey ld4-dz01 --max-users 128 --desired-status activated
		doublezero device update --pubkey ny5-dz01 --max-users 128 --desired-status activated
		doublezero device update --pubkey ld4-dz01 --max-users 128 --desired-status activated
		doublezero device update --pubkey frk-dz01 --max-users 128 --desired-status activated

		doublezero device interface create la2-dz01 "Switch1/1/1"
		doublezero device interface create ny5-dz01 "Switch1/1/1"
		doublezero device interface create ny5-dz01 "Switch1/1/2"
		doublezero device interface create ld4-dz01 "Switch1/1/1"
		doublezero device interface create ld4-dz01 "Switch1/1/2"
		doublezero device interface create frk-dz01 "Switch1/1/1"

		doublezero device interface create la2-dz01 "Loopback255" --loopback-type vpnv4
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4
		doublezero device interface create ld4-dz01 "Loopback255" --loopback-type vpnv4
		doublezero device interface create frk-dz01 "Loopback255" --loopback-type vpnv4

		doublezero device interface create la2-dz01 "Loopback256" --loopback-type ipv4
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4
		doublezero device interface create ld4-dz01 "Loopback256" --loopback-type ipv4
		doublezero device interface create frk-dz01 "Loopback256" --loopback-type ipv4

		doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Switch1/1/1 --side-z ny5-dz01 --side-z-interface Switch1/1/1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
		doublezero link update --pubkey "la2-dz01:ny5-dz01" --desired-status activated
		doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Switch1/1/2 --side-z ld4-dz01 --side-z-interface Switch1/1/1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
		doublezero link update --pubkey "ny5-dz01:ld4-dz01" --desired-status activated
		doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Switch1/1/2 --side-z frk-dz01 --side-z-interface Switch1/1/1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
		doublezero link update --pubkey "ld4-dz01:frk-dz01" --desired-status activated
	`})
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	log.Debug("==> Waiting for devices and links to be created onchain")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		return len(data.Devices) == 4 && len(data.Links) == 3
	}, 20*time.Second, 1*time.Second)

	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)

	links := map[string]*serviceability.Link{}
	for _, link := range data.Links {
		links[link.Code] = &link
	}

	devices := map[string]*serviceability.Device{}
	for _, device := range data.Devices {
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
	t.Run("try to get device latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Attempting to get device latency samples before initialized (should fail)")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		require.Nil(t, deviceLatencySamples)
		log.Debug("==> Got expected account not found error when getting device latency samples PDA", "error", err, "duration", time.Since(start))
	})

	// Check that we get a not found error when trying to write samples before initialized.
	t.Run("try to write device latency samples before initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Attempting to write device latency samples before initialized (should fail)")
		_, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      &epoch,
			StartTimestampMicroseconds: uint64(time.Now().UnixMicro()),
			Samples:                    []uint32{1, 2, 3},
		})
		require.ErrorIs(t, err, telemetry.ErrAccountNotFound)
		require.Nil(t, res)
		log.Debug("==> Got expected account not found error when writing device latency samples", "error", err, "duration", time.Since(start))
	})

	// Initialize device latency samples account.
	t.Run("initialize device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Initializing device latency samples")
		sig, res, err := la2AgentTelemetryClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
			AgentPK:                      la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:               la2DevicePK,
			TargetDevicePK:               ny5DevicePK,
			LinkPK:                       la2ToNy5LinkPK,
			Epoch:                        &epoch,
			SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Debug("==> Initialized device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get device latency samples from PDA and verify that it's initialized and has no samples.
	t.Run("get device latency samples after initialized", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Getting device latency samples from PDA")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Debug("==> Got device latency samples from PDA", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
		require.Equal(t, telemetry.AccountTypeDeviceLatencySamples, deviceLatencySamples.AccountType)
		require.Equal(t, epoch, deviceLatencySamples.Epoch)
		require.Equal(t, la2DeviceAgentPrivateKey.PublicKey(), deviceLatencySamples.OriginDeviceAgentPK)
		require.Equal(t, la2DevicePK, deviceLatencySamples.OriginDevicePK)
		require.Equal(t, ny5DevicePK, deviceLatencySamples.TargetDevicePK)
		require.Equal(t, la2ToNy5LinkPK, deviceLatencySamples.LinkPK)
		require.Equal(t, samplingIntervalMicroseconds, deviceLatencySamples.SamplingIntervalMicroseconds)
		require.Equal(t, uint32(0), deviceLatencySamples.NextSampleIndex)
		require.Empty(t, deviceLatencySamples.Samples)
	})

	// Write device latency samples.
	firstStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	firstSamples := []uint32{
		100000,
		200000,
		300000,
		400000,
		500000,
	}
	t.Run("write first device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Writing device latency samples")
		sig, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      &epoch,
			StartTimestampMicroseconds: firstStartTimestampMicroseconds,
			Samples:                    firstSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Debug("==> Wrote device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get device latency samples from PDA and verify that it's updated.
	t.Run("get device latency samples after writing first", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Getting device latency samples from PDA")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Debug("==> Got device latency samples from PDA", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
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
	})

	// Verify that initializing the same account again fails.
	t.Run("try to initialize device latency samples again", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		log.Debug("==> Attempting to initialize device latency samples again (should fail)")
		_, res, err := la2AgentTelemetryClient.InitializeDeviceLatencySamples(ctx, telemetry.InitializeDeviceLatencySamplesInstructionConfig{
			AgentPK:                      la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:               la2DevicePK,
			TargetDevicePK:               ny5DevicePK,
			LinkPK:                       la2ToNy5LinkPK,
			Epoch:                        &epoch,
			SamplingIntervalMicroseconds: 1000000,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		log.Debug("transaction error", "error", res.Meta.Err)
		require.NotNil(t, res.Meta.Err, "transaction should fail")
	})

	// Write more device latency samples.
	secondStartTimestampMicroseconds := uint64(time.Now().UnixMicro())
	secondSamples := []uint32{
		600000,
		700000,
		800000,
		900000,
		1000000,
	}

	t.Run("write second device latency samples", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Writing more device latency samples")
		sig, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      &epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    secondSamples,
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Debug("==> Wrote more device latency samples", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	// Get device latency samples from PDA and verify that it's updated.
	t.Run("get device latency samples after writing second", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Getting device latency samples from PDA")
		deviceLatencySamples, err := la2AgentTelemetryClient.GetDeviceLatencySamples(ctx, la2DevicePK, ny5DevicePK, la2ToNy5LinkPK, epoch)
		require.NoError(t, err)
		log.Debug("==> Got device latency samples from PDA", "deviceLatencySamples", deviceLatencySamples, "duration", time.Since(start))
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
	})

	t.Run("write largest possible batch of samples per transaction", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		start := time.Now()
		log.Debug("==> Writing largest possible batch of samples per transaction")
		sig, res, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      &epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    make([]uint32, telemetry.MaxSamplesPerBatch),
		})
		require.NoError(t, err)
		for _, msg := range res.Meta.LogMessages {
			log.Debug("solana log message", "msg", msg)
		}
		require.Nil(t, res.Meta.Err, "transaction failed: %+v", res.Meta.Err)
		log.Debug("==> Wrote largest possible batch of samples per transaction", "sig", sig, "tx", res, "duration", time.Since(start))
	})

	t.Run("write largest possible batch of samples per transaction +1 (should fail)", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(30*time.Second))
		defer cancel()
		log.Debug("==> Writing largest possible batch of samples per transaction +1 (should fail)")
		_, _, err := la2AgentTelemetryClient.WriteDeviceLatencySamples(ctx, telemetry.WriteDeviceLatencySamplesInstructionConfig{
			AgentPK:                    la2DeviceAgentPrivateKey.PublicKey(),
			OriginDevicePK:             la2DevicePK,
			TargetDevicePK:             ny5DevicePK,
			LinkPK:                     la2ToNy5LinkPK,
			Epoch:                      &epoch,
			StartTimestampMicroseconds: secondStartTimestampMicroseconds,
			Samples:                    make([]uint32, telemetry.MaxSamplesPerBatch+1),
		})
		require.ErrorIs(t, err, telemetry.ErrSamplesBatchTooLarge)
	})
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
