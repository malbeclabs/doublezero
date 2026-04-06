package migrations

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/pressly/goose/v3"
)

// RunMigrations applies pending goose migrations against ClickHouse.
func RunMigrations(addr, database, username, password string, secure bool, log *slog.Logger) error {
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
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("migration ping: %w", err)
	}

	goose.SetBaseFS(FS)
	goose.SetLogger(&gooseLogger{log: log})
	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// gooseLogger adapts slog to goose's Printf-style logger interface.
type gooseLogger struct {
	log *slog.Logger
}

func (g *gooseLogger) Fatalf(format string, v ...any) {
	g.log.Error(fmt.Sprintf(format, v...))
}

func (g *gooseLogger) Printf(format string, v ...any) {
	g.log.Info(fmt.Sprintf(format, v...))
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
