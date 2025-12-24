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

func TestReplaceTableViaCSV(t *testing.T) {
	t.Parallel()

	t.Run("replaces table with new data", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table and insert initial data
		_, err = db.Exec(`CREATE TABLE test_replace (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO test_replace VALUES (1, 'Old1'), (2, 'Old2')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []struct {
			id   int
			name string
		}{
			{10, "New1"},
			{20, "New2"},
			{30, "New3"},
		}

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_replace",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", data[i].id),
					data[i].name,
				})
			},
		)
		require.NoError(t, err)

		// Verify old data was replaced
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_replace").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify new data is present
		var name string
		err = db.QueryRow("SELECT name FROM test_replace WHERE id = 10").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "New1", name)

		// Verify old data is gone
		var oldCount int
		err = db.QueryRow("SELECT COUNT(*) FROM test_replace WHERE name = 'Old1'").Scan(&oldCount)
		require.NoError(t, err)
		require.Equal(t, 0, oldCount)
	})

	t.Run("truncates table when count is zero", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table and insert data
		_, err = db.Exec(`CREATE TABLE test_replace_empty (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO test_replace_empty VALUES (1, 'Data1'), (2, 'Data2')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_replace_empty",
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify table was truncated
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_replace_empty").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("handles context cancellation during CSV write", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_replace_cancel (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			ctx,
			log,
			db,
			"test_replace_cancel",
			5,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("handles context cancellation before transaction", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_replace_cancel2 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		// Cancel after a short delay to allow CSV writing to start
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err = ReplaceTableViaCSV(
			ctx,
			log,
			db,
			"test_replace_cancel2",
			100, // Large enough to take some time
			func(w *csv.Writer, i int) error {
				time.Sleep(1 * time.Millisecond) // Small delay to allow cancellation
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("handles context cancellation for empty count", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_replace_cancel_empty (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			ctx,
			log,
			db,
			"test_replace_cancel_empty",
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("handles CSV write error", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_replace_error (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_replace_error",
			2,
			func(w *csv.Writer, i int) error {
				if i == 1 {
					return errors.New("write error")
				}
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to write CSV record")
	})

	t.Run("handles database transaction error", func(t *testing.T) {
		t.Parallel()

		db := &failingDB{}
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err := ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_table",
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to begin transaction")
	})

	t.Run("handles TRUNCATE error", func(t *testing.T) {
		t.Parallel()

		// Use a table that doesn't exist to cause TRUNCATE error
		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"nonexistent_table",
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to truncate")
	})

	t.Run("handles COPY FROM error", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create table with different schema to cause COPY error
		_, err = db.Exec(`CREATE TABLE test_replace_copy_error (id INTEGER)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_replace_copy_error",
			1,
			func(w *csv.Writer, i int) error {
				// Write 2 columns but table only has 1
				return w.Write([]string{"1", "extra"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to COPY FROM CSV")
	})

	t.Run("handles large dataset", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_replace_large (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		count := 1000

		err = ReplaceTableViaCSV(
			context.Background(),
			log,
			db,
			"test_replace_large",
			count,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", i),
					"name",
				})
			},
		)
		require.NoError(t, err)

		// Verify all rows were inserted
		var actualCount int
		err = db.QueryRow("SELECT COUNT(*) FROM test_replace_large").Scan(&actualCount)
		require.NoError(t, err)
		require.Equal(t, count, actualCount)
	})
}

