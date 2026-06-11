package geolocation_test

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_State_ProgramConfig_RoundTrip(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationProgramConfig{
		AccountType:          geolocation.AccountTypeProgramConfig,
		BumpSeed:             255,
		Version:              1,
		MinCompatibleVersion: 1,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeolocationProgramConfig
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.AccountType, decoded.AccountType)
	require.Equal(t, original.BumpSeed, decoded.BumpSeed)
	require.Equal(t, original.Version, decoded.Version)
	require.Equal(t, original.MinCompatibleVersion, decoded.MinCompatibleVersion)
}

func TestSDK_Geolocation_State_GeoProbe_RoundTrip(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 42},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices: []solana.PublicKey{
			solana.NewWallet().PublicKey(),
			solana.NewWallet().PublicKey(),
		},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		ReferenceCount:     5,
		TargetUpdateCount:  42,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeoProbe
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.AccountType, decoded.AccountType)
	require.Equal(t, original.Owner, decoded.Owner)
	require.Equal(t, original.ExchangePK, decoded.ExchangePK)
	require.Equal(t, original.PublicIP, decoded.PublicIP)
	require.Equal(t, original.LocationOffsetPort, decoded.LocationOffsetPort)
	require.Equal(t, original.Code, decoded.Code)
	require.Equal(t, original.ParentDevices, decoded.ParentDevices)
	require.Equal(t, original.MetricsPublisherPK, decoded.MetricsPublisherPK)
	require.Equal(t, original.ReferenceCount, decoded.ReferenceCount)
	require.Equal(t, original.TargetUpdateCount, decoded.TargetUpdateCount)
}

func TestSDK_Geolocation_State_GeoProbe_EmptyParentDevices(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{192, 168, 1, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
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
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{172, 16, 0, 1},
		LocationOffsetPort: 9000,
		Code:               "max-devices-probe",
		ParentDevices:      devices,
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
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

func TestSDK_Geolocation_State_GeoProbe_BackwardCompat_NoTargetUpdateCount(t *testing.T) {
	t.Parallel()

	// Serialize a GeoProbe with TargetUpdateCount, then truncate the last 4 bytes
	// to simulate an old account that was serialized before target_update_count existed.
	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 1},
		LocationOffsetPort: 8923,
		Code:               "old-probe",
		ParentDevices:      []solana.PublicKey{solana.NewWallet().PublicKey()},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		ReferenceCount:     3,
		TargetUpdateCount:  0,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	// Truncate the trailing target_update_count (4 bytes) to simulate old data.
	data := buf.Bytes()[:buf.Len()-4]

	var decoded geolocation.GeoProbe
	require.NoError(t, decoded.Deserialize(data))

	require.Equal(t, original.Owner, decoded.Owner)
	require.Equal(t, original.ParentDevices, decoded.ParentDevices)
	require.Equal(t, uint32(0), decoded.TargetUpdateCount)
}

func TestSDK_Geolocation_State_GeolocationUser_RoundTrip(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "geo-user-01",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant: geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{
				Rate:                 1000,
				LastDeductionDzEpoch: 42,
			},
		},
		Status: geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{
			{
				TargetType:         geolocation.GeoLocationTargetTypeOutbound,
				IPAddress:          [4]uint8{8, 8, 8, 8},
				LocationOffsetPort: 8923,
				TargetPK:           solana.PublicKey{},
				GeoProbePK:         solana.NewWallet().PublicKey(),
			},
			{
				TargetType:         geolocation.GeoLocationTargetTypeInbound,
				IPAddress:          [4]uint8{0, 0, 0, 0},
				LocationOffsetPort: 0,
				TargetPK:           solana.NewWallet().PublicKey(),
				GeoProbePK:         solana.NewWallet().PublicKey(),
			},
		},
		ResultDestination: "185.199.108.1:9000",
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeolocationUser
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.AccountType, decoded.AccountType)
	require.Equal(t, original.Owner, decoded.Owner)
	require.Equal(t, original.Code, decoded.Code)
	require.Equal(t, original.TokenAccount, decoded.TokenAccount)
	require.Equal(t, original.PaymentStatus, decoded.PaymentStatus)
	require.Equal(t, original.Billing, decoded.Billing)
	require.Equal(t, original.Status, decoded.Status)
	require.Len(t, decoded.Targets, 2)
	require.Equal(t, original.Targets[0], decoded.Targets[0])
	require.Equal(t, original.Targets[1], decoded.Targets[1])
	require.Equal(t, original.ResultDestination, decoded.ResultDestination)
}

func TestSDK_Geolocation_State_GeolocationUser_EmptyTargets(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "empty-targets",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusDelinquent,
		Billing: geolocation.GeolocationBillingConfig{
			Variant: geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{
				Rate:                 0,
				LastDeductionDzEpoch: 0,
			},
		},
		Status:            geolocation.GeolocationUserStatusSuspended,
		Targets:           []geolocation.GeolocationTarget{},
		ResultDestination: "",
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	var decoded geolocation.GeolocationUser
	require.NoError(t, decoded.Deserialize(buf.Bytes()))

	require.Equal(t, original.Code, decoded.Code)
	require.Empty(t, decoded.Targets)
	require.Equal(t, geolocation.GeolocationUserStatusSuspended, decoded.Status)
	require.Equal(t, geolocation.GeolocationPaymentStatusDelinquent, decoded.PaymentStatus)
}

func TestSDK_Geolocation_State_GeolocationUser_BackwardCompat_NoResultDestination(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "old-user",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant: geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{
				Rate:                 1000,
				LastDeductionDzEpoch: 42,
			},
		},
		Status: geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{
			{
				TargetType:         geolocation.GeoLocationTargetTypeOutbound,
				IPAddress:          [4]uint8{8, 8, 8, 8},
				LocationOffsetPort: 8923,
				TargetPK:           solana.PublicKey{},
				GeoProbePK:         solana.NewWallet().PublicKey(),
			},
		},
		ResultDestination: "",
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	// Truncate the trailing 4 bytes (empty Borsh string = 4-byte length prefix)
	// to simulate old data without the result_destination field.
	data := buf.Bytes()[:buf.Len()-4]

	var decoded geolocation.GeolocationUser
	require.NoError(t, decoded.Deserialize(data))

	require.Equal(t, original.Owner, decoded.Owner)
	require.Equal(t, original.Targets[0], decoded.Targets[0])
	require.Equal(t, "", decoded.ResultDestination)
}

func TestSDK_Geolocation_State_GeolocationTarget_RoundTrip(t *testing.T) {
	t.Parallel()

	original := geolocation.GeolocationTarget{
		TargetType:         geolocation.GeoLocationTargetTypeOutbound,
		IPAddress:          [4]uint8{192, 168, 1, 1},
		LocationOffsetPort: 9000,
		TargetPK:           solana.NewWallet().PublicKey(),
		GeoProbePK:         solana.NewWallet().PublicKey(),
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	// Deserialize via a GeolocationUser wrapping a single target to test the flow
	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "target-test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant: geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{
				Rate:                 500,
				LastDeductionDzEpoch: 10,
			},
		},
		Status:  geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{original},
	}

	var userBuf bytes.Buffer
	require.NoError(t, user.Serialize(&userBuf))

	var decoded geolocation.GeolocationUser
	require.NoError(t, decoded.Deserialize(userBuf.Bytes()))

	require.Len(t, decoded.Targets, 1)
	require.Equal(t, original, decoded.Targets[0])
}

func TestSDK_Geolocation_State_GeolocationTarget_OutboundIcmp_RoundTrip(t *testing.T) {
	t.Parallel()

	original := geolocation.GeolocationTarget{
		TargetType:         geolocation.GeoLocationTargetTypeOutboundIcmp,
		IPAddress:          [4]uint8{8, 8, 8, 8},
		LocationOffsetPort: 8923,
		TargetPK:           solana.NewWallet().PublicKey(),
		GeoProbePK:         solana.NewWallet().PublicKey(),
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "icmp-test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant: geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{
				Rate:                 500,
				LastDeductionDzEpoch: 10,
			},
		},
		Status:  geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{original},
	}

	var userBuf bytes.Buffer
	require.NoError(t, user.Serialize(&userBuf))

	var decoded geolocation.GeolocationUser
	require.NoError(t, decoded.Deserialize(userBuf.Bytes()))

	require.Len(t, decoded.Targets, 1)
	require.Equal(t, original, decoded.Targets[0])
}

func TestSDK_Geolocation_State_EnumStrings(t *testing.T) {
	t.Parallel()

	require.Equal(t, "delinquent", geolocation.GeolocationPaymentStatusDelinquent.String())
	require.Equal(t, "paid", geolocation.GeolocationPaymentStatusPaid.String())
	require.Equal(t, "activated", geolocation.GeolocationUserStatusActivated.String())
	require.Equal(t, "suspended", geolocation.GeolocationUserStatusSuspended.String())
	require.Equal(t, "outbound", geolocation.GeoLocationTargetTypeOutbound.String())
	require.Equal(t, "inbound", geolocation.GeoLocationTargetTypeInbound.String())
	require.Equal(t, "outbound-icmp", geolocation.GeoLocationTargetTypeOutboundIcmp.String())
}
