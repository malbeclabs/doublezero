package duck

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInsertFactsViaCSV(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	t.Run("creates_table_and_inserts_facts", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName: "test_facts",
			Columns: []string{
				"time:TIMESTAMP",
				"device_pk:VARCHAR",
				"metric_value:BIGINT",
			},
		}

		now := time.Now().UTC()
		err = InsertFactsViaCSV(ctx, log, conn, cfg, 3, func(w *csv.Writer, i int) error {
			return w.Write([]string{
				now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				fmt.Sprintf("device_%d", i),
				fmt.Sprintf("%d", i*100),
			})
		})
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_facts").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify one row
		var devicePK string
		var metricValue int64
		var ts time.Time
		err = conn.QueryRowContext(ctx, "SELECT device_pk, metric_value, time FROM test_facts WHERE device_pk = 'device_0'").Scan(&devicePK, &metricValue, &ts)
		require.NoError(t, err)
		require.Equal(t, "device_0", devicePK)
		require.Equal(t, int64(0), metricValue)
	})

	t.Run("handles_empty_facts", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName: "test_facts_empty",
			Columns: []string{
				"time:TIMESTAMP",
				"value:BIGINT",
			},
		}

		err = InsertFactsViaCSV(ctx, log, conn, cfg, 0, func(w *csv.Writer, i int) error {
			return nil
		})
		require.NoError(t, err)

		// Table should exist but be empty
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_facts_empty").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("creates_partitioned_table_for_ducklake", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// This test only works if we're using DuckLake
		if _, ok := conn.DB().(*Lake); !ok {
			t.Skip("not using DuckLake, skipping partitioning test")
		}

		cfg := FactTableConfig{
			TableName:       "test_facts_partitioned",
			PartitionByTime: true,
			TimeColumn:      "time",
			Columns: []string{
				"time:TIMESTAMP",
				"device_pk:VARCHAR",
				"value:BIGINT",
			},
		}

		now := time.Now().UTC()
		err = InsertFactsViaCSV(ctx, log, conn, cfg, 2, func(w *csv.Writer, i int) error {
			return w.Write([]string{
				now.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
				fmt.Sprintf("device_%d", i),
				fmt.Sprintf("%d", i),
			})
		})
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_facts_partitioned").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("appends_to_existing_table", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName: "test_facts_append",
			Columns: []string{
				"time:TIMESTAMP",
				"value:BIGINT",
			},
		}

		// First insert
		now := time.Now().UTC()
		err = InsertFactsViaCSV(ctx, log, conn, cfg, 2, func(w *csv.Writer, i int) error {
			return w.Write([]string{
				now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				fmt.Sprintf("%d", i),
			})
		})
		require.NoError(t, err)

		// Second insert (append)
		err = InsertFactsViaCSV(ctx, log, conn, cfg, 2, func(w *csv.Writer, i int) error {
			return w.Write([]string{
				now.Add(time.Duration(i+2) * time.Minute).Format(time.RFC3339),
				fmt.Sprintf("%d", i+2),
			})
		})
		require.NoError(t, err)

		// Verify total count
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_facts_append").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 4, count)
	})

	t.Run("validates_column_format", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName: "test_facts_invalid",
			Columns: []string{
				"time:TIMESTAMP",
				"invalid_column", // Missing type
			},
		}

		err = InsertFactsViaCSV(ctx, log, conn, cfg, 1, func(w *csv.Writer, i int) error {
			return w.Write([]string{"2024-01-01T00:00:00Z", "value"})
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid column definition")
	})

	t.Run("validates_time_column_when_partitioning", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName:       "test_facts_no_time",
			PartitionByTime: true,
			// TimeColumn missing
			Columns: []string{
				"time:TIMESTAMP",
				"value:BIGINT",
			},
		}

		err = InsertFactsViaCSV(ctx, log, conn, cfg, 1, func(w *csv.Writer, i int) error {
			return w.Write([]string{"2024-01-01T00:00:00Z", "1"})
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "time_column is required")
	})

	t.Run("handles_context_cancellation", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		cfg := FactTableConfig{
			TableName: "test_facts_cancel",
			Columns: []string{
				"time:TIMESTAMP",
				"value:BIGINT",
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err = InsertFactsViaCSV(ctx, log, conn, cfg, 1, func(w *csv.Writer, i int) error {
			return w.Write([]string{"2024-01-01T00:00:00Z", "1"})
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "context canceled")
	})
}
