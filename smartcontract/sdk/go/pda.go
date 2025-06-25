package dzsdk

import (
	"bytes"
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

// Derives the PDA for DZ latency samples account
func DeriveDzLatencySamplesPDA(
	programID solana.PublicKey,
	deviceAPk solana.PublicKey,
	deviceZPk solana.PublicKey,
	linkPk solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, uint8, error) {
	// Order the pubkeys to ensure consistent PDA generation
	pkA, pkB := orderPubkeys(deviceAPk, deviceZPk)

	// Convert epoch to little-endian bytes
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)

	// Create seeds
	seeds := [][]byte{
		[]byte(SEED_PREFIX),
		[]byte(SEED_DZ_LATENCY_SAMPLES),
		pkA[:],
		pkB[:],
		linkPk[:],
		epochBytes,
	}

	// Find program address
	return solana.FindProgramAddress(seeds, programID)
}

// Ensures that (A, B) and (B, A) pubkeys map to the same PDA
func orderPubkeys(pkA, pkB solana.PublicKey) (solana.PublicKey, solana.PublicKey) {
	if bytes.Compare(pkA[:], pkB[:]) < 0 {
		return pkA, pkB
	}
	return pkB, pkA
}
