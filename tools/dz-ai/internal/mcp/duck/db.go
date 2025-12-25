package duck

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
)

type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Begin() (*sql.Tx, error)
	Close() error
}

type duckDB struct {
	dbPath  string
	log     *slog.Logger
	mu      sync.RWMutex // protects db connection during recovery
	writeMu sync.Mutex   // serializes all write operations
	db      *sql.DB
}

func NewDB(dbPath string, log *slog.Logger) (*duckDB, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for in-memory DuckDB
	// For in-memory databases, we typically only need 1 connection
	// but allow a small pool for concurrent reads
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(0) // Connections don't expire

	return &duckDB{
		dbPath: dbPath,
		log:    log,
		db:     db,
	}, nil
}

func isInvalidationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "database has been invalidated") ||
		strings.Contains(errStr, "FATAL Error") ||
		strings.Contains(errStr, "must be restarted")
}

func (r *duckDB) recover() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Warn("recoverable_db: database invalidated, attempting recovery")

	if r.db != nil {
		r.db.Close()
		r.db = nil
	}

	db, err := sql.Open("duckdb", r.dbPath)
	if err != nil {
		return fmt.Errorf("failed to reopen database: %w", err)
	}

	r.db = db
	r.log.Info("recoverable_db: database connection recovered successfully")
	return nil
}

func (r *duckDB) Exec(query string, args ...any) (sql.Result, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	r.mu.RLock()
	db := r.db
	r.mu.RUnlock()

	result, err := db.Exec(query, args...)
	if err != nil && isInvalidationError(err) {
		if recoverErr := r.recover(); recoverErr != nil {
			return nil, fmt.Errorf("failed to recover database: %w (original error: %w)", recoverErr, err)
		}
		r.mu.RLock()
		db = r.db
		r.mu.RUnlock()
		result, err = db.Exec(query, args...)
	}
	return result, err
}

func (r *duckDB) Query(query string, args ...any) (*sql.Rows, error) {
	r.mu.RLock()
	db := r.db
	r.mu.RUnlock()

	rows, err := db.Query(query, args...)
	if err != nil && isInvalidationError(err) {
		if recoverErr := r.recover(); recoverErr != nil {
			return nil, fmt.Errorf("failed to recover database: %w (original error: %w)", recoverErr, err)
		}
		r.mu.RLock()
		db = r.db
		r.mu.RUnlock()
		rows, err = db.Query(query, args...)
	}
	return rows, err
}

func (r *duckDB) QueryRow(query string, args ...any) *sql.Row {
	r.mu.RLock()
	db := r.db
	r.mu.RUnlock()

	row := db.QueryRow(query, args...)
	// Note: QueryRow doesn't return an error immediately, so we can't check for invalidation here
	// The error will be returned when Scan is called
	return row
}

func (r *duckDB) Begin() (*sql.Tx, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	r.mu.RLock()
	db := r.db
	r.mu.RUnlock()

	tx, err := db.Begin()
	if err != nil && isInvalidationError(err) {
		if recoverErr := r.recover(); recoverErr != nil {
			return nil, fmt.Errorf("failed to recover database: %w (original error: %w)", recoverErr, err)
		}
		r.mu.RLock()
		db = r.db
		r.mu.RUnlock()
		tx, err = db.Begin()
	}
	return tx, err
}

func (r *duckDB) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}
