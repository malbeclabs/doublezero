package migrations

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/pressly/goose/v3"
)

// RunMigrations applies pending goose migrations against ClickHouse.
func RunMigrations(addr, database, username, password string, secure bool, log *slog.Logger) error {
	db, err := NewDB(addr, database, username, password, secure)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectClickHouse, db, FS, goose.WithSlog(log))
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// NewDB opens a ClickHouse database connection for use in tests or custom migration scenarios.
func NewDB(addr, database, username, password string, secure bool) (*sql.DB, error) {
	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
	}
	if secure {
		opts.TLS = &tls.Config{}
	}
	db := clickhouse.OpenDB(opts)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return db, nil
}
