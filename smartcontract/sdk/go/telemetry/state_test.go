package telemetry

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_State_DeviceLatencySamples(t *testing.T) {
	t.Run("round-trip serialize/deserialize with samples", func(t *testing.T) {
		origin := solana.NewWallet().PublicKey()
		target := solana.NewWallet().PublicKey()
		loc1 := solana.NewWallet().PublicKey()
		loc2 := solana.NewWallet().PublicKey()
		link := solana.NewWallet().PublicKey()

		original := &DeviceLatencySamples{
			DeviceLatencySamplesHeader: DeviceLatencySamplesHeader{
				AccountType:                  AccountTypeDeviceLatencySamples,
				BumpSeed:                     255,
				Epoch:                        42,
				OriginDeviceAgentPK:          origin,
				OriginDevicePK:               origin,
				TargetDevicePK:               target,
				OriginDeviceLocationPK:       loc1,
				TargetDeviceLocationPK:       loc2,
				LinkPK:                       link,
				SamplingIntervalMicroseconds: 250_000,
				StartTimestampMicroseconds:   1_625_000_000,
				NextSampleIndex:              5,
				Unused:                       [128]byte{88},
			},
			Samples: []uint32{100, 200, 300, 400, 500},
		}

		var buf bytes.Buffer
		require.NoError(t, original.Serialize(&buf))

		var decoded DeviceLatencySamples
		require.NoError(t, decoded.Deserialize(&buf))

		require.Equal(t, original.DeviceLatencySamplesHeader, decoded.DeviceLatencySamplesHeader)
		require.Equal(t, original.Samples, decoded.Samples)
	})

	t.Run("round-trip with empty sample list", func(t *testing.T) {
		header := DeviceLatencySamplesHeader{
			AccountType:                  AccountTypeDeviceLatencySamples,
			SamplingIntervalMicroseconds: 100_000,
			NextSampleIndex:              0,
		}
		original := &DeviceLatencySamples{
			DeviceLatencySamplesHeader: header,
			Samples:                    []uint32{},
		}

		var buf bytes.Buffer
		require.NoError(t, original.Serialize(&buf))

		var decoded DeviceLatencySamples
		require.NoError(t, decoded.Deserialize(&buf))

		require.Equal(t, original.DeviceLatencySamplesHeader, decoded.DeviceLatencySamplesHeader)
		require.Empty(t, decoded.Samples)
	})

	t.Run("sample byte layout is little-endian", func(t *testing.T) {
		sample := &DeviceLatencySamples{
			DeviceLatencySamplesHeader: DeviceLatencySamplesHeader{
				AccountType:     AccountTypeDeviceLatencySamples,
				NextSampleIndex: 3,
			},
			Samples: []uint32{0x11223344, 0x55667788, 0x99aabbcc},
		}

		var buf bytes.Buffer
		require.NoError(t, sample.Serialize(&buf))
		b := buf.Bytes()

		expected := []byte{
			0x44, 0x33, 0x22, 0x11,
			0x88, 0x77, 0x66, 0x55,
			0xcc, 0xbb, 0xaa, 0x99,
		}
		require.GreaterOrEqual(t, len(b), len(expected))
		require.Equal(t, expected, b[len(b)-len(expected):])
	})
}
