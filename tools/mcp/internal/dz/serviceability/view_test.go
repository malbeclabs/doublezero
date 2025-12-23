package dzsvc

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
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

type failingDB struct{}

func (f *failingDB) Exec(query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) Query(query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) Begin() (*sql.Tx, error) {
	return nil, errors.New("database error")
}
func (f *failingDB) Close() error {
	return nil
}

func TestMCP_Serviceability_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
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

		db, err := sql.Open("duckdb", "")
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

		db, err := sql.Open("duckdb", "")
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

		db, err := sql.Open("duckdb", "")
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
		require.Contains(t, err.Error(), "failed to initialize database")
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

func TestMCP_Serviceability_View_Refresh_SavesToDB(t *testing.T) {
	t.Parallel()

	t.Run("saves contributors to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		contributorPK := [32]byte{1, 2, 3, 4}
		ownerPK := [32]byte{5, 6, 7, 8}

		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPK,
							Code:   "TEST",
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_contributors").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, code string
		err = db.QueryRow("SELECT pk, code FROM dz_contributors LIMIT 1").Scan(&pk, &code)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK[:]).String(), pk)
		require.Equal(t, "TEST", code)
	})

	t.Run("saves devices to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		devicePK := [32]byte{1, 2, 3, 4}
		ownerPK := [32]byte{5, 6, 7, 8}
		contributorPK := [32]byte{9, 10, 11, 12}
		metroPK := [32]byte{13, 14, 15, 16}
		publicIP := [4]byte{192, 168, 1, 1}

		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV001",
							PublicIp:          publicIP,
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_devices").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, status, deviceType, code, publicIPStr, contributorPKStr, metroPKStr string
		err = db.QueryRow("SELECT pk, status, device_type, code, public_ip, contributor_pk, metro_pk FROM dz_devices LIMIT 1").Scan(&pk, &status, &deviceType, &code, &publicIPStr, &contributorPKStr, &metroPKStr)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(devicePK[:]).String(), pk)
		require.Equal(t, "activated", status)
		require.Equal(t, "hybrid", deviceType)
		require.Equal(t, "DEV001", code)
		require.Equal(t, "192.168.1.1", publicIPStr)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK[:]).String(), contributorPKStr)
		require.Equal(t, solana.PublicKeyFromBytes(metroPK[:]).String(), metroPKStr)
	})

	t.Run("saves users to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		userPK := [32]byte{1, 2, 3, 4}
		ownerPK := [32]byte{5, 6, 7, 8}
		devicePK := [32]byte{9, 10, 11, 12}
		clientIP := [4]byte{10, 0, 0, 1}
		dzIP := [4]byte{10, 0, 0, 2}

		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Users: []serviceability.User{
						{
							PubKey:       userPK,
							Owner:        ownerPK,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     clientIP,
							DzIp:         dzIP,
							DevicePubKey: devicePK,
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_users").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, ownerPKStr, status, kind, clientIPStr, dzIPStr, devicePKStr string
		err = db.QueryRow("SELECT pk, owner_pk, status, kind, client_ip, dz_ip, device_pk FROM dz_users LIMIT 1").Scan(&pk, &ownerPKStr, &status, &kind, &clientIPStr, &dzIPStr, &devicePKStr)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(userPK[:]).String(), pk)
		require.Equal(t, solana.PublicKeyFromBytes(ownerPK[:]).String(), ownerPKStr)
		require.Equal(t, "activated", status)
		require.Equal(t, "ibrl", kind)
		require.Equal(t, "10.0.0.1", clientIPStr)
		require.Equal(t, "10.0.0.2", dzIPStr)
		require.Equal(t, solana.PublicKeyFromBytes(devicePK[:]).String(), devicePKStr)
	})

	t.Run("saves links to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		linkPK := [32]byte{1, 2, 3, 4}
		ownerPK := [32]byte{5, 6, 7, 8}
		contributorPK := [32]byte{9, 10, 11, 12}
		sideAPK := [32]byte{13, 14, 15, 16}
		sideZPK := [32]byte{17, 18, 19, 20}
		tunnelNet := [5]byte{10, 0, 0, 0, 24}

		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Links: []serviceability.Link{
						{
							PubKey:            linkPK,
							Owner:             ownerPK,
							Status:            serviceability.LinkStatusActivated,
							Code:              "LINK001",
							TunnelNet:         tunnelNet,
							ContributorPubKey: contributorPK,
							SideAPubKey:       sideAPK,
							SideZPubKey:       sideZPK,
							SideAIfaceName:    "eth0",
							SideZIfaceName:    "eth1",
							LinkType:          serviceability.LinkLinkTypeWAN,
							DelayNs:           1000000,
							JitterNs:          50000,
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_links").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, status, code, tunnelNetStr, contributorPKStr, sideAPKStr, sideZPKStr, sideAIfaceName, sideZIfaceName, linkType string
		var delayNs, jitterNs int64
		err = db.QueryRow("SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, delay_ns, jitter_ns FROM dz_links LIMIT 1").Scan(&pk, &status, &code, &tunnelNetStr, &contributorPKStr, &sideAPKStr, &sideZPKStr, &sideAIfaceName, &sideZIfaceName, &linkType, &delayNs, &jitterNs)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(linkPK[:]).String(), pk)
		require.Equal(t, "activated", status)
		require.Equal(t, "LINK001", code)
		require.Equal(t, "10.0.0.0/24", tunnelNetStr)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK[:]).String(), contributorPKStr)
		require.Equal(t, solana.PublicKeyFromBytes(sideAPK[:]).String(), sideAPKStr)
		require.Equal(t, solana.PublicKeyFromBytes(sideZPK[:]).String(), sideZPKStr)
		require.Equal(t, "eth0", sideAIfaceName)
		require.Equal(t, "eth1", sideZIfaceName)
		require.Equal(t, "WAN", linkType)
		require.Equal(t, int64(1000000), delayNs)
		require.Equal(t, int64(50000), jitterNs)
	})

	t.Run("saves metros to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		metroPK := [32]byte{1, 2, 3, 4}
		ownerPK := [32]byte{5, 6, 7, 8}

		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{
						{
							PubKey: metroPK,
							Owner:  ownerPK,
							Status: serviceability.ExchangeStatusActivated,
							Code:   "NYC",
							Name:   "New York",
							Lat:    40.7128,
							Lng:    -74.0060,
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_metros").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, code, name string
		var longitude, latitude float64
		err = db.QueryRow("SELECT pk, code, name, longitude, latitude FROM dz_metros LIMIT 1").Scan(&pk, &code, &name, &longitude, &latitude)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(metroPK[:]).String(), pk)
		require.Equal(t, "NYC", code)
		require.Equal(t, "New York", name)
		require.InDelta(t, -74.0060, longitude, 0.0001)
		require.InDelta(t, 40.7128, latitude, 0.0001)
	})

	t.Run("replaces existing data on refresh", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		contributorPK1 := [32]byte{1, 2, 3, 4}
		contributorPK2 := [32]byte{5, 6, 7, 8}
		ownerPK := [32]byte{9, 10, 11, 12}

		callCount := 0
		mockRPC := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				callCount++
				if callCount == 1 {
					return &serviceability.ProgramData{
						Contributors: []serviceability.Contributor{
							{
								PubKey: contributorPK1,
								Owner:  ownerPK,
								Code:   "TEST1",
							},
						},
					}, nil
				}
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK2,
							Owner:  ownerPK,
							Code:   "TEST2",
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: mockRPC,
			RefreshInterval:   time.Second,
			DB:                db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM dz_contributors").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk string
		err = db.QueryRow("SELECT pk FROM dz_contributors LIMIT 1").Scan(&pk)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK1[:]).String(), pk)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		err = db.QueryRow("SELECT COUNT(*) FROM dz_contributors").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		err = db.QueryRow("SELECT pk FROM dz_contributors LIMIT 1").Scan(&pk)
		require.NoError(t, err)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK2[:]).String(), pk)
	})
}
