package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/indexer/pkg/geoip"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
)

type Indexer struct {
	log *slog.Logger
	cfg Config

	svc          *dzsvc.View
	telemLatency *dztelemlatency.View
	telemUsage   *dztelemusage.View
	sol          *sol.View
	geoip        *mcpgeoip.View

	startedAt time.Time
}

func New(ctx context.Context, cfg Config) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.MigrationsEnable {
		// Run ClickHouse migrations to ensure tables exist
		if err := clickhouse.RunMigrations(ctx, cfg.Logger, cfg.MigrationsConfig); err != nil {
			return nil, fmt.Errorf("failed to run ClickHouse migrations: %w", err)
		}
		cfg.Logger.Info("ClickHouse migrations completed")
	}

	// Initialize GeoIP store
	geoIPStore, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger:     cfg.Logger,
		ClickHouse: cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GeoIP store: %w", err)
	}

	// Initialize serviceability view
	svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
		Logger:            cfg.Logger,
		Clock:             cfg.Clock,
		ServiceabilityRPC: cfg.ServiceabilityRPC,
		RefreshInterval:   cfg.RefreshInterval,
		ClickHouse:        cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create serviceability view: %w", err)
	}

	// Initialize telemetry view
	telemView, err := dztelemlatency.NewView(dztelemlatency.ViewConfig{
		Logger:                 cfg.Logger,
		Clock:                  cfg.Clock,
		TelemetryRPC:           cfg.TelemetryRPC,
		EpochRPC:               cfg.DZEpochRPC,
		MaxConcurrency:         cfg.MaxConcurrency,
		InternetLatencyAgentPK: cfg.InternetLatencyAgentPK,
		InternetDataProviders:  cfg.InternetDataProviders,
		ClickHouse:             cfg.ClickHouse,
		Serviceability:         svcView,
		RefreshInterval:        cfg.RefreshInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry view: %w", err)
	}

	// Initialize solana view
	solanaView, err := sol.NewView(sol.ViewConfig{
		Logger:          cfg.Logger,
		Clock:           cfg.Clock,
		RPC:             cfg.SolanaRPC,
		ClickHouse:      cfg.ClickHouse,
		RefreshInterval: cfg.RefreshInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create solana view: %w", err)
	}

	// Initialize geoip view
	geoipView, err := mcpgeoip.NewView(mcpgeoip.ViewConfig{
		Logger:              cfg.Logger,
		Clock:               cfg.Clock,
		GeoIPStore:          geoIPStore,
		GeoIPResolver:       cfg.GeoIPResolver,
		ServiceabilityStore: svcView.Store(),
		SolanaStore:         solanaView.Store(),
		RefreshInterval:     cfg.RefreshInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create geoip view: %w", err)
	}

	// Initialize telemetry usage view if influx client is configured
	var telemetryUsageView *dztelemusage.View
	if cfg.DeviceUsageInfluxClient != nil {
		telemetryUsageView, err = dztelemusage.NewView(dztelemusage.ViewConfig{
			Logger:          cfg.Logger,
			Clock:           cfg.Clock,
			ClickHouse:      cfg.ClickHouse,
			RefreshInterval: cfg.DeviceUsageRefreshInterval,
			InfluxDB:        cfg.DeviceUsageInfluxClient,
			Bucket:          cfg.DeviceUsageInfluxBucket,
			QueryWindow:     cfg.DeviceUsageInfluxQueryWindow,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create telemetry usage view: %w", err)
		}
	}

	i := &Indexer{
		log: cfg.Logger,
		cfg: cfg,

		svc:          svcView,
		telemLatency: telemView,
		telemUsage:   telemetryUsageView,
		sol:          solanaView,
		geoip:        geoipView,
	}

	return i, nil
}

func (i *Indexer) Ready() bool {
	svcReady := i.svc.Ready()
	telemLatencyReady := i.telemLatency.Ready()
	solReady := i.sol.Ready()
	geoipReady := i.geoip.Ready()
	// Don't wait for telemUsage to be ready, it takes too long to refresh from scratch.
	return svcReady && telemLatencyReady && solReady && geoipReady
}

func (i *Indexer) Start(ctx context.Context) {
	i.startedAt = i.cfg.Clock.Now()
	i.svc.Start(ctx)
	i.telemLatency.Start(ctx)
	i.sol.Start(ctx)
	i.geoip.Start(ctx)
	if i.telemUsage != nil {
		i.telemUsage.Start(ctx)
	}

}

func (i *Indexer) Close() error {
	return nil
}
