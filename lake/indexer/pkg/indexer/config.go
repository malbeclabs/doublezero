package indexer

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type Config struct {
	Logger           *slog.Logger
	Clock            clockwork.Clock
	ClickHouse       clickhouse.Client
	MigrationsEnable bool
	MigrationsConfig clickhouse.MigrationConfig

	Neo4jMigrationsEnable bool
	Neo4jMigrationsConfig neo4j.MigrationConfig

	RefreshInterval time.Duration
	MaxConcurrency  int

	// GeoIP configuration.
	GeoIPResolver geoip.Resolver

	// Serviceability RPC configuration.
	ServiceabilityRPC dzsvc.ServiceabilityRPC

	// Telemetry RPC configuration.
	TelemetryRPC           dztelemlatency.TelemetryRPC
	DZEpochRPC             dztelemlatency.EpochRPC
	InternetLatencyAgentPK solana.PublicKey
	InternetDataProviders  []string

	// Device usage configuration.
	DeviceUsageRefreshInterval   time.Duration
	DeviceUsageInfluxClient      dztelemusage.InfluxDBClient
	DeviceUsageInfluxBucket      string
	DeviceUsageInfluxQueryWindow time.Duration
	ReadyIncludesDeviceUsage     bool // If true, the indexer also waits for the device usage view to be ready.

	// Solana configuration.
	SolanaRPC sol.SolanaRPC

	// Neo4j configuration (optional).
	Neo4j neo4j.Client

	// ISIS configuration (optional, requires Neo4j).
	ISISEnabled         bool
	ISISS3Bucket        string        // S3 bucket for IS-IS dumps (default: doublezero-mn-beta-isis-db)
	ISISS3Region        string        // AWS region (default: us-east-1)
	ISISS3EndpointURL   string        // Custom S3 endpoint URL (for testing)
	ISISRefreshInterval time.Duration // Refresh interval for IS-IS sync (default: 30s)

	// SkipReadyWait makes the Ready() method return true immediately without waiting
	// for views to be populated. Useful for preview/dev environments where fast startup
	// is more important than having data immediately available.
	SkipReadyWait bool
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	if c.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	if c.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}

	// Serviceability configuration.
	if c.ServiceabilityRPC == nil {
		return errors.New("serviceability rpc is required")
	}

	// Telemetry configuration.
	if c.TelemetryRPC == nil {
		return errors.New("telemetry rpc is required")
	}
	if c.DZEpochRPC == nil {
		return errors.New("dz epoch rpc is required")
	}
	if c.InternetLatencyAgentPK.IsZero() {
		return errors.New("internet latency agent public key is required")
	}
	if len(c.InternetDataProviders) == 0 {
		return errors.New("internet data providers are required")
	}

	// Solana configuration.
	if c.SolanaRPC == nil {
		return errors.New("solana rpc is required")
	}

	// Device usage configuration.
	// Optional - if client is provided, all other fields must be set.
	if c.DeviceUsageInfluxClient != nil {
		if c.DeviceUsageInfluxBucket == "" {
			return fmt.Errorf("device usage influx bucket is required when influx client is provided")
		}
		if c.DeviceUsageInfluxQueryWindow <= 0 {
			return fmt.Errorf("device usage influx query window must be greater than 0 when influx client is provided")
		}
		if c.DeviceUsageRefreshInterval <= 0 {
			c.DeviceUsageRefreshInterval = c.RefreshInterval
		}
	} else if c.ReadyIncludesDeviceUsage {
		return errors.New("device usage influx client is required when ready includes device usage")
	}

	// ISIS configuration validation.
	// ISIS requires Neo4j to be configured.
	if c.ISISEnabled && c.Neo4j == nil {
		return errors.New("neo4j is required when isis is enabled")
	}

	// Optional with defaults
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	return nil
}
