package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dzgraph "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/graph"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/isis"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/indexer/pkg/geoip"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
)

type Indexer struct {
	log *slog.Logger
	cfg Config

	svc          *dzsvc.View
	graphStore   *dzgraph.Store
	telemLatency *dztelemlatency.View
	telemUsage   *dztelemusage.View
	sol          *sol.View
	geoip        *mcpgeoip.View
	isisSource   isis.Source

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

	if cfg.Neo4jMigrationsEnable && cfg.Neo4j != nil {
		if err := neo4j.RunMigrations(ctx, cfg.Logger, cfg.Neo4jMigrationsConfig); err != nil {
			return nil, fmt.Errorf("failed to run Neo4j migrations: %w", err)
		}
		cfg.Logger.Info("Neo4j migrations completed")
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

	// Initialize graph store if Neo4j is configured
	var graphStore *dzgraph.Store
	if cfg.Neo4j != nil {
		graphStore, err = dzgraph.NewStore(dzgraph.StoreConfig{
			Logger:     cfg.Logger,
			Neo4j:      cfg.Neo4j,
			ClickHouse: cfg.ClickHouse,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create graph store: %w", err)
		}
		cfg.Logger.Info("Neo4j graph store initialized")
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

	// Initialize ISIS source if enabled
	var isisSource isis.Source
	if cfg.ISISEnabled {
		isisSource, err = isis.NewS3Source(ctx, isis.S3SourceConfig{
			Bucket:      cfg.ISISS3Bucket,
			Region:      cfg.ISISS3Region,
			EndpointURL: cfg.ISISS3EndpointURL,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create ISIS S3 source: %w", err)
		}
		cfg.Logger.Info("ISIS S3 source initialized",
			"bucket", cfg.ISISS3Bucket,
			"region", cfg.ISISS3Region)
	}

	i := &Indexer{
		log: cfg.Logger,
		cfg: cfg,

		svc:          svcView,
		graphStore:   graphStore,
		telemLatency: telemView,
		telemUsage:   telemetryUsageView,
		sol:          solanaView,
		geoip:        geoipView,
		isisSource:   isisSource,
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

	// Start graph sync loop if Neo4j is configured
	if i.graphStore != nil {
		go i.startGraphSync(ctx)
	}

	// Start ISIS sync loop if enabled
	if i.isisSource != nil {
		go i.startISISSync(ctx)
	}
}

// startGraphSync runs the graph sync loop.
// It waits for the serviceability view to be ready, then syncs the graph periodically.
// When ISIS is enabled, it fetches ISIS data first and syncs everything atomically
// to ensure there is never a moment where the graph has nodes but no ISIS relationships.
func (i *Indexer) startGraphSync(ctx context.Context) {
	i.log.Info("graph_sync: waiting for serviceability view to be ready")

	// Wait for serviceability to be ready before first sync
	if err := i.svc.WaitReady(ctx); err != nil {
		i.log.Error("graph_sync: failed to wait for serviceability view", "error", err)
		return
	}

	// Initial sync
	i.log.Info("graph_sync: starting initial sync")
	if err := i.doGraphSync(ctx); err != nil {
		i.log.Error("graph_sync: initial sync failed", "error", err)
	} else {
		i.log.Info("graph_sync: initial sync completed")
	}

	// Periodic sync
	ticker := i.cfg.Clock.NewTicker(i.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			if err := i.doGraphSync(ctx); err != nil {
				i.log.Error("graph_sync: sync failed", "error", err)
			}
		}
	}
}

// doGraphSync performs a single graph sync operation.
// If ISIS is enabled, it fetches ISIS data and syncs atomically with the graph.
// Otherwise, it syncs just the base graph.
func (i *Indexer) doGraphSync(ctx context.Context) error {
	if i.isisSource != nil {
		// Fetch ISIS data first, then sync everything atomically
		lsps, err := i.fetchISISData(ctx)
		if err != nil {
			i.log.Warn("graph_sync: failed to fetch ISIS data, syncing without ISIS", "error", err)
			// Fall back to sync without ISIS data
			return i.graphStore.Sync(ctx)
		}
		return i.graphStore.SyncWithISIS(ctx, lsps)
	}
	// No ISIS source configured, just sync the base graph
	return i.graphStore.Sync(ctx)
}

// fetchISISData fetches and parses ISIS data from the source.
func (i *Indexer) fetchISISData(ctx context.Context) ([]isis.LSP, error) {
	dump, err := i.isisSource.FetchLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ISIS dump: %w", err)
	}

	lsps, err := isis.Parse(dump.RawJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ISIS dump: %w", err)
	}

	return lsps, nil
}

func (i *Indexer) Close() error {
	if i.isisSource != nil {
		if err := i.isisSource.Close(); err != nil {
			i.log.Warn("failed to close ISIS source", "error", err)
		}
	}
	return nil
}

// startISISSync runs the ISIS sync loop for picking up topology changes between graph syncs.
// The initial ISIS sync is handled atomically by startGraphSync, so this loop only handles
// periodic updates to catch IS-IS topology changes that occur between full graph syncs.
func (i *Indexer) startISISSync(ctx context.Context) {
	i.log.Info("isis_sync: waiting for serviceability view to be ready")

	// Wait for serviceability to be ready
	if err := i.svc.WaitReady(ctx); err != nil {
		i.log.Error("isis_sync: failed to wait for serviceability view", "error", err)
		return
	}

	// Determine refresh interval
	refreshInterval := i.cfg.ISISRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = 30 * time.Second
	}

	// Periodic sync only - initial sync is handled atomically by graph sync
	ticker := i.cfg.Clock.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			if err := i.doISISSync(ctx); err != nil {
				i.log.Error("isis_sync: sync failed", "error", err)
			}
		}
	}
}

// doISISSync performs a single IS-IS sync operation.
func (i *Indexer) doISISSync(ctx context.Context) error {
	i.log.Debug("isis_sync: fetching latest dump")

	// Fetch the latest IS-IS dump from S3
	dump, err := i.isisSource.FetchLatest(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch ISIS dump: %w", err)
	}

	i.log.Debug("isis_sync: parsing dump", "file", dump.FileName, "size", len(dump.RawJSON))

	// Parse the dump
	lsps, err := isis.Parse(dump.RawJSON)
	if err != nil {
		return fmt.Errorf("failed to parse ISIS dump: %w", err)
	}

	i.log.Debug("isis_sync: syncing to Neo4j", "lsps", len(lsps))

	// Sync to Neo4j
	if err := i.graphStore.SyncISIS(ctx, lsps); err != nil {
		return fmt.Errorf("failed to sync ISIS to graph: %w", err)
	}

	return nil
}

// GraphStore returns the Neo4j graph store, or nil if Neo4j is not configured.
func (i *Indexer) GraphStore() *dzgraph.Store {
	return i.graphStore
}
