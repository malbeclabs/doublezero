package server

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/telemetry/usage"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultShutdownTimeout   = 5 * time.Second
)

type Config struct {
	Version           string
	ListenAddr        string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration

	Logger            *slog.Logger
	Clock             clockwork.Clock
	ServiceabilityRPC dzsvc.ServiceabilityRPC
	TelemetryRPC      dztelemlatency.TelemetryRPC
	DZEpochRPC        dztelemlatency.EpochRPC
	SolanaRPC         sol.SolanaRPC
	DB                duck.DB

	RefreshInterval        time.Duration
	MaxConcurrency         int
	InternetLatencyAgentPK solana.PublicKey
	InternetDataProviders  []string
	AllowedTokens          []string // Bearer tokens allowed for MCP endpoint authentication

	GeoIPResolver geoip.Resolver

	DeviceUsageRefreshInterval   time.Duration
	DeviceUsageInfluxClient      dztelemusage.InfluxDBClient
	DeviceUsageInfluxBucket      string
	DeviceUsageInfluxQueryWindow time.Duration
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.ServiceabilityRPC == nil {
		return fmt.Errorf("serviceability rpc is required")
	}
	if c.TelemetryRPC == nil {
		return fmt.Errorf("telemetry rpc is required")
	}
	if c.DZEpochRPC == nil {
		return fmt.Errorf("dz epoch rpc is required")
	}
	if c.SolanaRPC == nil {
		return fmt.Errorf("solana rpc is required")
	}
	if c.DB == nil {
		return fmt.Errorf("database is required")
	}
	if c.MaxConcurrency <= 0 {
		return fmt.Errorf("max concurrency must be greater than 0")
	}
	if c.RefreshInterval <= 0 {
		return fmt.Errorf("refresh interval must be greater than 0")
	}
	if c.InternetLatencyAgentPK.IsZero() {
		return fmt.Errorf("internet latency agent public key is required")
	}
	if len(c.InternetDataProviders) == 0 {
		return fmt.Errorf("internet data providers are required")
	}
	// Device usage InfluxDB fields are optional - if client is provided, all fields must be set
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
	}

	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	return nil
}
