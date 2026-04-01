package geoprobe

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/db/clickhouse/migrations"
	"github.com/pressly/goose/v3"
)

func RunMigrations(cfg ClickhouseConfig, log *slog.Logger) error {
	opts := &clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
	}
	if cfg.Secure {
		opts.TLS = &tls.Config{}
	}
	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("migration ping: %w", err)
	}

	return runGoose(db, log)
}

type gooseLogger struct {
	log *slog.Logger
}

func (g *gooseLogger) Fatalf(format string, v ...interface{}) {
	g.log.Error(fmt.Sprintf(format, v...))
}

func (g *gooseLogger) Printf(format string, v ...interface{}) {
	g.log.Info(fmt.Sprintf(format, v...))
}

func runGoose(db *sql.DB, log *slog.Logger) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(&gooseLogger{log: log})
	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
