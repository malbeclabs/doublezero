package dz

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_DZ_ServiceabilityView_New_NilArgs(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	rpc := &MockServiceabilityRPC{}

	view, err := NewServiceabilityView(nil, rpc)
	require.Error(t, err)
	require.Nil(t, view)

	view, err = NewServiceabilityView(logger, nil)
	require.Error(t, err)
	require.Nil(t, view)
}

func TestGlobalMonitor_DZ_ServiceabilityView_New_Success(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	rpc := &MockServiceabilityRPC{}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, logger, view.log)
	require.Equal(t, rpc, view.rpc)
}

func TestGlobalMonitor_DZ_ServiceabilityView_GetProgramData_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	// Keys
	exchangePK := solana.NewWallet().PublicKey()
	devicePK := solana.NewWallet().PublicKey()
	userPK := solana.NewWallet().PublicKey()
	validatorPK := solana.NewWallet().PublicKey()

	// DzIp: create a v4 IP in a fixed-size array, as serviceability.User uses an array type.
	v4 := net.ParseIP("10.0.0.1").To4()
	require.NotNil(t, v4)
	var dzIP [4]byte
	copy(dzIP[:], v4)

	exchange := serviceability.Exchange{
		PubKey: exchangePK,
		Code:   "EXCH",
		Name:   "Example Exchange",
	}

	device := serviceability.Device{
		PubKey:         devicePK,
		Code:           "DEV1",
		ExchangePubKey: exchangePK,
	}

	user := serviceability.User{
		PubKey:          userPK,
		ValidatorPubKey: validatorPK,
		DzIp:            dzIP,
		DevicePubKey:    devicePK,
	}

	programData := &serviceability.ProgramData{
		Exchanges: []serviceability.Exchange{exchange},
		Devices:   []serviceability.Device{device},
		Users:     []serviceability.User{user},
	}

	rpc := &MockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return programData, nil
		},
	}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)

	out, err := view.GetProgramData(ctx)
	require.NoError(t, err)
	require.NotNil(t, out)

	// Exchanges
	require.Len(t, out.ExchangesByPK, 1)
	exOut, ok := out.ExchangesByPK[exchangePK]
	require.True(t, ok)
	require.Equal(t, exchangePK, exOut.PubKey)
	require.Equal(t, "EXCH", exOut.Code)
	require.Equal(t, "Example Exchange", exOut.Name)

	// Devices
	require.Len(t, out.DevicesByPK, 1)
	require.Len(t, out.DevicesByCode, 1)

	devByPK, ok := out.DevicesByPK[devicePK]
	require.True(t, ok)
	require.Equal(t, devicePK, devByPK.PubKey)
	require.Equal(t, "DEV1", devByPK.Code)
	require.NotNil(t, devByPK.Exchange)
	require.Equal(t, exOut, devByPK.Exchange)

	devByCode, ok := out.DevicesByCode["DEV1"]
	require.True(t, ok)
	require.Equal(t, devicePK, devByCode.PubKey)
	require.Equal(t, "DEV1", devByCode.Code)
	require.NotNil(t, devByCode.Exchange)
	require.Equal(t, exOut, devByCode.Exchange)

	// Users
	require.Len(t, out.UsersByPK, 1)
	require.Len(t, out.UsersByDZIP, 1)

	userOut, ok := out.UsersByPK[userPK]
	require.True(t, ok)
	require.Equal(t, userPK, userOut.PubKey)
	require.Equal(t, validatorPK, userOut.ValidatorPK)
	require.Equal(t, "10.0.0.1", userOut.DZIP.String())
	require.NotNil(t, userOut.Device)
	require.Equal(t, devicePK, userOut.Device.PubKey)
	require.Equal(t, "DEV1", userOut.Device.Code)
	require.NotNil(t, userOut.Device.Exchange)
	require.Equal(t, exOut, userOut.Device.Exchange)

	userOutByIP, ok := out.UsersByDZIP["10.0.0.1"]
	require.True(t, ok)
	require.Equal(t, userPK, userOutByIP.PubKey)
}

func TestGlobalMonitor_DZ_ServiceabilityView_GetProgramData_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	expErr := errors.New("backend error")

	rpc := &MockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return nil, expErr
		},
	}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)

	data, err := view.GetProgramData(ctx)
	require.Error(t, err)
	require.Nil(t, data)
	require.ErrorContains(t, err, "failed to get program data")
	require.ErrorIs(t, err, expErr)
}

func TestGlobalMonitor_DZ_ServiceabilityView_GetProgramData_EmptyProgram(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	rpc := &MockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{
				Users:     []serviceability.User{},
				Devices:   []serviceability.Device{},
				Exchanges: []serviceability.Exchange{},
			}, nil
		},
	}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)

	out, err := view.GetProgramData(ctx)
	require.NoError(t, err)
	require.NotNil(t, out)

	require.Empty(t, out.UsersByPK)
	require.Empty(t, out.UsersByDZIP)
	require.Empty(t, out.DevicesByPK)
	require.Empty(t, out.DevicesByCode)
	require.Empty(t, out.ExchangesByPK)
}

func TestGlobalMonitor_DZ_ServiceabilityView_GetProgramData_UserMissingDevice(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	userPK := solana.NewWallet().PublicKey()
	validatorPK := solana.NewWallet().PublicKey()
	missingDevicePK := solana.NewWallet().PublicKey()

	v4 := net.ParseIP("192.168.1.10").To4()
	require.NotNil(t, v4)
	var dzIP [4]byte
	copy(dzIP[:], v4)

	user := serviceability.User{
		PubKey:          userPK,
		ValidatorPubKey: validatorPK,
		DzIp:            dzIP,
		DevicePubKey:    missingDevicePK,
	}

	pd := &serviceability.ProgramData{
		Users:     []serviceability.User{user},
		Devices:   []serviceability.Device{},   // no devices
		Exchanges: []serviceability.Exchange{}, // no exchanges
	}

	rpc := &MockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return pd, nil
		},
	}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)

	out, err := view.GetProgramData(ctx)
	require.NoError(t, err)
	require.NotNil(t, out)

	userOut, ok := out.UsersByPK[userPK]
	require.True(t, ok)
	require.Equal(t, "192.168.1.10", userOut.DZIP.String())
	require.Nil(t, userOut.Device, "device should be nil if DevicePubKey not found in DevicesByPK")
}

func TestGlobalMonitor_DZ_ServiceabilityView_GetProgramData_DuplicateClientIP_PrefersNonMulticast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	exchangePK := solana.NewWallet().PublicKey()
	ibrlDevicePK := solana.NewWallet().PublicKey()
	multicastDevicePK := solana.NewWallet().PublicKey()
	ibrlUserPK := solana.NewWallet().PublicKey()
	multicastUserPK := solana.NewWallet().PublicKey()

	clientIPBytes := net.ParseIP("104.248.20.159").To4()
	require.NotNil(t, clientIPBytes)
	var clientIP [4]byte
	copy(clientIP[:], clientIPBytes)

	var ibrlDZIP [4]byte
	copy(ibrlDZIP[:], net.ParseIP("84.32.223.131").To4())
	var multicastDZIP [4]byte
	copy(multicastDZIP[:], net.ParseIP("84.32.223.132").To4())

	pd := &serviceability.ProgramData{
		Exchanges: []serviceability.Exchange{
			{PubKey: exchangePK, Code: "EXCH", Name: "Exchange"},
		},
		Devices: []serviceability.Device{
			{PubKey: ibrlDevicePK, Code: "frankry", ExchangePubKey: exchangePK},
			{PubKey: multicastDevicePK, Code: "fr2-dzx-001", ExchangePubKey: exchangePK},
		},
		// Multicast user listed first to ensure ordering doesn't matter.
		Users: []serviceability.User{
			{
				PubKey:          multicastUserPK,
				ValidatorPubKey: solana.NewWallet().PublicKey(),
				ClientIp:        clientIP,
				DzIp:            multicastDZIP,
				DevicePubKey:    multicastDevicePK,
				UserType:        serviceability.UserTypeMulticast,
			},
			{
				PubKey:          ibrlUserPK,
				ValidatorPubKey: solana.NewWallet().PublicKey(),
				ClientIp:        clientIP,
				DzIp:            ibrlDZIP,
				DevicePubKey:    ibrlDevicePK,
				UserType:        serviceability.UserTypeIBRLWithAllocatedIP,
			},
		},
	}

	rpc := &MockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return pd, nil
		},
	}

	view, err := NewServiceabilityView(logger, rpc)
	require.NoError(t, err)

	out, err := view.GetProgramData(ctx)
	require.NoError(t, err)

	// Both users should exist by PK.
	require.Len(t, out.UsersByPK, 2)

	// UsersByClientIP should prefer the non-multicast user.
	userByClientIP, ok := out.UsersByClientIP["104.248.20.159"]
	require.True(t, ok)
	require.Equal(t, ibrlUserPK, userByClientIP.PubKey)
	require.Equal(t, UserTypeIBRLWithAllocatedIP, userByClientIP.UserType)
	require.NotNil(t, userByClientIP.Device)
	require.Equal(t, "frankry", userByClientIP.Device.Code)
}

type MockServiceabilityRPC struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *MockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	if m.GetProgramDataFunc == nil {
		return nil, nil
	}
	return m.GetProgramDataFunc(ctx)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}
