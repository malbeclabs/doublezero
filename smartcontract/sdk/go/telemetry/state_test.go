package telemetry

import (
	"bytes"
	"encoding/binary"
	"testing"

	bin "github.com/gagliardetto/binary"
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
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

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
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

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

	t.Run("rejects deserialization with NextSampleIndex over MaxDeviceLatencySamplesPerAccount", func(t *testing.T) {
		header := DeviceLatencySamplesHeader{
			AccountType:                  AccountTypeDeviceLatencySamples,
			SamplingIntervalMicroseconds: 100_000,
			NextSampleIndex:              MaxDeviceLatencySamplesPerAccount + 1,
		}

		var buf bytes.Buffer
		require.NoError(t, binary.Write(&buf, binary.LittleEndian, header))

		// Add dummy data to simulate the start of the samples section.
		// It won't be read since it should fail before then.
		_, _ = buf.Write(make([]byte, 4)) // one sample's worth of padding

		var decoded DeviceLatencySamples
		err := decoded.Deserialize(buf.Bytes())
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds max allowed samples")
	})
}

func TestSDK_Telemetry_State_InternetLatencySamples(t *testing.T) {
	t.Run("round-trip serialize/deserialize with samples", func(t *testing.T) {
		oracle := solana.NewWallet().PublicKey()
		loc1 := solana.NewWallet().PublicKey()
		loc2 := solana.NewWallet().PublicKey()

		original := &InternetLatencySamples{
			InternetLatencySamplesHeader: InternetLatencySamplesHeader{
				AccountType:                  AccountTypeInternetLatencySamples,
				BumpSeed:                     1,
				Epoch:                        100,
				DataProviderName:             "test-data-provider-1",
				OracleAgentPK:                oracle,
				OriginLocationPK:             loc1,
				TargetLocationPK:             loc2,
				StartTimestampMicroseconds:   1_700_000_000,
				SamplingIntervalMicroseconds: 500_000,
				NextSampleIndex:              4,
				Unused:                       [128]byte{42},
			},
			Samples: []uint32{10, 20, 30, 40},
		}

		var buf bytes.Buffer
		require.NoError(t, original.Serialize(&buf))

		var decoded InternetLatencySamples
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

		require.Equal(t, original.InternetLatencySamplesHeader, decoded.InternetLatencySamplesHeader)
		require.Equal(t, original.Samples, decoded.Samples)
	})

	t.Run("round-trip with empty sample list", func(t *testing.T) {
		header := InternetLatencySamplesHeader{
			AccountType:                  AccountTypeInternetLatencySamples,
			SamplingIntervalMicroseconds: 250_000,
			NextSampleIndex:              0,
		}
		original := &InternetLatencySamples{
			InternetLatencySamplesHeader: header,
			Samples:                      []uint32{},
		}

		var buf bytes.Buffer
		require.NoError(t, original.Serialize(&buf))

		var decoded InternetLatencySamples
		require.NoError(t, decoded.Deserialize(buf.Bytes()))

		require.Equal(t, original.InternetLatencySamplesHeader, decoded.InternetLatencySamplesHeader)
		require.Empty(t, decoded.Samples)
	})

	t.Run("sample byte layout is little-endian", func(t *testing.T) {
		sample := &InternetLatencySamples{
			InternetLatencySamplesHeader: InternetLatencySamplesHeader{
				AccountType:     AccountTypeInternetLatencySamples,
				NextSampleIndex: 3,
			},
			Samples: []uint32{0xdeadbeef, 0x11223344, 0x99aabbcc},
		}

		var buf bytes.Buffer
		require.NoError(t, sample.Serialize(&buf))
		b := buf.Bytes()

		expected := []byte{
			0xef, 0xbe, 0xad, 0xde,
			0x44, 0x33, 0x22, 0x11,
			0xcc, 0xbb, 0xaa, 0x99,
		}
		require.GreaterOrEqual(t, len(b), len(expected))
		require.Equal(t, expected, b[len(b)-len(expected):])
	})

	t.Run("rejects deserialization with NextSampleIndex over MaxInternetLatencySamplesPerAccount", func(t *testing.T) {
		header := InternetLatencySamplesHeader{
			AccountType:                  AccountTypeInternetLatencySamples,
			SamplingIntervalMicroseconds: 123_456,
			NextSampleIndex:              MaxInternetLatencySamplesPerAccount + 1,
		}

		var buf bytes.Buffer
		enc := bin.NewBorshEncoder(&buf)
		require.NoError(t, enc.Encode(header))
		_, _ = buf.Write(make([]byte, 4))

		var decoded InternetLatencySamples
		err := decoded.Deserialize(buf.Bytes())
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds max allowed samples")
	})
}
