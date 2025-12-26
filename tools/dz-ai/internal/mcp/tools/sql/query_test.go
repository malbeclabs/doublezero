package sqltools

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
func TestAI_MCP_SQLTools_QueryTool_NewQueryTool(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			tool, err := NewQueryTool(QueryToolConfig{
				DB:          &failingDB{},
				Name:        "test",
				Description: "test description",
			})
			require.Error(t, err)
			require.Nil(t, tool)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing database", func(t *testing.T) {
			t.Parallel()
			tool, err := NewQueryTool(QueryToolConfig{
				Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
				Name:        "test",
				Description: "test description",
			})
			require.Error(t, err)
			require.Nil(t, tool)
			require.Contains(t, err.Error(), "database is required")
		})

		t.Run("missing name", func(t *testing.T) {
			t.Parallel()
			tool, err := NewQueryTool(QueryToolConfig{
				Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
				DB:          &failingDB{},
				Description: "test description",
			})
			require.Error(t, err)
			require.Nil(t, tool)
			require.Contains(t, err.Error(), "name is required")
		})

		t.Run("missing description", func(t *testing.T) {
			t.Parallel()
			tool, err := NewQueryTool(QueryToolConfig{
				Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
				DB:     &failingDB{},
				Name:   "test",
			})
			require.Error(t, err)
			require.Nil(t, tool)
			require.Contains(t, err.Error(), "description is required")
		})
	})

	t.Run("creates tool successfully", func(t *testing.T) {
		t.Parallel()

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          &failingDB{},
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)
		require.NotNil(t, tool)
		require.Equal(t, "test-query", tool.cfg.Name)
		require.Equal(t, "test description", tool.cfg.Description)
	})
}

func TestAI_MCP_SQLTools_QueryTool_Register(t *testing.T) {
	t.Parallel()

	t.Run("registers tool successfully", func(t *testing.T) {
		t.Parallel()

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          &failingDB{},
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		server := mcp.NewServer(&mcp.Implementation{
			Name:    "Test Server",
			Version: "1.0.0",
		}, nil)

		err = tool.Register(server)
		require.NoError(t, err)
	})
}

func TestAI_MCP_SQLTools_QueryTool_HandleQuery(t *testing.T) {
	t.Parallel()

	t.Run("executes query successfully", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE test_table (id INTEGER, name VARCHAR, value DOUBLE)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(t.Context(), `INSERT INTO test_table VALUES (1, 'test1', 10.5), (2, 'test2', 20.3)`)
		require.NoError(t, err)

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          db,
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		result, err := tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT id, name, value FROM test_table ORDER BY id",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"id", "name", "value"}, result.Columns)
		require.Len(t, result.Rows, 2)
		require.Equal(t, 2, result.Count)

		// Check first row
		require.Equal(t, int32(1), result.Rows[0]["id"])
		require.Equal(t, "test1", result.Rows[0]["name"])
		require.InDelta(t, 10.5, result.Rows[0]["value"], 0.01)

		// Check second row
		require.Equal(t, int32(2), result.Rows[1]["id"])
		require.Equal(t, "test2", result.Rows[1]["name"])
		require.InDelta(t, 20.3, result.Rows[1]["value"], 0.01)
	})

	t.Run("handles empty result set", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE empty_table (id INTEGER)`)
		require.NoError(t, err)

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          db,
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		result, err := tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT id FROM empty_table",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"id"}, result.Columns)
		require.Len(t, result.Rows, 0)
		require.Equal(t, 0, result.Count)
	})

	t.Run("handles NULL values", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE null_table (id INTEGER, name VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(t.Context(), `INSERT INTO null_table VALUES (1, NULL), (NULL, 'test')`)
		require.NoError(t, err)

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          db,
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		result, err := tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT id, name FROM null_table ORDER BY id NULLS LAST",
		})
		require.NoError(t, err)
		require.Len(t, result.Rows, 2)
		require.Nil(t, result.Rows[0]["name"])
		require.Nil(t, result.Rows[1]["id"])
	})

	t.Run("converts byte arrays to strings", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.ExecContext(t.Context(), `CREATE TABLE byte_table (data VARCHAR)`)
		require.NoError(t, err)

		_, err = conn.ExecContext(t.Context(), `INSERT INTO byte_table VALUES ('test data')`)
		require.NoError(t, err)

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          db,
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		result, err := tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT data FROM byte_table",
		})
		require.NoError(t, err)
		require.Len(t, result.Rows, 1)
		require.IsType(t, "", result.Rows[0]["data"])
		require.Equal(t, "test data", result.Rows[0]["data"])
	})

	t.Run("returns error on invalid SQL", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          db,
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		_, err = tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT * FROM nonexistent_table",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to execute query")
	})

	t.Run("returns error on database error", func(t *testing.T) {
		t.Parallel()

		tool, err := NewQueryTool(QueryToolConfig{
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:          &failingDB{},
			Name:        "test-query",
			Description: "test description",
		})
		require.NoError(t, err)

		_, err = tool.handleQuery(t.Context(), QueryInput{
			SQL: "SELECT 1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to execute query")
	})
}
