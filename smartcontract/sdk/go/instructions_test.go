package dzsdk

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestSerializeInitializeDeviceLatencySamples(t *testing.T) {
	originDevicePK := solana.NewWallet().PublicKey()
	targetDevicePK := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()

	args := &InitializeDeviceLatencySamplesArgs{
		OriginDevicePK:               originDevicePK,
		TargetDevicePK:               targetDevicePK,
		LinkPK:                       linkPK,
		Epoch:                        100,
		SamplingIntervalMicroseconds: 1000000, // 1 second
	}

	data, err := SerializeInitializeDeviceLatencySamples(args)
	require.NoError(t, err)

	// Verify discriminator
	require.Equal(t, uint8(InitializeDeviceLatencySamplesInstruction), data[0], "discriminator mismatch")

	// Verify minimum length (discriminator + 3 pubkeys + 2 uint64s)
	expectedMinLength := 1 + 32*3 + 8*2
	require.GreaterOrEqual(t, len(data), expectedMinLength, "data length too short")

	// Verify pubkeys are in the correct positions
	require.Equal(t, originDevicePK[:], data[1:33], "DeviceAPk not serialized correctly")
	require.Equal(t, targetDevicePK[:], data[33:65], "DeviceZPk not serialized correctly")
	require.Equal(t, linkPK[:], data[65:97], "LinkPk not serialized correctly")
}

func TestSerializeWriteDeviceLatencySamples(t *testing.T) {
	samples := []uint32{100, 200, 300, 400, 500}
	args := &WriteDeviceLatencySamplesArgs{
		StartTimestampMicroseconds: 1234567890,
		Samples:                    samples,
	}

	data, err := SerializeWriteDeviceLatencySamples(args)
	require.NoError(t, err)

	// Verify discriminator
	require.Equal(t, uint8(WriteDeviceLatencySamplesInstruction), data[0], "discriminator mismatch")

	// Verify data is not empty beyond discriminator
	require.Greater(t, len(data), 1, "Serialized data is too short")
}

func TestSerializeWriteDeviceLatencySamplesEmpty(t *testing.T) {
	// Test with empty samples
	args := &WriteDeviceLatencySamplesArgs{
		StartTimestampMicroseconds: 1234567890,
		Samples:                    []uint32{},
	}

	data, err := SerializeWriteDeviceLatencySamples(args)
	require.NoError(t, err)

	// Should still have discriminator
	require.Equal(t, uint8(WriteDeviceLatencySamplesInstruction), data[0], "discriminator mismatch")
}

func TestSerializeWriteDeviceLatencySamplesLarge(t *testing.T) {
	samples := make([]uint32, MAX_SAMPLES)
	for i := range samples {
		samples[i] = uint32(i * 100)
	}

	args := &WriteDeviceLatencySamplesArgs{
		StartTimestampMicroseconds: 9876543210,
		Samples:                    samples,
	}

	data, err := SerializeWriteDeviceLatencySamples(args)
	require.NoError(t, err)

	// Verify discriminator
	require.Equal(t, uint8(WriteDeviceLatencySamplesInstruction), data[0], "discriminator mismatch")

	// Verify reasonable size
	require.LessOrEqual(t, len(data), DEVICE_LATENCY_SAMPLES_MAX_SIZE, "Serialized data exceeds max size")
}
