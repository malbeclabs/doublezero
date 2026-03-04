package geolocation_test

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_State_ProgramConfig_RoundTrip(t *testing.T) {
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

	var decoded geolocation.GeolocationProgramConfig
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.AccountType, decoded.AccountType)
	require.Equal(t, original.BumpSeed, decoded.BumpSeed)
	require.Equal(t, original.Version, decoded.Version)
	require.Equal(t, original.MinCompatibleVersion, decoded.MinCompatibleVersion)
	require.Equal(t, original.ServiceabilityProgramID, decoded.ServiceabilityProgramID)
}

func TestSDK_Geolocation_State_GeoProbe_RoundTrip(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           254,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 42},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices: []solana.PublicKey{
			solana.NewWallet().PublicKey(),
			solana.NewWallet().PublicKey(),
		},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     5,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeoProbe
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.AccountType, decoded.AccountType)
	require.Equal(t, original.Owner, decoded.Owner)
	require.Equal(t, original.BumpSeed, decoded.BumpSeed)
	require.Equal(t, original.ExchangePK, decoded.ExchangePK)
	require.Equal(t, original.PublicIP, decoded.PublicIP)
	require.Equal(t, original.LocationOffsetPort, decoded.LocationOffsetPort)
	require.Equal(t, original.Code, decoded.Code)
	require.Equal(t, original.ParentDevices, decoded.ParentDevices)
	require.Equal(t, original.MetricsPublisherPK, decoded.MetricsPublisherPK)
	require.Equal(t, original.LatencyThresholdNs, decoded.LatencyThresholdNs)
	require.Equal(t, original.ReferenceCount, decoded.ReferenceCount)
}

func TestSDK_Geolocation_State_GeoProbe_EmptyParentDevices(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           100,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{192, 168, 1, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 500_000,
		ReferenceCount:     0,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeoProbe
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.Code, decoded.Code)
	require.Empty(t, decoded.ParentDevices)
}

func TestSDK_Geolocation_State_GeoProbe_MaxParentDevices(t *testing.T) {
	t.Parallel()

	devices := make([]solana.PublicKey, 5)
	for i := range devices {
		devices[i] = solana.NewWallet().PublicKey()
	}

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           50,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{172, 16, 0, 1},
		LocationOffsetPort: 9000,
		Code:               "max-devices-probe",
		ParentDevices:      devices,
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 2_000_000,
		ReferenceCount:     100,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeoProbe
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Len(t, decoded.ParentDevices, 5)
	for i := range devices {
		require.Equal(t, devices[i], decoded.ParentDevices[i])
	}
}
