package telemetry

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_State_DeviceLatencySamplesV0(t *testing.T) {
	t.Run("round-trip serialize/deserialize with samples", func(t *testing.T) {
		origin := solana.NewWallet().PublicKey()
		target := solana.NewWallet().PublicKey()
		loc1 := solana.NewWallet().PublicKey()
		loc2 := solana.NewWallet().PublicKey()
		link := solana.NewWallet().PublicKey()

		original := &DeviceLatencySamplesV0{
			DeviceLatencySamplesHeaderV0: DeviceLatencySamplesHeaderV0{
				AccountType:                  AccountTypeDeviceLatencySamplesV0,
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

		var decoded DeviceLatencySamplesV0
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

		require.Equal(t, original.DeviceLatencySamplesHeaderV0, decoded.DeviceLatencySamplesHeaderV0)
		require.Equal(t, original.Samples, decoded.Samples)
	})

	t.Run("round-trip with empty sample list", func(t *testing.T) {
		header := DeviceLatencySamplesHeaderV0{
			AccountType:                  AccountTypeDeviceLatencySamplesV0,
			SamplingIntervalMicroseconds: 100_000,
			NextSampleIndex:              0,
		}
		original := &DeviceLatencySamplesV0{
			DeviceLatencySamplesHeaderV0: header,
			Samples:                      []uint32{},
		}

		var buf bytes.Buffer
		require.NoError(t, original.Serialize(&buf))

		var decoded DeviceLatencySamplesV0
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

		require.Equal(t, original.DeviceLatencySamplesHeaderV0, decoded.DeviceLatencySamplesHeaderV0)
		require.Empty(t, decoded.Samples)
	})

	t.Run("sample byte layout is little-endian", func(t *testing.T) {
		sample := &DeviceLatencySamplesV0{
			DeviceLatencySamplesHeaderV0: DeviceLatencySamplesHeaderV0{
				AccountType:     AccountTypeDeviceLatencySamplesV0,
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

	t.Run("rejects deserialization with NextSampleIndex over MaxDeviceLatencySamplesPerAccount", func(t *testing.T) {
		header := DeviceLatencySamplesHeaderV0{
			AccountType:                  AccountTypeDeviceLatencySamplesV0,
			SamplingIntervalMicroseconds: 100_000,
			NextSampleIndex:              MaxDeviceLatencySamplesPerAccount + 1,
		}

		var buf bytes.Buffer
		require.NoError(t, binary.Write(&buf, binary.LittleEndian, header))

		// Add dummy data to simulate the start of the samples section.
		// It won't be read since it should fail before then.
		_, _ = buf.Write(make([]byte, 4)) // one sample's worth of padding

		var decoded DeviceLatencySamplesV0
		err := decoded.Deserialize(buf.Bytes())
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds max allowed samples")
	})

	t.Run("ToV1 conversion", func(t *testing.T) {
		origin := solana.NewWallet().PublicKey()
		target := solana.NewWallet().PublicKey()
		loc1 := solana.NewWallet().PublicKey()
		loc2 := solana.NewWallet().PublicKey()
		link := solana.NewWallet().PublicKey()

		v0 := &DeviceLatencySamplesV0{
			DeviceLatencySamplesHeaderV0: DeviceLatencySamplesHeaderV0{
				AccountType:                  AccountTypeDeviceLatencySamplesV0,
				BumpSeed:                     7,
				Epoch:                        12345,
				OriginDeviceAgentPK:          origin,
				OriginDevicePK:               origin,
				TargetDevicePK:               target,
				OriginDeviceLocationPK:       loc1,
				TargetDeviceLocationPK:       loc2,
				LinkPK:                       link,
				SamplingIntervalMicroseconds: 1_000_000,
				StartTimestampMicroseconds:   2_000_000,
				NextSampleIndex:              3,
				Unused:                       [128]byte{0x42},
			},
			Samples: []uint32{111, 222, 333},
		}

		v1 := v0.ToV1()

		require.Equal(t, AccountTypeDeviceLatencySamples, v1.AccountType)
		require.Equal(t, v0.Epoch, v1.Epoch)
		require.Equal(t, v0.OriginDeviceAgentPK, v1.OriginDeviceAgentPK)
		require.Equal(t, v0.OriginDevicePK, v1.OriginDevicePK)
		require.Equal(t, v0.TargetDevicePK, v1.TargetDevicePK)
		require.Equal(t, v0.OriginDeviceLocationPK, v1.OriginDeviceLocationPK)
		require.Equal(t, v0.TargetDeviceLocationPK, v1.TargetDeviceLocationPK)
		require.Equal(t, v0.LinkPK, v1.LinkPK)
		require.Equal(t, v0.SamplingIntervalMicroseconds, v1.SamplingIntervalMicroseconds)
		require.Equal(t, v0.StartTimestampMicroseconds, v1.StartTimestampMicroseconds)
		require.Equal(t, v0.NextSampleIndex, v1.NextSampleIndex)
		require.Equal(t, v0.Unused, v1.Unused)
		require.Equal(t, v0.Samples, v1.Samples)
	})
}
