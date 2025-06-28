package telemetry

import (
	"testing"

	"github.com/gagliardetto/solana-go"
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
	pda1, bump1, err := DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, epoch)
	require.NoError(t, err)

	// Verify PDA is not zero
	require.False(t, pda1.IsZero(), "PDA should not be zero")

	// Verify bump is valid (0-255)
	require.LessOrEqual(t, int(bump1), 255, "Invalid bump seed")

	// Test that swapping device pubkeys produces different PDAs
	pda2, _, err := DeriveDeviceLatencySamplesPDA(programID, targetDevicePK, originDevicePK, linkPK, epoch)
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
	pda1, _, err := DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, 100)
	require.NoError(t, err)

	pda2, _, err := DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK, 101)
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
	pda1, _, err := DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK1, epoch)
	require.NoError(t, err)

	pda2, _, err := DeriveDeviceLatencySamplesPDA(programID, originDevicePK, targetDevicePK, linkPK2, epoch)
	require.NoError(t, err)

	// PDAs should be different for different links
	require.NotEqual(t, pda1, pda2, "PDAs should be different for different links")
}
