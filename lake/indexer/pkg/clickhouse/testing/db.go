package clickhousetesting

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
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
	log       *slog.Logger
	cfg       *DBConfig
	addr      string
	httpAddr  string // HTTP endpoint for schema fetching
	container *tcch.ClickHouseContainer
}

// HTTPAddr returns the HTTP endpoint URL (http://host:port) for the ClickHouse container.
func (db *DB) HTTPAddr() string {
	return db.httpAddr
}

// Username returns the ClickHouse username.
func (db *DB) Username() string {
	return db.cfg.Username
}

// Password returns the ClickHouse password.
func (db *DB) Password() string {
	return db.cfg.Password
}

// Addr returns the ClickHouse native protocol address (host:port).
func (db *DB) Addr() string {
	return db.addr
}

// MigrationConfig returns a MigrationConfig for the given database name.
func (db *DB) MigrationConfig(database string) clickhouse.MigrationConfig {
	return clickhouse.MigrationConfig{
		Addr:     db.addr,
		Database: database,
		Username: db.cfg.Username,
		Password: db.cfg.Password,
		Secure:   false,
	}
}

func (db *DB) Close() {
	terminateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.container.Terminate(terminateCtx); err != nil {
		db.log.Error("failed to terminate ClickHouse container", "error", err)
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

// TestClientInfo holds a test client and its database name.
type TestClientInfo struct {
	Client   clickhouse.Client
	Database string
}

// NewTestClientWithInfo creates a test client and returns info including the database name.
func NewTestClientWithInfo(t *testing.T, db *DB) (*TestClientInfo, error) {
	client, dbName, err := newTestClientInternal(t, db)
	if err != nil {
		return nil, err
	}
	return &TestClientInfo{Client: client, Database: dbName}, nil
}

func NewTestClient(t *testing.T, db *DB) (clickhouse.Client, error) {
	client, _, err := newTestClientInternal(t, db)
	return client, err
}

func newTestClientInternal(t *testing.T, db *DB) (clickhouse.Client, string, error) {
	// Create admin client
	// Retry admin client connection/ping up to 3 times for retryable errors
	// ClickHouse may need a moment after container start to be ready for connections
	var adminClient clickhouse.Client
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		adminClient, err = clickhouse.NewClient(t.Context(), db.log, db.addr, db.cfg.Database, db.cfg.Username, db.cfg.Password, false)
		if err != nil {
			if isRetryableConnectionErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
				continue
			}
			return nil, "", fmt.Errorf("failed to create ClickHouse admin client: %w", err)
		}
		break
	}

	// Create test database
	randomSuffix := strings.ReplaceAll(uuid.New().String(), "-", "")
	databaseName := fmt.Sprintf("test_%s", randomSuffix)

	// Create random test database
	adminConn, err := adminClient.Conn(t.Context())
	require.NoError(t, err)
	err = adminConn.Exec(t.Context(), fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", databaseName))
	require.NoError(t, err)
	defer adminConn.Close()

	// Create test client
	// Retry test client connection/ping up to 3 times for retryable errors
	// ClickHouse may need a moment after container start to be ready for connections
	var testClient clickhouse.Client
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		testClient, err = clickhouse.NewClient(t.Context(), db.log, db.addr, databaseName, db.cfg.Username, db.cfg.Password, false)
		if err != nil {
			if isRetryableConnectionErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
				continue
			}
			return nil, "", fmt.Errorf("failed to create ClickHouse client: %w", err)
		}
		break
	}

	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = adminConn.Exec(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", databaseName))
		require.NoError(t, err)
		testClient.Close()
	})

	return testClient, databaseName, nil
}

func NewTestConn(t *testing.T, db *DB) (clickhouse.Connection, error) {
	testClient, err := NewTestClient(t, db)
	require.NoError(t, err)
	conn, err := testClient.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		conn.Close()
	})
	return conn, nil
}

func NewDB(ctx context.Context, log *slog.Logger, cfg *DBConfig) (*DB, error) {
	if cfg == nil {
		cfg = &DBConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate DB config: %w", err)
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
			return nil, fmt.Errorf("failed to start ClickHouse container after retries: %w", lastErr)
		}
		break
	}

	if container == nil {
		return nil, fmt.Errorf("failed to start ClickHouse container after retries: %w", lastErr)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse container host: %w", err)
	}

	port := nat.Port(fmt.Sprintf("%s/tcp", cfg.Port))
	mappedPort, err := container.MappedPort(ctx, port)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse container mapped port: %w", err)
	}

	addr := fmt.Sprintf("%s:%s", host, mappedPort.Port())

	// Get HTTP port for schema fetching
	httpPort := nat.Port("8123/tcp")
	mappedHTTPPort, err := container.MappedPort(ctx, httpPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse container HTTP port: %w", err)
	}
	httpAddr := fmt.Sprintf("http://%s:%s", host, mappedHTTPPort.Port())

	db := &DB{
		log:       log,
		cfg:       cfg,
		addr:      addr,
		httpAddr:  httpAddr,
		container: container,
	}

	return db, nil
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
