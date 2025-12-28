package indexer

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
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

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func TestLake_Querier_ValidateSchema(t *testing.T) {
	t.Parallel()

	t.Run("validates schema successfully", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE test_table (id INTEGER, name VARCHAR, value DOUBLE)`)
		require.NoError(t, err)

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "test_table",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
						{Name: "value", Type: "DOUBLE", Description: "Value column"},
					},
				},
			},
		}

		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("validates multiple tables", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE table1 (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE table2 (id INTEGER, value DOUBLE)`)
		require.NoError(t, err)

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "table1",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
					},
				},
				{
					Name: "table2",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
						{Name: "value", Type: "DOUBLE", Description: "Value column"},
					},
				},
			},
		}

		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("handles table with single quotes in name", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		// DuckDB allows table names with quotes, but we'll test with a normal name
		// and verify the escaping logic works
		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE "test'table" (id INTEGER)`)
		require.NoError(t, err)

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "test'table",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
					},
				},
			},
		}

		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("returns error when database query fails", func(t *testing.T) {
		t.Parallel()

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "test_table",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
					},
				},
			},
		}

		err := ValidateSchema(t.Context(), &failingDB{}, schema)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to query schema")
	})
}

func TestLake_Querier_ValidateSchema_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handles empty schema", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		// Empty schema should pass validation (no tables to check)
		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables:      []schematypes.TableInfo{},
		}

		// Empty schema should return early without querying the database
		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("handles table with only one column", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE single_column_table (id INTEGER)`)
		require.NoError(t, err)

		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name: "single_column_table",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "INTEGER", Description: "ID column"},
					},
				},
			},
		}

		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("validates SCD2 tables using _current suffix", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		// Create _current table (SCD2 pattern)
		_, err = conn.ExecContext(t.Context(), `
			CREATE TABLE test_scd2_current (
				as_of_ts TIMESTAMP,
				row_hash VARCHAR,
				id VARCHAR,
				name VARCHAR
			)
		`)
		require.NoError(t, err)

		// Schema defines base table name with (SCD2) in description
		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name:        "test_scd2",
					Description: "Test SCD2 table (SCD2)",
					Columns: []schematypes.ColumnInfo{
						{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
						{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
						{Name: "id", Type: "VARCHAR", Description: "Primary key"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
					},
				},
			},
		}

		err = ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("allows missing SCD2 tables (created on first use)", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		// Don't create the table - it should be OK for SCD2 tables
		// Schema defines base table name with (SCD2) in description
		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name:        "test_scd2_missing",
					Description: "Test SCD2 table that doesn't exist yet (SCD2)",
					Columns: []schematypes.ColumnInfo{
						{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
						{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
						{Name: "id", Type: "VARCHAR", Description: "Primary key"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
					},
				},
			},
		}

		// Should not error - SCD2 tables are created on first use
		err := ValidateSchema(t.Context(), db, schema)
		require.NoError(t, err)
	})

	t.Run("still errors for missing non-SCD2 tables", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		// Schema defines a non-SCD2 table that doesn't exist
		schema := &schematypes.Schema{
			Name:        "test-schema",
			Description: "test description",
			Tables: []schematypes.TableInfo{
				{
					Name:        "test_non_scd2_missing",
					Description: "Test non-SCD2 table that doesn't exist",
					Columns: []schematypes.ColumnInfo{
						{Name: "id", Type: "VARCHAR", Description: "Primary key"},
						{Name: "name", Type: "VARCHAR", Description: "Name column"},
					},
				},
			},
		}

		// Should error - non-SCD2 tables must exist
		err := ValidateSchema(t.Context(), db, schema)
		require.Error(t, err)
		require.Contains(t, err.Error(), "test_non_scd2_missing")
		require.Contains(t, err.Error(), "in-code schema but not in database")
	})
}
