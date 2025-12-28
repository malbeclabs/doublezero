package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/dz/telemetry/usage"
	mcpgeoip "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/geoip"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/sol"
)

var (
	Schemas = []*schema.Schema{
		mcpgeoip.Schema,
		dzsvc.Schema,
		dztelemlatency.Schema,
		dztelemusage.Schema,
		sol.Schema,
	}
)

type Indexer struct {
	log *slog.Logger
	cfg Config

	svc          *dzsvc.View
	telemLatency *dztelemlatency.View
	telemUsage   *dztelemusage.View
	sol          *sol.View

	startedAt time.Time
	readyOnce sync.Once
	readyCh   chan struct{}
}

func New(ctx context.Context, cfg Config) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Initialize GeoIP store
	geoIPStore, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: cfg.Logger,
		DB:     cfg.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GeoIP store: %w", err)
	}
	if err := geoIPStore.CreateTablesIfNotExists(); err != nil {
		return nil, fmt.Errorf("failed to create GeoIP tables: %w", err)
	}

	// Initialize serviceability view
	svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
		Logger:            cfg.Logger,
		Clock:             cfg.Clock,
		ServiceabilityRPC: cfg.ServiceabilityRPC,
		RefreshInterval:   cfg.RefreshInterval,
		DB:                cfg.DB,
		GeoIPStore:        geoIPStore,
		GeoIPResolver:     cfg.GeoIPResolver,
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
		DB:                     cfg.DB,
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
		DB:              cfg.DB,
		RefreshInterval: cfg.RefreshInterval,
		GeoIPStore:      *geoIPStore,
		GeoIPResolver:   cfg.GeoIPResolver,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create solana view: %w", err)
	}

	// Initialize telemetry usage view if influx client is configured
	var telemetryUsageView *dztelemusage.View
	if cfg.DeviceUsageInfluxClient != nil {
		telemetryUsageView, err = dztelemusage.NewView(dztelemusage.ViewConfig{
			Logger:          cfg.Logger,
			Clock:           cfg.Clock,
			DB:              cfg.DB,
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
	}
	if err := i.validateSchemas(ctx); err != nil {
		return nil, fmt.Errorf("failed to validate schemas: %w", err)
	}
	return i, nil
}

func (i *Indexer) Schemas() []*schema.Schema {
	schemas := make([]*schema.Schema, 0, len(Schemas))
	for _, schema := range Schemas {
		if schema == dztelemusage.Schema && i.telemUsage == nil {
			continue
		}
		schemas = append(schemas, schema)
	}
	return schemas
}

func (i *Indexer) Ready() bool {
	svcReady := i.svc.Ready()
	telemLatencyReady := i.telemLatency.Ready()
	solReady := i.sol.Ready()
	// NOTE: Don't wait for telemUsage to be ready, it takes too long to refresh from scratch.
	return svcReady && telemLatencyReady && solReady
}

func (i *Indexer) Start(ctx context.Context) {
	i.startedAt = i.cfg.Clock.Now()
	i.svc.Start(ctx)
	i.telemLatency.Start(ctx)
	i.sol.Start(ctx)
	if i.telemUsage != nil {
		i.telemUsage.Start(ctx)
	}

	// Start maintenance tasks if enabled and DB is DuckLake (not plain DuckDB)
	if _, ok := i.cfg.DB.(*duck.Lake); ok {
		if i.cfg.MaintenanceIntervalShort > 0 || i.cfg.MaintenanceIntervalLong > 0 {
			i.startMaintenanceTasks(ctx)
		}
	}
}

func (i *Indexer) Close() error {
	return nil
}
