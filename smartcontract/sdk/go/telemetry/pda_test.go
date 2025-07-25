package telemetry_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_DeriveDeviceLatencySamplesPDA(t *testing.T) {
	t.Parallel()

	// Use a known program ID for testing
	programID := solana.NewWallet().PublicKey()
	originDevicePK := solana.NewWallet().PublicKey()
	targetDevicePK := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDA
	pda1, bump1, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, epoch)
	require.NoError(t, err)

	// Verify PDA is not zero
	require.False(t, pda1.IsZero(), "PDA should not be zero")

	// Verify bump is valid (0-255)
	require.LessOrEqual(t, int(bump1), 255, "Invalid bump seed")

	// Test that swapping device pubkeys produces different PDAs
	pda2, _, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, targetDevicePK, originDevicePK, linkPK, epoch)
	require.NoError(t, err)
	require.NotEqual(t, pda1, pda2, "PDA should be different if device key order changes")
}

func TestSDK_Telemetry_DeriveDeviceLatencySamplesPDADifferentEpochs(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	originDevicePK := solana.NewWallet().PublicKey()
	targetDevicePK := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()

	// Derive PDAs for different epochs
	pda1, _, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, 100)
	require.NoError(t, err)

	pda2, _, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, 101)
	require.NoError(t, err)

	// PDAs should be different for different epochs
	require.NotEqual(t, pda1, pda2, "PDAs should be different for different epochs")
}

func TestSDK_Telemetry_DeriveDeviceLatencySamplesPDADifferentLinks(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	originDevicePK := solana.NewWallet().PublicKey()
	targetDevicePK := solana.NewWallet().PublicKey()
	linkPK1 := solana.NewWallet().PublicKey()
	linkPK2 := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDAs for different links
	pda1, _, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK1, epoch)
	require.NoError(t, err)

	pda2, _, err := telemetry.DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK2, epoch)
	require.NoError(t, err)

	// PDAs should be different for different links
	require.NotEqual(t, pda1, pda2, "PDAs should be different for different links")
}

func TestSDK_Telemetry_DeriveInternetLatencySamplesPDA(t *testing.T) {
	t.Parallel()

	// Use a known program ID for testing
	programID := solana.NewWallet().PublicKey()
	dataProviderName := "test"
	originLocationPK := solana.NewWallet().PublicKey()
	targetLocationPK := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDA
	pda1, bump1, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName, originLocationPK, targetLocationPK, epoch)
	require.NoError(t, err)

	// Verify PDA is not zero
	require.False(t, pda1.IsZero(), "PDA should not be zero")

	// Verify bump is valid (0-255)
	require.LessOrEqual(t, int(bump1), 255, "Invalid bump seed")

	// Test that swapping device pubkeys produces different PDAs
	pda2, _, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName, targetLocationPK, originLocationPK, epoch)
	require.NoError(t, err)
	require.NotEqual(t, pda1, pda2, "PDA should be different if device key order changes")
}

func TestSDK_Telemetry_DeriveInternetLatencySamplesPDADifferentEpochs(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	dataProviderName := "test"
	originLocationPK := solana.NewWallet().PublicKey()
	targetLocationPK := solana.NewWallet().PublicKey()

	// Derive PDAs for different epochs
	pda1, _, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName, originLocationPK, targetLocationPK, 100)
	require.NoError(t, err)

	pda2, _, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName, originLocationPK, targetLocationPK, 101)
	require.NoError(t, err)

	// PDAs should be different for different epochs
	require.NotEqual(t, pda1, pda2, "PDAs should be different for different epochs")
}

func TestSDK_Telemetry_DeriveInternetLatencySamplesPDADifferentDataProviders(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	dataProviderName := "test"
	dataProviderName2 := "test2"
	originLocationPK := solana.NewWallet().PublicKey()
	targetLocationPK := solana.NewWallet().PublicKey()
	epoch := uint64(100)

	// Derive PDAs for different data providers
	pda1, _, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName, originLocationPK, targetLocationPK, epoch)
	require.NoError(t, err)

	pda2, _, err := telemetry.DeriveInternetLatencySamplesPDA(programID, dataProviderName2, originLocationPK, targetLocationPK, epoch)
	require.NoError(t, err)

	// PDAs should be different for different data providers
	require.NotEqual(t, pda1, pda2, "PDAs should be different for different data providers")
}
