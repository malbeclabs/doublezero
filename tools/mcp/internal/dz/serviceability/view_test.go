package dzsvc

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type MockServiceabilityRPC struct {
	getProgramDataFunc func(context.Context) (*serviceability.ProgramData, error)
}

func (m *MockServiceabilityRPC) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	if m.getProgramDataFunc != nil {
		return m.getProgramDataFunc(ctx)
	}
	return &serviceability.ProgramData{}, nil
}

func TestMCP_Serviceability_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		require.True(t, view.Ready(), "view should be ready after successful refresh")
	})
}

func TestMCP_Serviceability_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when already ready", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		err = view.WaitReady(ctx)
		require.NoError(t, err, "WaitReady should return immediately when already ready")
	})

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		t.Parallel()

		db, err := duck.NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestMCP_Serviceability_View_NewServiceabilityView(t *testing.T) {
	t.Parallel()

	t.Run("returns error when database initialization fails", func(t *testing.T) {
		t.Parallel()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                &failingDB{},
		})
		require.Error(t, err)
		require.Nil(t, view)
		require.Contains(t, err.Error(), "failed to create tables")
	})
}

func TestMCP_Serviceability_View_ConvertContributors(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain contributors to domain types", func(t *testing.T) {
		t.Parallel()

		pk := [32]byte{1, 2, 3, 4}
		owner := [32]byte{5, 6, 7, 8}

		onchain := []serviceability.Contributor{
			{
				PubKey: pk,
				Owner:  owner,
				Status: serviceability.ContributorStatusActivated,
				Code:   "TEST",
			},
		}

		result := convertContributors(onchain)

		require.Len(t, result, 1)
		require.Equal(t, solana.PublicKeyFromBytes(pk[:]).String(), result[0].PK)
		require.Equal(t, "TEST", result[0].Code)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		result := convertContributors([]serviceability.Contributor{})
		require.Empty(t, result)
	})
}

func TestMCP_Serviceability_View_ConvertDevices(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain devices to domain types", func(t *testing.T) {
		t.Parallel()

		pk := [32]byte{1, 2, 3, 4}
		owner := [32]byte{5, 6, 7, 8}
		contributorPK := [32]byte{9, 10, 11, 12}
		exchangePK := [32]byte{13, 14, 15, 16}
		publicIP := [4]byte{192, 168, 1, 1}

		onchain := []serviceability.Device{
			{
				PubKey:            pk,
				Owner:             owner,
				Status:            serviceability.DeviceStatusActivated,
				Code:              "DEV001",
				PublicIp:          publicIP,
				ContributorPubKey: contributorPK,
				ExchangePubKey:    exchangePK,
			},
		}

		result := convertDevices(onchain)

		require.Len(t, result, 1)
		require.Equal(t, solana.PublicKeyFromBytes(pk[:]).String(), result[0].PK)
		require.Equal(t, "activated", result[0].Status)
		require.Equal(t, "DEV001", result[0].Code)
		require.Equal(t, "192.168.1.1", result[0].PublicIP)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK[:]).String(), result[0].ContributorPK)
		require.Equal(t, solana.PublicKeyFromBytes(exchangePK[:]).String(), result[0].MetroPK)
	})
}

func TestMCP_Serviceability_View_ConvertUsers(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain users to domain types", func(t *testing.T) {
		t.Parallel()

		pk := [32]byte{1, 2, 3, 4}
		owner := [32]byte{5, 6, 7, 8}

		onchain := []serviceability.User{
			{
				PubKey:   pk,
				Owner:    owner,
				Status:   serviceability.UserStatusActivated,
				UserType: serviceability.UserTypeIBRL,
			},
		}

		result := convertUsers(onchain)

		require.Len(t, result, 1)
		require.Equal(t, solana.PublicKeyFromBytes(pk[:]).String(), result[0].PK)
		require.Equal(t, "activated", result[0].Status)
		require.Equal(t, "ibrl", result[0].Kind)
	})
}

func TestMCP_Serviceability_View_ConvertMetros(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain exchanges to domain metros", func(t *testing.T) {
		t.Parallel()

		pk := [32]byte{1, 2, 3, 4}
		owner := [32]byte{5, 6, 7, 8}

		onchain := []serviceability.Exchange{
			{
				PubKey: pk,
				Owner:  owner,
				Status: serviceability.ExchangeStatusActivated,
				Code:   "NYC",
				Name:   "New York",
				Lat:    40.7128,
				Lng:    -74.0060,
			},
		}

		result := convertMetros(onchain)

		require.Len(t, result, 1)
		require.Equal(t, solana.PublicKeyFromBytes(pk[:]).String(), result[0].PK)
		require.Equal(t, "NYC", result[0].Code)
		require.Equal(t, "New York", result[0].Name)
		require.Equal(t, -74.0060, result[0].Longitude)
		require.Equal(t, 40.7128, result[0].Latitude)
	})
}
