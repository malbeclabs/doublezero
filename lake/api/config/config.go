package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// DB is the global ClickHouse connection pool
var DB driver.Conn

// Config holds the ClickHouse configuration
type CHConfig struct {
	Addr     string
	Database string
	Username string
	Password string
}

// cfg holds the parsed configuration
var cfg CHConfig

// Database returns the configured database name
func Database() string {
	return cfg.Database
}

// Load initializes configuration from environment variables and creates the connection pool
func Load() error {
	cfg.Addr = os.Getenv("CLICKHOUSE_ADDR_TCP")
	if cfg.Addr == "" {
		cfg.Addr = "localhost:9000"
	}

	cfg.Database = os.Getenv("CLICKHOUSE_DATABASE")
	if cfg.Database == "" {
		cfg.Database = "default"
	}

	cfg.Username = os.Getenv("CLICKHOUSE_USERNAME")
	if cfg.Username == "" {
		cfg.Username = "default"
	}

	cfg.Password = os.Getenv("CLICKHOUSE_PASSWORD")

	log.Printf("Connecting to ClickHouse: addr=%s, database=%s, username=%s", cfg.Addr, cfg.Database, cfg.Username)

	// Create connection pool
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:     5 * time.Second,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		return fmt.Errorf("failed to create clickhouse connection: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	DB = conn
	log.Printf("Connected to ClickHouse successfully")

	return nil
}

// Close closes the ClickHouse connection pool
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
