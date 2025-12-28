package duck

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"
)

// testDBWithConn creates a test database and connection for testing
func testDBWithConn(t *testing.T) (DB, Connection, error) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use in-memory database for tests
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := NewDB(ctx, dbPath, log)
	if err != nil {
		return nil, nil, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	return db, conn, nil
}

// failingDBConn is a mock connection that fails on all operations
type failingDBConn struct{}

func (f *failingDBConn) DB() DB {
	return &failingDB{}
}

func (f *failingDBConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return nil
}

func (f *failingDBConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("failed to begin transaction")
}

func (f *failingDBConn) Close() error {
	return nil
}

// failingDB is a mock DB that fails on all operations
type failingDB struct{}

func (f *failingDB) Catalog() string {
	return "test"
}

func (f *failingDB) Schema() string {
	return "main"
}

func (f *failingDB) Close() error {
	return nil
}

func (f *failingDB) Conn(ctx context.Context) (Connection, error) {
	return &failingDBConn{}, nil
}

