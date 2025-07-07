package telemetry_test

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

func TestSDK_Telemetry_State(t *testing.T) {
	t.Run("serialize and deserialize round trip", func(t *testing.T) {
		agent := solana.NewWallet().PublicKey()
		origin := solana.NewWallet().PublicKey()
		target := solana.NewWallet().PublicKey()
		originLoc := solana.NewWallet().PublicKey()
		targetLoc := solana.NewWallet().PublicKey()
		link := solana.NewWallet().PublicKey()

		header := telemetry.DeviceLatencySamplesHeader{
			AccountType:                  telemetry.AccountTypeDeviceLatencySamples,
			Epoch:                        42,
			OriginDeviceAgentPK:          agent,
			OriginDevicePK:               origin,
			TargetDevicePK:               target,
			OriginDeviceLocationPK:       originLoc,
			TargetDeviceLocationPK:       targetLoc,
			LinkPK:                       link,
			SamplingIntervalMicroseconds: 50000,
			StartTimestampMicroseconds:   uint64(time.Now().UnixMicro()),
			NextSampleIndex:              5,
		}
		copy(header.Unused[:], make([]byte, 128))

		samples := make([]uint32, header.NextSampleIndex)
		for i := range samples {
			samples[i] = rand.Uint32()
		}

		original := &telemetry.DeviceLatencySamples{
			DeviceLatencySamplesHeader: header,
			Samples:                    samples,
		}

		serialized, err := original.Serialize()
		require.NoError(t, err)
		require.NotEmpty(t, serialized)
		require.True(t, bytes.HasPrefix(serialized, []byte{byte(telemetry.AccountTypeDeviceLatencySamples)}))

		deserialized, err := telemetry.DeserializeDeviceLatencySamples(serialized)
		require.NoError(t, err)
		require.Equal(t, original.DeviceLatencySamplesHeader, deserialized.DeviceLatencySamplesHeader)
		require.Equal(t, original.Samples, deserialized.Samples)
	})

	t.Run("deserialize fails on short header", func(t *testing.T) {
		_, err := telemetry.DeserializeDeviceLatencySamples([]byte{1, 2, 3})
		require.Error(t, err)
		require.Contains(t, err.Error(), "account data too short for header")
	})

	t.Run("deserialize fails on truncated samples", func(t *testing.T) {
		header := telemetry.DeviceLatencySamplesHeader{
			Epoch:                        42,
			OriginDeviceAgentPK:          solana.NewWallet().PublicKey(),
			OriginDevicePK:               solana.NewWallet().PublicKey(),
			TargetDevicePK:               solana.NewWallet().PublicKey(),
			OriginDeviceLocationPK:       solana.NewWallet().PublicKey(),
			TargetDeviceLocationPK:       solana.NewWallet().PublicKey(),
			LinkPK:                       solana.NewWallet().PublicKey(),
			SamplingIntervalMicroseconds: 50000,
			NextSampleIndex:              2,
		}
		copy(header.Unused[:], make([]byte, 128))

		hdrBytes, err := borsh.Serialize(&header)
		require.NoError(t, err)
		// NOTE: Borsh adds a 1 byte padding to every thing it serializes. Not every field, but
		// just every top-level structure or type. So we need to add 1 to the expected size.
		require.Equal(t, telemetry.DEVICE_LATENCY_SAMPLES_HEADER_SIZE+1, len(hdrBytes))

		// 2 samples expected (8 bytes), only provide 4
		truncated := append(hdrBytes, 0x01, 0x02, 0x03, 0x04)
		_, err = telemetry.DeserializeDeviceLatencySamples(truncated)
		require.Error(t, err)
		require.Contains(t, err.Error(), "account data too short for sample count")
	})
}
