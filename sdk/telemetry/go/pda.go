package telemetry

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

func DeriveDeviceLatencySamplesPDA(
	programID solana.PublicKey,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, uint8, error) {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)

	seeds := [][]byte{
		[]byte(TelemetrySeedPrefix),
		[]byte(DeviceLatencySamplesSeed),
		originDevicePK[:],
		targetDevicePK[:],
		linkPK[:],
		epochBytes,
	}

	return solana.FindProgramAddress(seeds, programID)
}

func DeriveInternetLatencySamplesPDA(
	programID solana.PublicKey,
	collectorOraclePK solana.PublicKey,
	dataProviderName string,
	originLocationPK solana.PublicKey,
	targetLocationPK solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, uint8, error) {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)

	seeds := [][]byte{
		[]byte(TelemetrySeedPrefix),
		[]byte(InternetLatencySamplesSeed),
		collectorOraclePK[:],
		[]byte(dataProviderName),
		originLocationPK[:],
		targetLocationPK[:],
		epochBytes,
	}

	return solana.FindProgramAddress(seeds, programID)
}
