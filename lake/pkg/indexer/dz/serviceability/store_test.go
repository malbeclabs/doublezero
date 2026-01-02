package dzsvc

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

type failingDB struct{}

func (f *failingDB) Close() error {
	return nil
}

func (f *failingDB) Catalog() string {
	return "main"
}

func (f *failingDB) Schema() string {
	return "default"
}

func (f *failingDB) Conn(ctx context.Context) (duck.Connection, error) {
	return &failingDBConn{db: f}, nil
}

type failingDBConn struct {
	db *failingDB
}

func (f *failingDBConn) DB() duck.DB {
	if f.db == nil {
		return &failingDB{}
	}
	return f.db
}

func (f *failingDBConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return &sql.Row{}
}

func (f *failingDBConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) Close() error {
	return nil
}

// testPK creates a deterministic public key string from an integer identifier
func testPK(n int) string {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(n + i)
	}
	return solana.PublicKeyFromBytes(bytes).String()
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func TestLake_Serviceability_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				DB: &failingDB{},
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing db", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "db is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

func TestLake_Serviceability_Store_ReplaceContributors(t *testing.T) {
	t.Parallel()

	t.Run("saves contributors to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		contributorPK := testPK(1)

		contributors := []Contributor{
			{
				PK:   contributorPK,
				Code: "TEST",
				Name: "Test Contributor",
			},
		}

		err = store.ReplaceContributors(context.Background(), contributors)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_contributors_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, code, name string
		err = conn.QueryRowContext(ctx, "SELECT pk, code, name FROM dz_contributors_current LIMIT 1").Scan(&pk, &code, &name)
		require.NoError(t, err)
		require.Equal(t, contributorPK, pk)
		require.Equal(t, "TEST", code)
		require.Equal(t, "Test Contributor", name)
	})

	t.Run("replaces existing contributors", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		contributorPK1 := testPK(1)
		contributorPK2 := testPK(2)

		contributors1 := []Contributor{
			{
				PK:   contributorPK1,
				Code: "TEST1",
				Name: "Test Contributor 1",
			},
		}

		err = store.ReplaceContributors(context.Background(), contributors1)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_contributors_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		contributors2 := []Contributor{
			{
				PK:   contributorPK2,
				Code: "TEST2",
				Name: "Test Contributor 2",
			},
		}

		err = store.ReplaceContributors(context.Background(), contributors2)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_contributors_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk string
		err = conn.QueryRowContext(ctx, "SELECT pk FROM dz_contributors_current LIMIT 1").Scan(&pk)
		require.NoError(t, err)
		require.Equal(t, contributorPK2, pk)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		// First insert some data
		contributorPK := testPK(1)
		contributors := []Contributor{
			{
				PK:   contributorPK,
				Code: "TEST",
				Name: "Test Contributor",
			},
		}
		err = store.ReplaceContributors(context.Background(), contributors)
		require.NoError(t, err)

		// Then replace with empty slice
		err = store.ReplaceContributors(context.Background(), []Contributor{})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_contributors_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestLake_Serviceability_Store_ReplaceDevices(t *testing.T) {
	t.Parallel()

	t.Run("saves devices to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		devicePK := testPK(1)
		contributorPK := testPK(2)
		metroPK := testPK(3)

		devices := []Device{
			{
				PK:            devicePK,
				Status:        "activated",
				DeviceType:    "hybrid",
				Code:          "DEV001",
				PublicIP:      "192.168.1.1",
				ContributorPK: contributorPK,
				MetroPK:       metroPK,
			},
		}

		err = store.ReplaceDevices(context.Background(), devices)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_devices_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, status, deviceType, code, publicIPStr, contributorPKStr, metroPKStr string
		err = conn.QueryRowContext(ctx, "SELECT pk, status, device_type, code, public_ip, contributor_pk, metro_pk FROM dz_devices_current LIMIT 1").Scan(&pk, &status, &deviceType, &code, &publicIPStr, &contributorPKStr, &metroPKStr)
		require.NoError(t, err)
		require.Equal(t, devicePK, pk)
		require.Equal(t, "activated", status)
		require.Equal(t, "hybrid", deviceType)
		require.Equal(t, "DEV001", code)
		require.Equal(t, "192.168.1.1", publicIPStr)
		require.Equal(t, contributorPK, contributorPKStr)
		require.Equal(t, metroPK, metroPKStr)
	})
}

func TestLake_Serviceability_Store_ReplaceUsers(t *testing.T) {
	t.Parallel()

	t.Run("saves users to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		defer db.Close()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		userPK := testPK(1)
		ownerPK := testPK(2)
		devicePK := testPK(3)

		users := []User{
			{
				PK:       userPK,
				OwnerPK:  ownerPK,
				Status:   "activated",
				Kind:     "ibrl",
				ClientIP: net.IP{10, 0, 0, 1},
				DZIP:     net.IP{10, 0, 0, 2},
				DevicePK: devicePK,
			},
		}

		err = store.ReplaceUsers(context.Background(), users)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_users_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, ownerPKStr, status, kind, clientIPStr, dzIPStr, devicePKStr string
		err = conn.QueryRowContext(ctx, "SELECT pk, owner_pk, status, kind, client_ip, dz_ip, device_pk FROM dz_users_current LIMIT 1").Scan(&pk, &ownerPKStr, &status, &kind, &clientIPStr, &dzIPStr, &devicePKStr)
		require.NoError(t, err)
		require.Equal(t, userPK, pk)
		require.Equal(t, ownerPK, ownerPKStr)
		require.Equal(t, "activated", status)
		require.Equal(t, "ibrl", kind)
		require.Equal(t, "10.0.0.1", clientIPStr)
		require.Equal(t, "10.0.0.2", dzIPStr)
		require.Equal(t, devicePK, devicePKStr)
	})
}

func TestLake_Serviceability_Store_ReplaceLinks(t *testing.T) {
	t.Parallel()

	t.Run("saves links to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		linkPK := testPK(1)
		contributorPK := testPK(2)
		sideAPK := testPK(3)
		sideZPK := testPK(4)

		links := []Link{
			{
				PK:                linkPK,
				Status:            "activated",
				Code:              "LINK001",
				TunnelNet:         "10.0.0.0/24",
				ContributorPK:     contributorPK,
				SideAPK:           sideAPK,
				SideZPK:           sideZPK,
				SideAIfaceName:    "eth0",
				SideZIfaceName:    "eth1",
				LinkType:          "WAN",
				CommittedRTTNs:    1000000,
				CommittedJitterNs: 50000,
				Bandwidth:         10000000000, // 10Gbps
			},
		}

		err = store.ReplaceLinks(context.Background(), links)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_links_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, status, code, tunnelNetStr, contributorPKStr, sideAPKStr, sideZPKStr, sideAIfaceName, sideZIfaceName, linkType string
		var delayNs, jitterNs, bandwidthBps, delayOverrideNs int64
		err = conn.QueryRowContext(ctx, "SELECT pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns FROM dz_links_current LIMIT 1").Scan(&pk, &status, &code, &tunnelNetStr, &contributorPKStr, &sideAPKStr, &sideZPKStr, &sideAIfaceName, &sideZIfaceName, &linkType, &delayNs, &jitterNs, &bandwidthBps, &delayOverrideNs)
		require.NoError(t, err)
		require.Equal(t, linkPK, pk)
		require.Equal(t, "activated", status)
		require.Equal(t, "LINK001", code)
		require.Equal(t, "10.0.0.0/24", tunnelNetStr)
		require.Equal(t, contributorPK, contributorPKStr)
		require.Equal(t, sideAPK, sideAPKStr)
		require.Equal(t, sideZPK, sideZPKStr)
		require.Equal(t, "eth0", sideAIfaceName)
		require.Equal(t, "eth1", sideZIfaceName)
		require.Equal(t, "WAN", linkType)
		require.Equal(t, int64(1000000), delayNs)
		require.Equal(t, int64(50000), jitterNs)
		require.Equal(t, int64(10000000000), bandwidthBps)
		require.Equal(t, int64(0), delayOverrideNs)
	})
}

func TestLake_Serviceability_Store_ReplaceMetros(t *testing.T) {
	t.Parallel()

	t.Run("saves metros to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		metroPK := testPK(1)

		metros := []Metro{
			{
				PK:        metroPK,
				Code:      "NYC",
				Name:      "New York",
				Longitude: -74.0060,
				Latitude:  40.7128,
			},
		}

		err = store.ReplaceMetros(context.Background(), metros)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_metros_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pk, code, name string
		var longitude, latitude float64
		err = conn.QueryRowContext(ctx, "SELECT pk, code, name, longitude, latitude FROM dz_metros_current LIMIT 1").Scan(&pk, &code, &name, &longitude, &latitude)
		require.NoError(t, err)
		require.Equal(t, metroPK, pk)
		require.Equal(t, "NYC", code)
		require.Equal(t, "New York", name)
		require.InDelta(t, -74.0060, longitude, 0.0001)
		require.InDelta(t, 40.7128, latitude, 0.0001)
	})
}

func TestLake_Serviceability_Store_GetDevices(t *testing.T) {
	t.Parallel()

	t.Run("reads devices from database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		devicePK1 := testPK(1)
		devicePK2 := testPK(2)
		contributorPK := testPK(3)
		metroPK := testPK(4)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create table manually for test (normally created by SCDTableViaCSV)
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_devices_current (
			pk VARCHAR,
			status VARCHAR,
			device_type VARCHAR,
			code VARCHAR,
			public_ip VARCHAR,
			contributor_pk VARCHAR,
			metro_pk VARCHAR,
			max_users INTEGER,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		// Insert with required SCD2 columns (as_of_ts, row_hash)
		// For tests, we use a simple hash value
		_, err = conn.ExecContext(ctx, `INSERT INTO dz_devices_current (pk, status, device_type, code, public_ip, contributor_pk, metro_pk, max_users, as_of_ts, row_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'test_hash1'), (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'test_hash2')`,
			devicePK1, "activated", "hybrid", "DEV1", "192.168.1.1", contributorPK, metroPK, 0,
			devicePK2, "activated", "hybrid", "DEV2", "192.168.1.2", contributorPK, metroPK, 0)
		require.NoError(t, err)

		devices, err := store.GetDevices()
		require.NoError(t, err)
		require.Len(t, devices, 2)
		require.Equal(t, devicePK1, devices[0].PK)
		require.Equal(t, "activated", devices[0].Status)
		require.Equal(t, "hybrid", devices[0].DeviceType)
		require.Equal(t, "DEV1", devices[0].Code)
		require.Equal(t, "192.168.1.1", devices[0].PublicIP)
		require.Equal(t, devicePK2, devices[1].PK)
		require.Equal(t, "DEV2", devices[1].Code)
	})
}

func TestLake_Serviceability_Store_GetLinks(t *testing.T) {
	t.Parallel()

	t.Run("reads links from database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create table manually for test (normally created by SCDTableViaCSV)
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_links_current (
			pk VARCHAR,
			status VARCHAR,
			code VARCHAR,
			tunnel_net VARCHAR,
			contributor_pk VARCHAR,
			side_a_pk VARCHAR,
			side_z_pk VARCHAR,
			side_a_iface_name VARCHAR,
			side_z_iface_name VARCHAR,
			link_type VARCHAR,
			committed_rtt_ns VARCHAR,
			committed_jitter_ns VARCHAR,
			bandwidth_bps VARCHAR,
			isis_delay_override_ns VARCHAR,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		linkPK := testPK(1)
		contributorPK := testPK(2)
		sideAPK := testPK(3)
		sideZPK := testPK(4)

		// Insert with required SCD2 columns (as_of_ts, row_hash)
		_, err = conn.ExecContext(ctx, `INSERT INTO dz_links_current (pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns, as_of_ts, row_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'test_hash')`,
			linkPK, "activated", "LINK1", "10.0.0.0/24", contributorPK, sideAPK, sideZPK, "eth0", "eth1", "WAN", 1000000, 50000, 10000000000, 10)
		require.NoError(t, err)

		links, err := store.GetLinks()
		require.NoError(t, err)
		require.Len(t, links, 1)
		require.Equal(t, linkPK, links[0].PK)
		require.Equal(t, "activated", links[0].Status)
		require.Equal(t, "LINK1", links[0].Code)
		require.Equal(t, "10.0.0.0/24", links[0].TunnelNet)
		require.Equal(t, sideAPK, links[0].SideAPK)
		require.Equal(t, sideZPK, links[0].SideZPK)
		require.Equal(t, "eth0", links[0].SideAIfaceName)
		require.Equal(t, "eth1", links[0].SideZIfaceName)
		require.Equal(t, "WAN", links[0].LinkType)
		require.Equal(t, uint64(1000000), links[0].CommittedRTTNs)
		require.Equal(t, uint64(50000), links[0].CommittedJitterNs)
		require.Equal(t, uint64(10000000000), links[0].Bandwidth)
		require.Equal(t, uint64(10), links[0].ISISDelayOverrideNs)
	})
}

func TestLake_Serviceability_Store_GetContributors(t *testing.T) {
	t.Parallel()

	t.Run("reads contributors from database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		contributorPK1 := testPK(1)
		contributorPK2 := testPK(2)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create table manually for test (normally created by SCDTableViaCSV)
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_contributors_current (
			pk VARCHAR,
			code VARCHAR,
			name VARCHAR,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		// Insert with required SCD2 columns (as_of_ts, row_hash)
		_, err = conn.ExecContext(ctx, `INSERT INTO dz_contributors_current (pk, code, name, as_of_ts, row_hash) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 'test_hash1'), (?, ?, ?, CURRENT_TIMESTAMP, 'test_hash2')`,
			contributorPK1, "CONTRIB1", "Contributor 1",
			contributorPK2, "CONTRIB2", "Contributor 2")
		require.NoError(t, err)

		contributors, err := store.GetContributors()
		require.NoError(t, err)
		require.Len(t, contributors, 2)
		require.Equal(t, contributorPK1, contributors[0].PK)
		require.Equal(t, "CONTRIB1", contributors[0].Code)
		require.Equal(t, "Contributor 1", contributors[0].Name)
		require.Equal(t, contributorPK2, contributors[1].PK)
		require.Equal(t, "CONTRIB2", contributors[1].Code)
		require.Equal(t, "Contributor 2", contributors[1].Name)
	})
}

func TestLake_Serviceability_Store_GetMetros(t *testing.T) {
	t.Parallel()

	t.Run("reads metros from database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create table manually for test (normally created by SCDTableViaCSV)
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_metros_current (
			pk VARCHAR,
			code VARCHAR,
			name VARCHAR,
			longitude VARCHAR,
			latitude VARCHAR,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		metroPK1 := testPK(1)
		metroPK2 := testPK(2)

		// Insert with required SCD2 columns (as_of_ts, row_hash)
		_, err = conn.ExecContext(ctx, `INSERT INTO dz_metros_current (pk, code, name, longitude, latitude, as_of_ts, row_hash) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'test_hash1'), (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'test_hash2')`,
			metroPK1, "NYC", "New York", -74.0060, 40.7128,
			metroPK2, "LAX", "Los Angeles", -118.2437, 34.0522)
		require.NoError(t, err)

		metros, err := store.GetMetros()
		require.NoError(t, err)
		require.Len(t, metros, 2)
		// Order by code: LAX comes before NYC alphabetically
		require.Equal(t, metroPK2, metros[0].PK)
		require.Equal(t, "LAX", metros[0].Code)
		require.Equal(t, "Los Angeles", metros[0].Name)
		require.InDelta(t, -118.2437, metros[0].Longitude, 0.0001)
		require.InDelta(t, 34.0522, metros[0].Latitude, 0.0001)
		require.Equal(t, metroPK1, metros[1].PK)
		require.Equal(t, "NYC", metros[1].Code)
		require.Equal(t, "New York", metros[1].Name)
		require.InDelta(t, -74.0060, metros[1].Longitude, 0.0001)
		require.InDelta(t, 40.7128, metros[1].Latitude, 0.0001)
	})
}
