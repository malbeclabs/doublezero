package geolocation_test

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_DeserializeProgramConfig_HappyPath(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationProgramConfig{
		AccountType:          geolocation.AccountTypeProgramConfig,
		BumpSeed:             255,
		Version:              1,
		MinCompatibleVersion: 1,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	result, err := geolocation.DeserializeProgramConfig(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, original.AccountType, result.AccountType)
	require.Equal(t, original.Version, result.Version)
}

func TestSDK_Geolocation_DeserializeProgramConfig_WrongAccountType(t *testing.T) {
	t.Parallel()

	// Create data with wrong account type
	original := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 0, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
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
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 42},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices: []solana.PublicKey{
			solana.NewWallet().PublicKey(),
		},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
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
		AccountType:          geolocation.AccountTypeProgramConfig,
		BumpSeed:             255,
		Version:              1,
		MinCompatibleVersion: 1,
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
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 0, 1},
		LocationOffsetPort: 8923,
		Code:               "test",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
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

func TestSDK_Geolocation_DeserializeGeolocationUser_HappyPath(t *testing.T) {
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
		},
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	result, err := geolocation.DeserializeGeolocationUser(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, original.AccountType, result.AccountType)
	require.Equal(t, original.Code, result.Code)
	require.Len(t, result.Targets, 1)
}

func TestSDK_Geolocation_DeserializeGeolocationUser_WrongAccountType(t *testing.T) {
	t.Parallel()

	original := &geolocation.GeolocationProgramConfig{
		AccountType:          geolocation.AccountTypeProgramConfig,
		BumpSeed:             255,
		Version:              1,
		MinCompatibleVersion: 1,
	}

	var buf bytes.Buffer
	require.NoError(t, original.Serialize(&buf))

	_, err := geolocation.DeserializeGeolocationUser(buf.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected account type")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_TruncatedData(t *testing.T) {
	t.Parallel()

	_, err := geolocation.DeserializeGeolocationUser([]byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_OversizedCodeLength(t *testing.T) {
	t.Parallel()

	// Craft raw bytes with a valid discriminator but a code length prefix
	// that exceeds MaxCodeLength. Layout: account_type(1) + owner(32) + update_count(4) + code_len(4).
	data := make([]byte, 41)
	data[0] = byte(geolocation.AccountTypeGeolocationUser)
	// Set code length to 255 (> MaxCodeLength=32) at offset 37.
	data[37] = 255

	_, err := geolocation.DeserializeGeolocationUser(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "code length")
	require.Contains(t, err.Error(), "exceeds max")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_OversizedTargetCount(t *testing.T) {
	t.Parallel()

	// Build a valid user with 0 targets, then patch the target count to exceed MaxTargets.
	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant:      geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{Rate: 100, LastDeductionDzEpoch: 1},
		},
		Status:  geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{},
	}

	var buf bytes.Buffer
	require.NoError(t, user.Serialize(&buf))
	data := buf.Bytes()

	// Target count is the last 4 bytes (code "test" = 4 bytes).
	// Offset: 1 + 32 + 4 + 4 + 4 + 32 + 1 + 17 + 1 = 96.
	targetCountOffset := 1 + 32 + 4 + 4 + len("test") + 32 + 1 + 17 + 1
	data[targetCountOffset] = 0xFF
	data[targetCountOffset+1] = 0xFF
	data[targetCountOffset+2] = 0x00
	data[targetCountOffset+3] = 0x00 // 65535 > MaxTargets

	_, err := geolocation.DeserializeGeolocationUser(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "targets count")
	require.Contains(t, err.Error(), "exceeds max")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_InvalidPaymentStatus(t *testing.T) {
	t.Parallel()

	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant:      geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{Rate: 100, LastDeductionDzEpoch: 1},
		},
		Status:  geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{},
	}

	var buf bytes.Buffer
	require.NoError(t, user.Serialize(&buf))
	data := buf.Bytes()

	// PaymentStatus is at offset: 1 + 32 + 4 + 4 + len("test") + 32 = 77.
	paymentOffset := 1 + 32 + 4 + 4 + len("test") + 32
	data[paymentOffset] = 99 // invalid

	_, err := geolocation.DeserializeGeolocationUser(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid payment status")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_InvalidUserStatus(t *testing.T) {
	t.Parallel()

	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant:      geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{Rate: 100, LastDeductionDzEpoch: 1},
		},
		Status:  geolocation.GeolocationUserStatusActivated,
		Targets: []geolocation.GeolocationTarget{},
	}

	var buf bytes.Buffer
	require.NoError(t, user.Serialize(&buf))
	data := buf.Bytes()

	// Status is at offset: 1 + 32 + 4 + 4 + len("test") + 32 + 1 + 17 = 95.
	statusOffset := 1 + 32 + 4 + 4 + len("test") + 32 + 1 + 17
	data[statusOffset] = 99 // invalid

	_, err := geolocation.DeserializeGeolocationUser(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid user status")
}

func TestSDK_Geolocation_DeserializeGeolocationUser_InvalidTargetType(t *testing.T) {
	t.Parallel()

	user := &geolocation.GeolocationUser{
		AccountType:   geolocation.AccountTypeGeolocationUser,
		Owner:         solana.NewWallet().PublicKey(),
		Code:          "test",
		TokenAccount:  solana.NewWallet().PublicKey(),
		PaymentStatus: geolocation.GeolocationPaymentStatusPaid,
		Billing: geolocation.GeolocationBillingConfig{
			Variant:      geolocation.BillingConfigFlatPerEpoch,
			FlatPerEpoch: geolocation.FlatPerEpochConfig{Rate: 100, LastDeductionDzEpoch: 1},
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
	}

	var buf bytes.Buffer
	require.NoError(t, user.Serialize(&buf))
	data := buf.Bytes()

	// First target starts at offset: 1 + 32 + 4 + 4 + len("test") + 32 + 1 + 17 + 1 + 4 = 100.
	// The first byte of the target is TargetType.
	targetOffset := 1 + 32 + 4 + 4 + len("test") + 32 + 1 + 17 + 1 + 4
	data[targetOffset] = 99 // invalid target type

	_, err := geolocation.DeserializeGeolocationUser(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid target type")
}
