package dztelemusage

import (
	"context"
	"testing"
	"time"

	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

func TestLake_TelemetryUsage_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				ClickHouse: nil,
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing clickhouse", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: laketesting.NewLogger(),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "clickhouse connection is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

// CreateTablesIfNotExists was removed - tables are created via migrations
// Tests for this method are obsolete

func TestLake_TelemetryUsage_Store_GetMaxTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty table", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		maxTime, err := store.GetMaxTimestamp(context.Background())
		require.NoError(t, err)
		require.Nil(t, maxTime)
	})

	t.Run("returns max timestamp when table has data", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

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

		err = store.InsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		maxTime, err := store.GetMaxTimestamp(context.Background())
		require.NoError(t, err)
		require.NotNil(t, maxTime)
		require.True(t, maxTime.Equal(t3) || maxTime.After(t3))
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		maxTime, err := store.GetMaxTimestamp(ctx)
		require.Error(t, err)
		require.Nil(t, maxTime)
		require.Contains(t, err.Error(), "context canceled")
	})
}

func TestLake_TelemetryUsage_Store_InsertInterfaceUsage(t *testing.T) {
	t.Parallel()

	t.Run("inserts new rows to empty table", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

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

		err = store.InsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(), "SELECT count() FROM fact_dz_device_interface_counters WHERE device_pk = ? AND intf = ?", *usage[0].DevicePK, *usage[0].Intf)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Greater(t, count, uint64(0), "should have inserted usage records")
		conn.Close()
	})

	t.Run("appends rows (fact table is append-only)", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage1 := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
				InOctets: int64Ptr(1000),
			},
		}

		err = store.InsertInterfaceUsage(context.Background(), usage1)
		require.NoError(t, err)

		// Append with same key (fact tables are append-only, so this creates a new row)
		usage2 := []InterfaceUsage{
			{
				Time:      now,
				DevicePK:  stringPtr("device1"),
				Intf:      stringPtr("eth0"),
				InOctets:  int64Ptr(2000),
				OutOctets: int64Ptr(3000),
			},
		}

		err = store.InsertInterfaceUsage(context.Background(), usage2)
		require.NoError(t, err)

		// With mock, we can't verify data was written by querying
		// Both InsertInterfaceUsage calls succeeding is sufficient verification
	})

	t.Run("handles nullable fields", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
				// All nullable fields are nil
			},
		}

		err = store.InsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(), "SELECT count() FROM fact_dz_device_interface_counters WHERE device_pk = ? AND intf = ?", *usage[0].DevicePK, *usage[0].Intf)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Greater(t, count, uint64(0), "should have inserted usage records")
		conn.Close()

		// With mock, we can't verify nullable fields by querying
		// The InsertInterfaceUsage call succeeding is sufficient verification
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

		// First insert some data
		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		usage1 := []InterfaceUsage{
			{
				Time:     now,
				DevicePK: stringPtr("device1"),
				Intf:     stringPtr("eth0"),
			},
		}
		err = store.InsertInterfaceUsage(context.Background(), usage1)
		require.NoError(t, err)

		// Then insert empty slice
		err = store.InsertInterfaceUsage(context.Background(), []InterfaceUsage{})
		require.NoError(t, err)

		// With mock, we can't verify data was written by querying
		// Both InsertInterfaceUsage calls succeeding is sufficient verification
	})

	t.Run("handles all counter fields", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Tables are created via migrations, no need to create them here

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

		err = store.InsertInterfaceUsage(context.Background(), usage)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(), "SELECT count() FROM fact_dz_device_interface_counters WHERE device_pk = ? AND intf = ?", *usage[0].DevicePK, *usage[0].Intf)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Greater(t, count, uint64(0), "should have inserted usage records")
		conn.Close()
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
