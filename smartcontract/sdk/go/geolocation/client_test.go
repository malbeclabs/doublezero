package geolocation_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
	"github.com/stretchr/testify/require"
)

func TestSDK_Geolocation_Client_GetProgramConfig_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	expected := &geolocation.GeolocationProgramConfig{
		AccountType:             geolocation.AccountTypeProgramConfig,
		BumpSeed:                255,
		Version:                 1,
		MinCompatibleVersion:    1,
		ServiceabilityProgramID: solana.NewWallet().PublicKey(),
	}

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			buf := new(bytes.Buffer)
			if err := expected.Serialize(buf); err != nil {
				t.Fatalf("mock serialize: %v", err)
			}
			return &solanarpc.GetAccountInfoResult{
				Value: &solanarpc.Account{
					Data: solanarpc.DataBytesOrJSONFromBytes(buf.Bytes()),
				},
			}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	got, err := client.GetProgramConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, expected.AccountType, got.AccountType)
	require.Equal(t, expected.ServiceabilityProgramID, got.ServiceabilityProgramID)
}

func TestSDK_Geolocation_Client_GetProgramConfig_NotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	_, err := client.GetProgramConfig(context.Background())
	require.ErrorIs(t, err, geolocation.ErrAccountNotFound)
}

func TestSDK_Geolocation_Client_GetGeoProbeByCode_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	expected := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           200,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 42},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices:      []solana.PublicKey{solana.NewWallet().PublicKey()},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     3,
	}

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			buf := new(bytes.Buffer)
			if err := expected.Serialize(buf); err != nil {
				t.Fatalf("mock serialize: %v", err)
			}
			return &solanarpc.GetAccountInfoResult{
				Value: &solanarpc.Account{
					Data: solanarpc.DataBytesOrJSONFromBytes(buf.Bytes()),
				},
			}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	got, err := client.GetGeoProbeByCode(context.Background(), "ams-probe-01")
	require.NoError(t, err)
	require.Equal(t, expected.Code, got.Code)
	require.Equal(t, expected.PublicIP, got.PublicIP)
}

func TestSDK_Geolocation_Client_GetGeoProbeByCode_NotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetAccountInfoFunc: func(_ context.Context, _ solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	_, err := client.GetGeoProbeByCode(context.Background(), "nonexistent")
	require.ErrorIs(t, err, geolocation.ErrAccountNotFound)
}

func TestSDK_Geolocation_Client_GetGeoProbes_HappyPath(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	probe1 := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           200,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 1},
		LocationOffsetPort: 8923,
		Code:               "ams-probe-01",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     0,
	}
	probe2 := &geolocation.GeoProbe{
		AccountType:        geolocation.AccountTypeGeoProbe,
		Owner:              solana.NewWallet().PublicKey(),
		BumpSeed:           201,
		ExchangePK:         solana.NewWallet().PublicKey(),
		PublicIP:           [4]uint8{10, 0, 1, 2},
		LocationOffsetPort: 8923,
		Code:               "fra-probe-01",
		ParentDevices:      []solana.PublicKey{},
		MetricsPublisherPK: solana.NewWallet().PublicKey(),
		LatencyThresholdNs: 1_000_000,
		ReferenceCount:     0,
	}

	mockRPC := &mockRPCClient{
		GetProgramAccountsWithOptsFunc: func(_ context.Context, _ solana.PublicKey, _ *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error) {
			var buf1, buf2 bytes.Buffer
			if err := probe1.Serialize(&buf1); err != nil {
				t.Fatalf("mock serialize: %v", err)
			}
			if err := probe2.Serialize(&buf2); err != nil {
				t.Fatalf("mock serialize: %v", err)
			}
			return solanarpc.GetProgramAccountsResult{
				{
					Pubkey: solana.NewWallet().PublicKey(),
					Account: &solanarpc.Account{
						Data: solanarpc.DataBytesOrJSONFromBytes(buf1.Bytes()),
					},
				},
				{
					Pubkey: solana.NewWallet().PublicKey(),
					Account: &solanarpc.Account{
						Data: solanarpc.DataBytesOrJSONFromBytes(buf2.Bytes()),
					},
				},
			}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	probes, err := client.GetGeoProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, probes, 2)
	require.Equal(t, "ams-probe-01", probes[0].Code)
	require.Equal(t, "fra-probe-01", probes[1].Code)
}

func TestSDK_Geolocation_Client_GetGeoProbes_Empty(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	mockRPC := &mockRPCClient{
		GetProgramAccountsWithOptsFunc: func(_ context.Context, _ solana.PublicKey, _ *solanarpc.GetProgramAccountsOpts) (solanarpc.GetProgramAccountsResult, error) {
			return solanarpc.GetProgramAccountsResult{}, nil
		},
	}

	client := geolocation.New(slog.Default(), mockRPC, &signer, programID)
	probes, err := client.GetGeoProbes(context.Background())
	require.NoError(t, err)
	require.Empty(t, probes)
}
