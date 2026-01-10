package clickhousetesting

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
	"github.com/stretchr/testify/require"
	tcch "github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

type DBConfig struct {
	Database       string
	Username       string
	Password       string
	Port           string
	ContainerImage string
}

type DB struct {
	clickhouse.DB
	container *tcch.ClickHouseContainer
	t         testing.TB
}

func (db *DB) Close() {
	if err := db.DB.Close(); err != nil {
		db.t.Logf("failed to close ClickHouse: %v", err)
	}
}

func (cfg *DBConfig) Validate() error {
	if cfg.Database == "" {
		cfg.Database = "test"
	}
	if cfg.Username == "" {
		cfg.Username = "default"
	}
	if cfg.Password == "" {
		cfg.Password = "password"
	}
	if cfg.Port == "" {
		cfg.Port = "9000"
	}
	if cfg.ContainerImage == "" {
		cfg.ContainerImage = "clickhouse/clickhouse-server:latest"
	}
	return nil
}

func NewDefaultDB(t testing.TB) *DB {
	return NewDB(t, nil)
}

func NewDB(t testing.TB, cfg *DBConfig) *DB {
	ctx := t.Context()

	if cfg == nil {
		cfg = &DBConfig{}
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("failed to validate DB config: %v", err)
	}

	// Retry container start up to 3 times for retryable errors
	var container *tcch.ClickHouseContainer
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		container, err = tcch.Run(ctx,
			cfg.ContainerImage,
			tcch.WithDatabase(cfg.Database),
			tcch.WithUsername(cfg.Username),
			tcch.WithPassword(cfg.Password),
		)
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
				continue
			}
			require.NoError(t, err)
		}
		break
	}

	if container == nil {
		t.Fatalf("failed to start ClickHouse container after retries: %v", lastErr)
	}

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port := nat.Port(fmt.Sprintf("%s/tcp", cfg.Port))
	mappedPort, err := container.MappedPort(ctx, port)
	require.NoError(t, err)

	addr := fmt.Sprintf("%s:%s", host, mappedPort.Port())

	log := slog.Default()
	// Retry client connection/ping up to 3 times for retryable errors
	// ClickHouse may need a moment after container start to be ready for connections
	var chDB clickhouse.DB
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		chDB, err = clickhouse.NewClient(ctx, log, addr, cfg.Database, cfg.Username, cfg.Password)
		if err != nil {
			if isRetryableConnectionErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
				continue
			}
			_ = container.Terminate(ctx)
			require.NoError(t, err)
		}
		break
	}

	db := &DB{
		DB:        chDB,
		container: container,
		t:         t,
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func isRetryableContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http://%2Fvar%2Frun%2Fdocker.sock")
}

func isRetryableConnectionErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "handshake") ||
		strings.Contains(s, "unexpected packet") ||
		strings.Contains(s, "[handshake]") ||
		strings.Contains(s, "packet") ||
		strings.Contains(s, "failed to ping") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "dial tcp")
}

func (db *DB) Conn() clickhouse.Connection {
	conn, err := db.DB.Conn(db.t.Context())
	require.NoError(db.t, err, "failed to get ClickHouse connection")
	return conn
}
