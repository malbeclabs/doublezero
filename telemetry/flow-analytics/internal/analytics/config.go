package analytics

import (
	"errors"
	"os"
)

const (
	defaultClickHouseAddr     = "localhost:9000"
	defaultClickHouseDatabase = "default"
	defaultClickHouseUser     = "default"
	defaultTableName          = "default.flows_testnet"
	defaultPort               = "8080"
)

// Config holds the application configuration.
type Config struct {
	ClickHouseAddr     string
	ClickHouseDatabase string
	ClickHouseUser     string
	ClickHousePassword string
	ClickHouseSecure   bool
	TableName          string
	Port               string
}

// ConfigFromEnv creates a Config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		ClickHouseAddr:     getEnvOrDefault("CLICKHOUSE_ADDR", defaultClickHouseAddr),
		ClickHouseDatabase: getEnvOrDefault("CLICKHOUSE_DATABASE", defaultClickHouseDatabase),
		ClickHouseUser:     getEnvOrDefault("CLICKHOUSE_USER", defaultClickHouseUser),
		ClickHousePassword: os.Getenv("CLICKHOUSE_PASS"),
		ClickHouseSecure:   os.Getenv("CLICKHOUSE_SECURE") == "true",
		TableName:          getEnvOrDefault("FLOWS_TABLE", defaultTableName),
		Port:               getEnvOrDefault("PORT", defaultPort),
	}
}

// Validate checks that required configuration is present.
func (c *Config) Validate() error {
	if c.ClickHouseAddr == "" {
		return errors.New("CLICKHOUSE_ADDR is required")
	}
	if c.TableName == "" {
		return errors.New("FLOWS_TABLE is required")
	}
	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
