package dzsvc

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
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

func TestLake_Serviceability_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		require.True(t, view.Ready(), "view should be ready after successful refresh")
	})
}

func TestLake_Serviceability_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when already ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     &mockGeoIPResolver{},
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

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestLake_Serviceability_View_NewServiceabilityView(t *testing.T) {
	t.Parallel()

	t.Run("returns error when database initialization fails", func(t *testing.T) {
		t.Parallel()

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			DB:                &failingDB{},
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     &mockGeoIPResolver{},
		})
		require.Error(t, err)
		require.Nil(t, view)
		require.Contains(t, err.Error(), "failed to create tables")
	})
}

func TestLake_Serviceability_View_ConvertContributors(t *testing.T) {
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

func TestLake_Serviceability_View_ConvertDevices(t *testing.T) {
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

func TestLake_Serviceability_View_ConvertUsers(t *testing.T) {
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

func TestLake_Serviceability_View_ConvertMetros(t *testing.T) {
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

func TestLake_Serviceability_View_ConvertLinks(t *testing.T) {
	t.Parallel()

	t.Run("converts onchain links to domain types", func(t *testing.T) {
		t.Parallel()

		pk := [32]byte{1, 2, 3, 4}
		sideAPK := [32]byte{5, 6, 7, 8}
		sideZPK := [32]byte{9, 10, 11, 12}
		contributorPK := [32]byte{13, 14, 15, 16}
		// TunnelNet: [192, 168, 1, 0, 24] = 192.168.1.0/24
		tunnelNet := [5]uint8{192, 168, 1, 0, 24}

		onchain := []serviceability.Link{
			{
				PubKey:            pk,
				Status:            serviceability.LinkStatusActivated,
				Code:              "LINK001",
				TunnelNet:         tunnelNet,
				SideAPubKey:       sideAPK,
				SideZPubKey:       sideZPK,
				ContributorPubKey: contributorPK,
				SideAIfaceName:    "eth0",
				SideZIfaceName:    "eth1",
				LinkType:          serviceability.LinkLinkTypeWAN,
			DelayNs:           5000000,    // 5ms (onchain field name)
			JitterNs:          1000000,    // 1ms (onchain field name)
			Bandwidth:         1000000000, // 1 Gbps
			DelayOverrideNs:   0,           // onchain field name
			},
		}

		result := convertLinks(onchain)

		require.Len(t, result, 1)
		require.Equal(t, solana.PublicKeyFromBytes(pk[:]).String(), result[0].PK)
		require.Equal(t, "activated", result[0].Status)
		require.Equal(t, "LINK001", result[0].Code)
		require.Equal(t, "192.168.1.0/24", result[0].TunnelNet)
		require.Equal(t, solana.PublicKeyFromBytes(sideAPK[:]).String(), result[0].SideAPK)
		require.Equal(t, solana.PublicKeyFromBytes(sideZPK[:]).String(), result[0].SideZPK)
		require.Equal(t, solana.PublicKeyFromBytes(contributorPK[:]).String(), result[0].ContributorPK)
		require.Equal(t, "eth0", result[0].SideAIfaceName)
		require.Equal(t, "eth1", result[0].SideZIfaceName)
		require.Equal(t, "WAN", result[0].LinkType)
		require.Equal(t, uint64(5000000), result[0].CommittedRTTNs)
		require.Equal(t, uint64(1000000), result[0].CommittedJitterNs)
		require.Equal(t, uint64(1000000000), result[0].Bandwidth)
		require.Equal(t, uint64(0), result[0].ISISDelayOverrideNs)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		result := convertLinks([]serviceability.Link{})
		require.Empty(t, result)
	})
}

// testPubkey generates a deterministic test public key from a seed byte
func testPubkey(seed byte) solana.PublicKey {
	var pk [32]byte
	for i := range pk {
		pk[i] = seed
	}
	return solana.PublicKeyFromBytes(pk[:])
}

// testPubkeyBytes generates a deterministic test public key bytes from a seed byte
func testPubkeyBytes(seed byte) [32]byte {
	var pk [32]byte
	for i := range pk {
		pk[i] = seed
	}
	return pk
}

type mockGeoIPResolver struct {
	resolveFunc func(net.IP) *geoip.Record
}

func (m *mockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	if m.resolveFunc != nil {
		return m.resolveFunc(ip)
	}
	return nil
}

type testGeoIPStore struct {
	store *mcpgeoip.Store
	db    duck.DB
}

func newTestGeoIPStore(t *testing.T) (*testGeoIPStore, error) {
	t.Helper()
	db := testDB(t)

	store, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:     db,
	})
	if err != nil {
		return nil, err
	}

	if err := store.CreateTablesIfNotExists(); err != nil {
		return nil, err
	}

	return &testGeoIPStore{
		store: store,
		db:    db,
	}, nil
}

func TestLake_Serviceability_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("stores all data on refresh", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		geoipResolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "1.1.1.1" {
					return &geoip.Record{
						IP:          ip,
						CountryCode: "US",
						Country:     "United States",
						City:        "San Francisco",
					}
				}
				if ip.String() == "8.8.8.8" {
					return &geoip.Record{
						IP:          ip,
						CountryCode: "US",
						Country:     "United States",
						City:        "Mountain View",
					}
				}
				return nil
			},
		}

		// Create test data
		contributorPK := testPubkeyBytes(1)
		metroPK := testPubkeyBytes(2)
		devicePK := testPubkeyBytes(3)
		userPK := testPubkeyBytes(4)
		ownerPK := testPubkeyBytes(5)
		linkPK := testPubkeyBytes(6)
		sideAPK := testPubkeyBytes(7)
		sideZPK := testPubkeyBytes(8)

		rpc := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPK,
							Status: serviceability.ContributorStatusActivated,
							Code:   "TEST",
						},
					},
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK,
							Owner:             ownerPK,
							Status:            serviceability.DeviceStatusActivated,
							DeviceType:        serviceability.DeviceDeviceTypeHybrid,
							Code:              "DEV001",
							PublicIp:          [4]byte{192, 168, 1, 1},
							ContributorPubKey: contributorPK,
							ExchangePubKey:    metroPK,
						},
					},
					Users: []serviceability.User{
						{
							PubKey:       userPK,
							Owner:        ownerPK,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{1, 1, 1, 1},
							DzIp:         [4]byte{10, 0, 0, 1},
							DevicePubKey: devicePK,
						},
						{
							PubKey:       testPubkeyBytes(9),
							Owner:        ownerPK,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{8, 8, 8, 8},
							DzIp:         [4]byte{10, 0, 0, 2},
							DevicePubKey: devicePK,
						},
						{
							PubKey:       testPubkeyBytes(10),
							Owner:        ownerPK,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{0, 0, 0, 0}, // No client IP
							DzIp:         [4]byte{10, 0, 0, 3},
							DevicePubKey: devicePK,
						},
					},
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
					Links: []serviceability.Link{
						{
							PubKey:            linkPK,
							Status:            serviceability.LinkStatusActivated,
							Code:              "LINK001",
							TunnelNet:         [5]uint8{192, 168, 1, 0, 24},
							SideAPubKey:       sideAPK,
							SideZPubKey:       sideZPK,
							ContributorPubKey: contributorPK,
							SideAIfaceName:    "eth0",
							SideZIfaceName:    "eth1",
							LinkType:          serviceability.LinkLinkTypeWAN,
						DelayNs:           5000000,    // onchain field name
						JitterNs:          1000000,    // onchain field name
						Bandwidth:         1000000000,
						DelayOverrideNs:   0,           // onchain field name
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: rpc,
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     geoipResolver,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify contributors were stored
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var contributorsCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_contributors").Scan(&contributorsCount)
		require.NoError(t, err)
		require.Equal(t, 1, contributorsCount, "should have 1 contributor")

		// Verify devices were stored
		var devicesCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_devices").Scan(&devicesCount)
		require.NoError(t, err)
		require.Equal(t, 1, devicesCount, "should have 1 device")

		// Verify users were stored
		var usersCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_users").Scan(&usersCount)
		require.NoError(t, err)
		require.Equal(t, 3, usersCount, "should have 3 users")

		// Verify metros were stored
		var metrosCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_metros").Scan(&metrosCount)
		require.NoError(t, err)
		require.Equal(t, 1, metrosCount, "should have 1 metro")

		// Verify links were stored
		var linksCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_links").Scan(&linksCount)
		require.NoError(t, err)
		require.Equal(t, 1, linksCount, "should have 1 link")

		// Verify geoip records were upserted (only for users with ClientIP)
		records, err := geoipStore.store.GetRecords()
		require.NoError(t, err)
		require.Len(t, records, 2, "should have 2 resolved geoip records")
		// Find records by IP
		var record1, record2 *geoip.Record
		for _, r := range records {
			if r.IP.String() == "1.1.1.1" {
				record1 = r
			}
			if r.IP.String() == "8.8.8.8" {
				record2 = r
			}
		}
		require.NotNil(t, record1, "should have record for 1.1.1.1")
		require.Equal(t, "San Francisco", record1.City)
		require.NotNil(t, record2, "should have record for 8.8.8.8")
		require.Equal(t, "Mountain View", record2.City)

		// Verify specific data in contributors
		var code string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT code FROM dz_contributors WHERE pk = ?", testPubkey(1).String()).Scan(&code)
		require.NoError(t, err)
		require.Equal(t, "TEST", code, "contributor should have correct code")

		// Verify specific data in devices
		var deviceCode string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT code FROM dz_devices WHERE pk = ?", testPubkey(3).String()).Scan(&deviceCode)
		require.NoError(t, err)
		require.Equal(t, "DEV001", deviceCode, "device should have correct code")

		// Verify specific data in metros
		var metroName string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT name FROM dz_metros WHERE pk = ?", testPubkey(2).String()).Scan(&metroName)
		require.NoError(t, err)
		require.Equal(t, "New York", metroName, "metro should have correct name")

		// Verify specific data in links
		var linkCode string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT code FROM dz_links WHERE pk = ?", testPubkey(6).String()).Scan(&linkCode)
		require.NoError(t, err)
		require.Equal(t, "LINK001", linkCode, "link should have correct code")
	})

	t.Run("handles users without client IPs for geoip", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		geoipResolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				// Return nil for zero/unset IPs
				if ip == nil || ip.IsUnspecified() {
					return nil
				}
				return &geoip.Record{IP: ip}
			},
		}

		userPK := testPubkeyBytes(1)
		ownerPK := testPubkeyBytes(2)
		devicePK := testPubkeyBytes(3)

		rpc := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Users: []serviceability.User{
						{
							PubKey:       userPK,
							Owner:        ownerPK,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{0, 0, 0, 0}, // No client IP (zero IP)
							DzIp:         [4]byte{10, 0, 0, 1},
							DevicePubKey: devicePK,
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: rpc,
			RefreshInterval:   time.Second,
			DB:                db,
			GeoIPStore:        geoipStore.store,
			GeoIPResolver:     geoipResolver,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify users are still stored even without geoip
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var usersCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_users").Scan(&usersCount)
		require.NoError(t, err)
		require.Equal(t, 1, usersCount, "should have 1 user even without geoip")

		// Verify no geoip records were upserted
		records, err := geoipStore.store.GetRecords()
		require.NoError(t, err)
		require.Len(t, records, 0, "should have no geoip records when no client IPs")
	})
}
