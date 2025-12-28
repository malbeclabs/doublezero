package indexer

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/telemetry/usage"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type Config struct {
	Logger *slog.Logger
	Clock  clockwork.Clock
	DB     duck.DB

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

	// Maintenance configuration.
	// If set to 0, the maintenance task is disabled.
	MaintenanceIntervalShort time.Duration // Interval for short maintenance tasks: flush_inlined_data, merge_adjacent_files (default: 30 minutes)
	MaintenanceIntervalLong  time.Duration // Interval for long maintenance tasks: rewrite_data_files, merge_adjacent_files, expire_snapshots, cleanup_old_files, delete_orphaned_files (default: 3 hours)
	ExpireSnapshotsOlderThan time.Duration // Age threshold for expiring snapshots (default: 1 day)
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.DB == nil {
		return errors.New("db is required")
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

	// Optional with defaults
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	return nil
}
