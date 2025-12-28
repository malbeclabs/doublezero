package dztelemusage

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

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

func TestAI_MCP_TelemetryUsage_Store_NewStore(t *testing.T) {
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

func TestAI_MCP_TelemetryUsage_Store_CreateTablesIfNotExists(t *testing.T) {
	t.Parallel()

	t.Run("creates table successfully", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Verify table exists by querying it
		var count int
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_iface_usage").Scan(&count)
		require.NoError(t, err, "table dz_device_iface_usage should exist")
		require.Equal(t, 0, count)
	})

	t.Run("returns error when database fails", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     &failingDB{},
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create table")
	})
}

func TestAI_MCP_TelemetryUsage_Store_GetMaxTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty table", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		maxTime, err := store.GetMaxTimestamp(context.Background())
		require.NoError(t, err)
		require.Nil(t, maxTime)
	})

	t.Run("returns max timestamp when table has data", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Insert test data with different timestamps
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
		t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		usage := []InterfaceUsage{
			{
				Time:     t1,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
			},
			{
				Time:     t2,
				DevicePK: stringPtr("device2"),
				Intf:     stringPtr("eth1"),
			},
			{
				Time:     t3,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		maxTime, err := store.GetMaxTimestamp(context.Background())
		require.NoError(t, err)
		require.NotNil(t, maxTime)
		require.True(t, maxTime.Equal(t3) || maxTime.After(t3))
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		maxTime, err := store.GetMaxTimestamp(ctx)
		require.Error(t, err)
		require.Nil(t, maxTime)
		require.Contains(t, err.Error(), "context canceled")
	})
}

func TestAI_MCP_TelemetryUsage_Store_UpsertInterfaceUsage(t *testing.T) {
	t.Parallel()

	t.Run("upserts new rows to empty table", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage := []InterfaceUsage{
			{
				Time:           now,
				DevicePK:       stringPtr("device1"),
				Intf:           stringPtr("eth0"),
				UserTunnelID:   int64Ptr(501),
				LinkPK:         stringPtr("link1"),
				LinkSide:       stringPtr("A"),
				ModelName:      stringPtr("ModelX"),
				SerialNumber:   stringPtr("SN123"),
				InOctets:       int64Ptr(1000),
				OutOctets:      int64Ptr(2000),
				InPkts:         int64Ptr(10),
				OutPkts:        int64Ptr(20),
				InOctetsDelta:  int64Ptr(100),
				OutOctetsDelta: int64Ptr(200),
				InPktsDelta:    int64Ptr(1),
				OutPktsDelta:   int64Ptr(2),
				DeltaDuration:  float64Ptr(60.0),
			},
			{
				Time:     now.Add(time.Minute),
				DevicePK: stringPtr("device2"),
				Intf:     stringPtr("eth1"),
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_iface_usage").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Verify first row with all fields
		var timeVal time.Time
		var devicePK, intf, userTunnelID, linkPK, linkSide, modelName, serialNumber sql.NullString
		var inOctets, outOctets, inPkts, outPkts sql.NullInt64
		var inOctetsDelta, outOctetsDelta, inPktsDelta, outPktsDelta sql.NullInt64
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var deltaDuration sql.NullFloat64

		err = conn.QueryRowContext(ctx, `
			SELECT time, device_pk, intf, user_tunnel_id, link_pk, link_side, model_name, serial_number,
			       in_octets, out_octets, in_pkts, out_pkts,
			       in_octets_delta, out_octets_delta, in_pkts_delta, out_pkts_delta,
			       delta_duration
			FROM dz_device_iface_usage
			WHERE device_pk = 'device1' AND intf = 'eth0'
		`).Scan(
			&timeVal, &devicePK, &intf, &userTunnelID, &linkPK, &linkSide, &modelName, &serialNumber,
			&inOctets, &outOctets, &inPkts, &outPkts,
			&inOctetsDelta, &outOctetsDelta, &inPktsDelta, &outPktsDelta,
			&deltaDuration,
		)
		require.NoError(t, err)
		require.True(t, timeVal.Equal(now))
		require.True(t, devicePK.Valid)
		require.Equal(t, "device1", devicePK.String)
		require.True(t, intf.Valid)
		require.Equal(t, "eth0", intf.String)
		require.True(t, userTunnelID.Valid)
		require.Equal(t, "501", userTunnelID.String)
		require.True(t, linkPK.Valid)
		require.Equal(t, "link1", linkPK.String)
		require.True(t, linkSide.Valid)
		require.Equal(t, "A", linkSide.String)
		require.True(t, modelName.Valid)
		require.Equal(t, "ModelX", modelName.String)
		require.True(t, serialNumber.Valid)
		require.Equal(t, "SN123", serialNumber.String)
		require.True(t, inOctets.Valid)
		require.Equal(t, int64(1000), inOctets.Int64)
		require.True(t, outOctets.Valid)
		require.Equal(t, int64(2000), outOctets.Int64)
		require.True(t, inPkts.Valid)
		require.Equal(t, int64(10), inPkts.Int64)
		require.True(t, outPkts.Valid)
		require.Equal(t, int64(20), outPkts.Int64)
		require.True(t, inOctetsDelta.Valid)
		require.Equal(t, int64(100), inOctetsDelta.Int64)
		require.True(t, outOctetsDelta.Valid)
		require.Equal(t, int64(200), outOctetsDelta.Int64)
		require.True(t, inPktsDelta.Valid)
		require.Equal(t, int64(1), inPktsDelta.Int64)
		require.True(t, outPktsDelta.Valid)
		require.Equal(t, int64(2), outPktsDelta.Int64)
		require.True(t, deltaDuration.Valid)
		require.InDelta(t, 60.0, deltaDuration.Float64, 0.01)
	})

	t.Run("updates existing rows", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage1 := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
				InOctets: int64Ptr(1000),
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage1)
		require.NoError(t, err)

		// Update with new values
		usage2 := []InterfaceUsage{
			{
				Time:      now,
				DevicePK:  stringPtr("device1"),
				Intf:      stringPtr("eth0"),
				InOctets:  int64Ptr(2000),
				OutOctets: int64Ptr(3000),
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage2)
		require.NoError(t, err)

		// Verify row count is still 1 (not 2)
		var count int
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_iface_usage").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		// Verify row was updated
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var inOctets, outOctets sql.NullInt64
		err = conn.QueryRowContext(ctx, "SELECT in_octets, out_octets FROM dz_device_iface_usage WHERE device_pk = 'device1' AND intf = 'eth0'").Scan(&inOctets, &outOctets)
		require.NoError(t, err)
		require.True(t, inOctets.Valid)
		require.Equal(t, int64(2000), inOctets.Int64)
		require.True(t, outOctets.Valid)
		require.Equal(t, int64(3000), outOctets.Int64)
	})

	t.Run("handles nullable fields", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
				// All nullable fields are nil
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify row was inserted with nulls
		var count int
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_iface_usage").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		// Verify nullable fields are null
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var userTunnelID, linkPK, linkSide, modelName sql.NullString
		var inOctets sql.NullInt64
		var deltaDuration sql.NullFloat64

		err = conn.QueryRowContext(ctx, `
			SELECT user_tunnel_id, link_pk, link_side, model_name, in_octets, delta_duration
			FROM dz_device_iface_usage
			WHERE device_pk = 'device1' AND intf = 'eth0'
		`).Scan(&userTunnelID, &linkPK, &linkSide, &modelName, &inOctets, &deltaDuration)
		require.NoError(t, err)
		require.False(t, userTunnelID.Valid)
		require.False(t, linkPK.Valid)
		require.False(t, linkSide.Valid)
		require.False(t, modelName.Valid)
		require.False(t, inOctets.Valid)
		require.False(t, deltaDuration.Valid)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// First insert some data
		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage1 := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
			},
		}
		err = store.UpsertInterfaceUsage(context.Background(), usage1)
		require.NoError(t, err)

		// Then upsert empty slice
		err = store.UpsertInterfaceUsage(context.Background(), []InterfaceUsage{})
		require.NoError(t, err)

		// Verify existing data is still there (not truncated)
		var count int
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_iface_usage").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("handles all counter fields", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage := []InterfaceUsage{
			{
				Time:                    now,
				DevicePK:                stringPtr("device1"),
				Intf:                    stringPtr("eth0"),
				CarrierTransitions:      int64Ptr(1),
				InBroadcastPkts:         int64Ptr(2),
				InDiscards:              int64Ptr(3),
				InErrors:                int64Ptr(4),
				InFCSErrors:             int64Ptr(5),
				InMulticastPkts:         int64Ptr(6),
				InOctets:                int64Ptr(7),
				InPkts:                  int64Ptr(8),
				InUnicastPkts:           int64Ptr(9),
				OutBroadcastPkts:        int64Ptr(10),
				OutDiscards:             int64Ptr(11),
				OutErrors:               int64Ptr(12),
				OutMulticastPkts:        int64Ptr(13),
				OutOctets:               int64Ptr(14),
				OutPkts:                 int64Ptr(15),
				OutUnicastPkts:          int64Ptr(16),
				CarrierTransitionsDelta: int64Ptr(101),
				InBroadcastPktsDelta:    int64Ptr(102),
				InDiscardsDelta:         int64Ptr(103),
				InErrorsDelta:           int64Ptr(104),
				InFCSErrorsDelta:        int64Ptr(105),
				InMulticastPktsDelta:    int64Ptr(106),
				InOctetsDelta:           int64Ptr(107),
				InPktsDelta:             int64Ptr(108),
				InUnicastPktsDelta:      int64Ptr(109),
				OutBroadcastPktsDelta:   int64Ptr(110),
				OutDiscardsDelta:        int64Ptr(111),
				OutErrorsDelta:          int64Ptr(112),
				OutMulticastPktsDelta:   int64Ptr(113),
				OutOctetsDelta:          int64Ptr(114),
				OutPktsDelta:            int64Ptr(115),
				OutUnicastPktsDelta:     int64Ptr(116),
				DeltaDuration:           float64Ptr(60.5),
			},
		}

		err = store.UpsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify all fields were stored
		var carrierTransitions, inBroadcastPkts, inDiscards, inErrors, inFCSErrors sql.NullInt64
		var inMulticastPkts, inOctets, inPkts, inUnicastPkts sql.NullInt64
		var outBroadcastPkts, outDiscards, outErrors, outMulticastPkts sql.NullInt64
		var outOctets, outPkts, outUnicastPkts sql.NullInt64
		var carrierTransitionsDelta, inBroadcastPktsDelta, inDiscardsDelta sql.NullInt64
		var inErrorsDelta, inFCSErrorsDelta, inMulticastPktsDelta sql.NullInt64
		var inOctetsDelta, inPktsDelta, inUnicastPktsDelta sql.NullInt64
		var outBroadcastPktsDelta, outDiscardsDelta, outErrorsDelta sql.NullInt64
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var outMulticastPktsDelta, outOctetsDelta, outPktsDelta, outUnicastPktsDelta sql.NullInt64
		var deltaDuration sql.NullFloat64

		err = conn.QueryRowContext(ctx, `
			SELECT
				carrier_transitions, in_broadcast_pkts, in_discards, in_errors, in_fcs_errors,
				in_multicast_pkts, in_octets, in_pkts, in_unicast_pkts,
				out_broadcast_pkts, out_discards, out_errors, out_multicast_pkts,
				out_octets, out_pkts, out_unicast_pkts,
				carrier_transitions_delta, in_broadcast_pkts_delta, in_discards_delta,
				in_errors_delta, in_fcs_errors_delta, in_multicast_pkts_delta,
				in_octets_delta, in_pkts_delta, in_unicast_pkts_delta,
				out_broadcast_pkts_delta, out_discards_delta, out_errors_delta,
				out_multicast_pkts_delta, out_octets_delta, out_pkts_delta, out_unicast_pkts_delta,
				delta_duration
			FROM dz_device_iface_usage
			WHERE device_pk = 'device1' AND intf = 'eth0'
		`).Scan(
			&carrierTransitions, &inBroadcastPkts, &inDiscards, &inErrors, &inFCSErrors,
			&inMulticastPkts, &inOctets, &inPkts, &inUnicastPkts,
			&outBroadcastPkts, &outDiscards, &outErrors, &outMulticastPkts,
			&outOctets, &outPkts, &outUnicastPkts,
			&carrierTransitionsDelta, &inBroadcastPktsDelta, &inDiscardsDelta,
			&inErrorsDelta, &inFCSErrorsDelta, &inMulticastPktsDelta,
			&inOctetsDelta, &inPktsDelta, &inUnicastPktsDelta,
			&outBroadcastPktsDelta, &outDiscardsDelta, &outErrorsDelta,
			&outMulticastPktsDelta, &outOctetsDelta, &outPktsDelta, &outUnicastPktsDelta,
			&deltaDuration,
		)
		require.NoError(t, err)

		// Verify all raw counter values
		require.Equal(t, int64(1), carrierTransitions.Int64)
		require.Equal(t, int64(2), inBroadcastPkts.Int64)
		require.Equal(t, int64(3), inDiscards.Int64)
		require.Equal(t, int64(4), inErrors.Int64)
		require.Equal(t, int64(5), inFCSErrors.Int64)
		require.Equal(t, int64(6), inMulticastPkts.Int64)
		require.Equal(t, int64(7), inOctets.Int64)
		require.Equal(t, int64(8), inPkts.Int64)
		require.Equal(t, int64(9), inUnicastPkts.Int64)
		require.Equal(t, int64(10), outBroadcastPkts.Int64)
		require.Equal(t, int64(11), outDiscards.Int64)
		require.Equal(t, int64(12), outErrors.Int64)
		require.Equal(t, int64(13), outMulticastPkts.Int64)
		require.Equal(t, int64(14), outOctets.Int64)
		require.Equal(t, int64(15), outPkts.Int64)
		require.Equal(t, int64(16), outUnicastPkts.Int64)

		// Verify all delta values
		require.Equal(t, int64(101), carrierTransitionsDelta.Int64)
		require.Equal(t, int64(102), inBroadcastPktsDelta.Int64)
		require.Equal(t, int64(103), inDiscardsDelta.Int64)
		require.Equal(t, int64(104), inErrorsDelta.Int64)
		require.Equal(t, int64(105), inFCSErrorsDelta.Int64)
		require.Equal(t, int64(106), inMulticastPktsDelta.Int64)
		require.Equal(t, int64(107), inOctetsDelta.Int64)
		require.Equal(t, int64(108), inPktsDelta.Int64)
		require.Equal(t, int64(109), inUnicastPktsDelta.Int64)
		require.Equal(t, int64(110), outBroadcastPktsDelta.Int64)
		require.Equal(t, int64(111), outDiscardsDelta.Int64)
		require.Equal(t, int64(112), outErrorsDelta.Int64)
		require.Equal(t, int64(113), outMulticastPktsDelta.Int64)
		require.Equal(t, int64(114), outOctetsDelta.Int64)
		require.Equal(t, int64(115), outPktsDelta.Int64)
		require.Equal(t, int64(116), outUnicastPktsDelta.Int64)

		// Verify delta duration
		require.InDelta(t, 60.5, deltaDuration.Float64, 0.01)
	})
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}
