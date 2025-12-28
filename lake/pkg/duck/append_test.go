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

func testDBWithConn(t *testing.T) (*duckDB, Connection, error) {
	db, err := NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		return nil, nil, err
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, nil, err
	}
	return db, conn, nil
}

func TestLake_Duck_AppendTableViaCSV(t *testing.T) {
	t.Parallel()

	t.Run("appends rows to empty table", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create test table
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append (id INTEGER, name VARCHAR)`)
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
			conn,
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
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_append").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify specific row
		var name string
		err = conn.QueryRowContext(context.Background(), "SELECT name FROM test_append WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)
	})

	t.Run("appends rows to existing table", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create test table and insert initial data
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append2 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)
		_, err = conn.ExecContext(context.Background(), `INSERT INTO test_append2 VALUES (1, 'Initial')`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		data := []string{"Second", "Third"}

		err = AppendTableViaCSV(
			context.Background(),
			log,
			conn,
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
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_append2").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create test table
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_empty (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			conn,
			"test_append_empty",
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify no rows were inserted
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_append_empty").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	t.Run("handles context cancellation during CSV write", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_cancel (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			ctx,
			log,
			conn,
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

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_cancel2 (id INTEGER, name VARCHAR)`)
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
			conn,
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

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_error (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			conn,
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

		conn := &failingDBConn{}
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err := AppendTableViaCSV(
			context.Background(),
			log,
			conn,
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

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		// Create table with different schema to cause COPY error
		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_copy_error (id INTEGER)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		err = AppendTableViaCSV(
			context.Background(),
			log,
			conn,
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

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		_, err = conn.ExecContext(context.Background(), `CREATE TABLE test_append_large (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		count := 1000

		err = AppendTableViaCSV(
			context.Background(),
			log,
			conn,
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
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_append_large").Scan(&actualCount)
		require.NoError(t, err)
		require.Equal(t, count, actualCount)
	})
}

type failingDB struct{}

func (f *failingDB) Catalog() string {
	return "main"
}

func (f *failingDB) Close() error {
	return nil
}

func (f *failingDB) Schema() string {
	return "default"
}

func (f *failingDB) Conn(ctx context.Context) (Connection, error) {
	return &failingDBConn{db: f}, nil
}

type failingDBConn struct {
	db *failingDB
}

func (f *failingDBConn) DB() DB {
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
