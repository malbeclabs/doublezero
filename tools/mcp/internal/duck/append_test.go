package duck

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAppendTableViaCSV(t *testing.T) {
	t.Parallel()

	t.Run("appends rows to empty table", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table
		_, err = db.Exec(`CREATE TABLE test_append (id INTEGER, name VARCHAR)`)
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

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", data[i].id),
					data[i].name,
				})
			},
		)
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_append").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify specific row
		var name string
		err = db.QueryRow("SELECT name FROM test_append WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)
	})

	t.Run("appends rows to existing table", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table and insert initial data
		_, err = db.Exec(`CREATE TABLE test_append2 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO test_append2 VALUES (1, 'Initial')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []string{"Second", "Third"}

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append2",
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{
					fmt.Sprintf("%d", i+2),
					data[i],
				})
			},
		)
		require.NoError(t, err)

		// Verify all rows are present
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_append2").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create test table
		_, err = db.Exec(`CREATE TABLE test_append_empty (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append_empty",
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify no rows were inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_append_empty").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("handles context cancellation during CSV write", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		_, err = db.Exec(`CREATE TABLE test_append_cancel (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			ctx,
			log,
			db,
			"test_append_cancel",
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

		_, err = db.Exec(`CREATE TABLE test_append_cancel2 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		// Cancel after a short delay to allow CSV writing to start
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err = AppendTableViaCSV(
			ctx,
			log,
			db,
			"test_append_cancel2",
			100, // Large enough to take some time
			func(w *csv.Writer, i int) error {
				time.Sleep(1 * time.Millisecond) // Small delay to allow cancellation
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

		_, err = db.Exec(`CREATE TABLE test_append_error (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append_error",
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

		err := AppendTableViaCSV(
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

	t.Run("handles COPY FROM error", func(t *testing.T) {
		t.Parallel()

		db, err := NewDB("", slog.New(slog.NewTextHandler(os.Stderr, nil)))
		require.NoError(t, err)
		defer db.Close()

		// Create table with different schema to cause COPY error
		_, err = db.Exec(`CREATE TABLE test_append_copy_error (id INTEGER)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append_copy_error",
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

		_, err = db.Exec(`CREATE TABLE test_append_large (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		count := 1000

		err = AppendTableViaCSV(
			context.Background(),
			log,
			db,
			"test_append_large",
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
		err = db.QueryRow("SELECT COUNT(*) FROM test_append_large").Scan(&actualCount)
		require.NoError(t, err)
		require.Equal(t, count, actualCount)
	})
}

type failingDB struct{}

func (f *failingDB) Exec(query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) Query(query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) QueryRow(query string, args ...any) *sql.Row {
	return &sql.Row{}
}

func (f *failingDB) Begin() (*sql.Tx, error) {
	return nil, errors.New("database error")
}

func (f *failingDB) Close() error {
	return nil
}

