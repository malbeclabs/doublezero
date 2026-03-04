package geolocation_test

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_DeserializeProgramConfig_HappyPath(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationProgramConfig{
		AccountType:             geolocation.AccountTypeProgramConfig,
		BumpSeed:                255,
		Version:                 1,
		MinCompatibleVersion:    1,
		ServiceabilityProgramID: solana.NewWallet().PublicKey(),
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	result, err := geolocation.DeserializeProgramConfig(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, original.AccountType, result.AccountType)
	require.Equal(t, original.ServiceabilityProgramID, result.ServiceabilityProgramID)
}

func TestSDK_Geolocation_DeserializeProgramConfig_WrongAccountType(t *testing.T) {
	t.Parallel()

	// Create data with wrong account type
	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           100,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 0, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     0,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	_, err := geolocation.DeserializeProgramConfig(buf.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected account type")
}

func TestSDK_Geolocation_DeserializeProgramConfig_TruncatedData(t *testing.T) {
	t.Parallel()

	_, err := geolocation.DeserializeProgramConfig([]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestSDK_Geolocation_DeserializeGeoProbe_HappyPath(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           200,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 42},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices: []solana.PublicKey{
			solana.NewWallet().PublicKey(),
		},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     3,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	result, err := geolocation.DeserializeGeoProbe(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, original.AccountType, result.AccountType)
	require.Equal(t, original.Code, result.Code)
	require.Len(t, result.ParentDevices, 1)
}

func TestSDK_Geolocation_DeserializeGeoProbe_WrongAccountType(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationProgramConfig{
		AccountType:             geolocation.AccountTypeProgramConfig,
		BumpSeed:                255,
		Version:                 1,
		MinCompatibleVersion:    1,
		ServiceabilityProgramID: solana.NewWallet().PublicKey(),
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	_, err := geolocation.DeserializeGeoProbe(buf.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected account type")
}

func TestSDK_Geolocation_DeserializeGeoProbe_TruncatedData(t *testing.T) {
	t.Parallel()

	_, err := geolocation.DeserializeGeoProbe([]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestSDK_Geolocation_DeserializeGeoProbe_ExtraTrailingBytes(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           100,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 0, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     0,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	// Append extra trailing bytes
	data := append(buf.Bytes(), 0xFF, 0xFE, 0xFD)

	result, err := geolocation.DeserializeGeoProbe(data)
	require.NoError(t, err)
	require.Equal(t, original.Code, result.Code)
}
