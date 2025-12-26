package duck

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAI_MCP_Duck_UpsertTableViaCSV(t *testing.T) {
	t.Parallel()

	t.Run("upserts new rows to empty table", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create test table with
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_upsert (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []struct {
			id   int
			name string
		}{
			{1, "Alice"},
			{2, "Bob"},
			{3, "Charlie"},
		}

		err = UpsertTableViaCSV(
			context.Background(),
			log,
			conn,
			"test_upsert",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", data[i].id),
					data[i].name,
				})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_upsert").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify specific row
		var name string
		err = conn.QueryRowContext(context.Background(), "SELECT name FROM test_upsert WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)
	})

	t.Run("updates existing rows", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create test table with and insert initial data
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_upsert_update (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = conn.ExecContext(context.Background(), `INSERT INTO test_upsert_update VALUES (1, 'Old1'), (2, 'Old2')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []struct {
			id   int
			name string
		}{
			{1, "New1"},
			{2, "New2"},
		}

		err = UpsertTableViaCSV(
			context.Background(),
			log,
			conn,
			"test_upsert_update",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", data[i].id),
					data[i].name,
				})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify row count is still 2 (not 4)
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_upsert_update").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Verify rows were updated
		var name string
		err = conn.QueryRowContext(context.Background(), "SELECT name FROM test_upsert_update WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "New1", name)

		err = conn.QueryRowContext(context.Background(), "SELECT name FROM test_upsert_update WHERE id = 2").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "New2", name)
	})

	t.Run("upserts mix of new and existing rows", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table with and insert initial data
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_mix (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `INSERT INTO test_upsert_mix VALUES (1, 'Existing1'), (2, 'Existing2')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []struct {
			id   int
			name string
		}{
			{1, "Updated1"}, // Update existing
			{3, "New1"},     // Insert new
			{4, "New2"},     // Insert new
		}

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_mix",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", data[i].id),
					data[i].name,
				})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify total count is 4 (2 existing + 2 new, 1 updated)
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_upsert_mix").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 4, count)

		// Verify updated row
		var name string
		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_mix WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "Updated1", name)

		// Verify existing row unchanged
		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_mix WHERE id = 2").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "Existing2", name)

		// Verify new rows
		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_mix WHERE id = 3").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "New1", name)

		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_mix WHERE id = 4").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "New2", name)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table and insert data
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_empty (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `INSERT INTO test_upsert_empty VALUES (1, 'Existing')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_empty",
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify existing data is still there (not truncated)
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_upsert_empty").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("handles context cancellation during CSV write", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_cancel (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx2, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx2,
			log,
			conn,
			"test_upsert_cancel",
			5,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("handles context cancellation before transaction", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_cancel2 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx2, cancel := context.WithCancel(context.Background())

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		// Cancel after a short delay to allow CSV writing to start
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err = UpsertTableViaCSV(
			ctx2,
			log,
			conn,
			"test_upsert_cancel2",
			100, // Large enough to take some time
			func(w *csv.Writer, i int) error {
				time.Sleep(1 * time.Millisecond) // Small delay to allow cancellation
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("handles CSV write error", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_error (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_error",
			2,
			func(w *csv.Writer, i int) error {
				if i == 1 {
					return errors.New("write error")
				}
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to write CSV record")
	})

	t.Run("handles database transaction error", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		failingConn, err := (&failingDB{}).Conn(ctx)
		require.NoError(t, err)
		defer failingConn.Close()
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx,
			log,
			failingConn,
			"test_table",
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to begin transaction")
	})

	t.Run("handles temp table creation error", func(t *testing.T) {
		t.Parallel()

		// Use a table that doesn't exist to cause temp table creation error
		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"nonexistent_table",
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create temp table")
	})

	t.Run("handles COPY FROM error", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		// Create table with different schema to cause COPY error
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_copy_error (id INTEGER)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_copy_error",
			1,
			func(w *csv.Writer, i int) error {
				// Write 2 columns but table only has 1
				return w.Write([]string{"1", "extra"})
			},
			[]string{"id"},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to COPY FROM CSV")
	})

	t.Run("handles large dataset", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_large (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		count := 1000

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_large",
			count,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", i),
					"name",
				})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify all rows were inserted
		var actualCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_upsert_large").Scan(&actualCount)
		require.NoError(t, err)
		require.Equal(t, count, actualCount)
	})

	t.Run("handles upserting large dataset with existing data", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.ExecContext(ctx, `CREATE TABLE test_upsert_large_existing (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		// Insert initial data
		_, err = conn.ExecContext(ctx, `INSERT INTO test_upsert_large_existing SELECT i, 'old' || i FROM generate_series(0, 499) t(i)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		count := 1000

		err = UpsertTableViaCSV(
			ctx,
			log,
			conn,
			"test_upsert_large_existing",
			count,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", i),
					"new",
				})
			},
			[]string{"id"},
		)
		require.NoError(t, err)

		// Verify total count is 1000 (500 updated + 500 new)
		var actualCount int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_upsert_large_existing").Scan(&actualCount)
		require.NoError(t, err)
		require.Equal(t, count, actualCount)

		// Verify some rows were updated
		var name string
		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_large_existing WHERE id = 0").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "new", name)

		// Verify new rows were inserted
		err = conn.QueryRowContext(ctx, "SELECT name FROM test_upsert_large_existing WHERE id = 500").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "new", name)
	})
}
