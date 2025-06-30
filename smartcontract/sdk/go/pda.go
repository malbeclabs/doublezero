package dzsdk

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

// Derives the PDA for device latency samples account
func DeriveDeviceLatencySamplesPDA(
	programID solana.PublicKey,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, uint8, error) {
	// Convert epoch to little-endian bytes
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)

	// Create seeds
	seeds := [][]byte{
		[]byte(SEED_PREFIX),
		[]byte(SEED_DEVICE_LATENCY_SAMPLES),
		originDevicePK[:],
		targetDevicePK[:],
		linkPK[:],
		epochBytes,
	}

	// Find program address
	return solana.FindProgramAddress(seeds, programID)
}
