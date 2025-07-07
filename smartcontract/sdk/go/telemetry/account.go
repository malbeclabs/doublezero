package telemetry

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

var (
	SeedPrefix               = []byte("telemetry")
	SeedDeviceLatencySamples = []byte("device-latency-samples")
)

func DeriveDeviceLatencySamplesSeed(
	programID, origin, target, link solana.PublicKey,
	epoch uint64,
) (string, error) {
	h := sha256.New()
	h.Write(programID[:])
	h.Write(SeedPrefix)
	h.Write(SeedDeviceLatencySamples)
	h.Write(origin[:])
	h.Write(target[:])
	h.Write(link[:])

	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	h.Write(epochBytes)

	sum := h.Sum(nil)
	encoded := base58.Encode(sum)

	if len(encoded) < 32 {
		return "", fmt.Errorf("derived seed is too short")
	}
	return encoded[:32], nil
}

func DeriveDeviceLatencySamplesAddress(
	agentPK, programID, origin, target, link solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, string, error) {
	seed, err := DeriveDeviceLatencySamplesSeed(programID, origin, target, link, epoch)
	if err != nil {
		return solana.PublicKey{}, "", err
	}

	addr, err := solana.CreateWithSeed(agentPK, seed, programID)
	if err != nil {
		return solana.PublicKey{}, "", fmt.Errorf("create_with_seed failed: %w", err)
	}

	return addr, seed, nil
}
