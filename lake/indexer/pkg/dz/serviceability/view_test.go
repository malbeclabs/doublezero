package dzsvc

import (
	"context"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
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
	// Return minimal valid data to pass empty snapshot validation
	return &serviceability.ProgramData{
		Contributors: []serviceability.Contributor{{PubKey: [32]byte{1}, Code: "test"}},
		Devices:      []serviceability.Device{{PubKey: [32]byte{2}, Code: "test-device", Status: serviceability.DeviceStatusActivated}},
		Exchanges:    []serviceability.Exchange{{PubKey: [32]byte{3}, Code: "test-metro", Name: "Test Metro"}},
	}, nil
}

func TestLake_Serviceability_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
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

		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
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

		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
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

	t.Run("succeeds (tables are now created automatically by SCD2)", func(t *testing.T) {
		t.Parallel()

		// View creation should succeed (tables are created automatically by SCDTableViaCSV)
		// The failure will happen later when trying to use the database
		mockDB := testClient(t)
		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: &MockServiceabilityRPC{},
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
		})
		require.NoError(t, err)
		require.NotNil(t, view)
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
				DelayOverrideNs:   0,          // onchain field name
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

func TestLake_Serviceability_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("stores all data on refresh", func(t *testing.T) {
		t.Parallel()

		mockDB := testClient(t)

		// Create test data
		contributorPK := testPubkeyBytes(1)
		metroPK := testPubkeyBytes(2)
		devicePK := testPubkeyBytes(3)
		userPK := testPubkeyBytes(4)
		ownerPubkey := testPubkeyBytes(5)
		linkPK := testPubkeyBytes(6)
		sideAPK := testPubkeyBytes(7)
		sideZPK := testPubkeyBytes(8)

		rpc := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Contributors: []serviceability.Contributor{
						{
							PubKey: contributorPK,
							Owner:  ownerPubkey,
							Status: serviceability.ContributorStatusActivated,
							Code:   "TEST",
						},
					},
					Devices: []serviceability.Device{
						{
							PubKey:            devicePK,
							Owner:             ownerPubkey,
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
							Owner:        ownerPubkey,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{1, 1, 1, 1},
							DzIp:         [4]byte{10, 0, 0, 1},
							DevicePubKey: devicePK,
						},
						{
							PubKey:       testPubkeyBytes(9),
							Owner:        ownerPubkey,
							Status:       serviceability.UserStatusActivated,
							UserType:     serviceability.UserTypeIBRL,
							ClientIp:     [4]byte{8, 8, 8, 8},
							DzIp:         [4]byte{10, 0, 0, 2},
							DevicePubKey: devicePK,
						},
						{
							PubKey:       testPubkeyBytes(10),
							Owner:        ownerPubkey,
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
							Owner:  ownerPubkey,
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
							DelayNs:           5000000, // onchain field name
							JitterNs:          1000000, // onchain field name
							Bandwidth:         1000000000,
							DelayOverrideNs:   0, // onchain field name
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: rpc,
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify contributors were stored by querying the database
		conn, err := mockDB.Conn(ctx)
		require.NoError(t, err)

		// Query row counts from history table (current state computed at query time)
		rows, err := conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_contributors_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var contributorCount uint64
		require.NoError(t, rows.Scan(&contributorCount))
		rows.Close()

		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_devices_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var deviceCount uint64
		require.NoError(t, rows.Scan(&deviceCount))
		rows.Close()

		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_users_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var userCount uint64
		require.NoError(t, rows.Scan(&userCount))
		rows.Close()

		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_metros_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var metroCount uint64
		require.NoError(t, rows.Scan(&metroCount))
		rows.Close()

		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_links_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var linkCount uint64
		require.NoError(t, rows.Scan(&linkCount))
		rows.Close()

		require.Equal(t, uint64(1), contributorCount, "should have 1 contributor")
		require.Equal(t, uint64(1), deviceCount, "should have 1 device")
		require.Equal(t, uint64(3), userCount, "should have 3 users")
		require.Equal(t, uint64(1), metroCount, "should have 1 metro")
		require.Equal(t, uint64(1), linkCount, "should have 1 link")

		// Note: GeoIP records are now handled by the geoip view, not the serviceability view

		// Verify specific data using the dataset API
		contributorPKStr := testPubkey(1).String()
		contributorsDataset, err := NewContributorDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, contributorsDataset)
		contributorEntityID := dataset.NewNaturalKey(contributorPKStr).ToSurrogate()
		current, err := contributorsDataset.GetCurrentRow(ctx, conn, contributorEntityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "TEST", current["code"], "contributor should have correct code")

		devicePKStr := testPubkey(3).String()
		devicesDataset, err := NewDeviceDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, devicesDataset)
		deviceEntityID := dataset.NewNaturalKey(devicePKStr).ToSurrogate()
		current, err = devicesDataset.GetCurrentRow(ctx, conn, deviceEntityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "DEV001", current["code"], "device should have correct code")

		metroPKStr := testPubkey(2).String()
		metrosDataset, err := NewMetroDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, metrosDataset)
		metroEntityID := dataset.NewNaturalKey(metroPKStr).ToSurrogate()
		current, err = metrosDataset.GetCurrentRow(ctx, conn, metroEntityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "New York", current["name"], "metro should have correct name")

		linkPKStr := testPubkey(6).String()
		linksDataset, err := NewLinkDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, linksDataset)
		linkEntityID := dataset.NewNaturalKey(linkPKStr).ToSurrogate()
		current, err = linksDataset.GetCurrentRow(ctx, conn, linkEntityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "LINK001", current["code"], "link should have correct code")
	})

	t.Run("handles users without client IPs", func(t *testing.T) {
		t.Parallel()

		mockDB := testClient(t)

		userPK := testPubkeyBytes(1)
		ownerPubkey := testPubkeyBytes(2)
		devicePK := testPubkeyBytes(3)

		rpc := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					// Minimal data to pass empty snapshot validation
					Contributors: []serviceability.Contributor{{PubKey: [32]byte{1}, Code: "test"}},
					Devices:      []serviceability.Device{{PubKey: [32]byte{2}, Code: "test-device", Status: serviceability.DeviceStatusActivated}},
					Exchanges:    []serviceability.Exchange{{PubKey: [32]byte{3}, Code: "test-metro", Name: "Test Metro"}},
					Users: []serviceability.User{
						{
							PubKey:       userPK,
							Owner:        ownerPubkey,
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
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: rpc,
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify users are still stored even without geoip
		conn, err := mockDB.Conn(ctx)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `
			WITH ranked AS (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_dz_users_history
			)
			SELECT count(*) FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var userCount uint64
		require.NoError(t, rows.Scan(&userCount))
		rows.Close()

		require.Equal(t, uint64(1), userCount, "should have 1 user even without geoip")

		// Note: GeoIP records are now handled by the geoip view, not the serviceability view
	})

	t.Run("rejects empty snapshot to prevent mass deletion", func(t *testing.T) {
		t.Parallel()

		mockDB := testClient(t)

		rpc := &MockServiceabilityRPC{
			getProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				// Return empty data - simulates RPC returning no accounts
				return &serviceability.ProgramData{}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:            laketesting.NewLogger(),
			Clock:             clockwork.NewFakeClock(),
			ServiceabilityRPC: rpc,
			RefreshInterval:   time.Second,
			ClickHouse:        mockDB,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "refusing to write snapshot")
	})
}
