package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/config"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer"
	dztelemusage "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/usage"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/metrics"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/server"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/metrodb"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
	"github.com/oschwald/geoip2-golang"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultListenAddr                   = "0.0.0.0:3010"
	defaultRefreshInterval              = 60 * time.Second
	defaultMaxConcurrency               = 64
	defaultDZEnv                        = config.EnvMainnetBeta
	defaultMetricsAddr                  = "0.0.0.0:0"
	defaultGeoipCityDBPath              = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	defaultGeoipASNDBPath               = "/usr/share/GeoIP/GeoLite2-ASN.mmdb"
	defaultDeviceUsageInfluxQueryWindow = 1 * time.Hour
	defaultDeviceUsageRefreshInterval   = 5 * time.Minute
	defaultMaintenanceIntervalShort     = 30 * time.Minute
	defaultMaintenanceIntervalLong      = 24 * time.Hour
	defaultMaintenanceExpireOlderThan   = 24 * time.Hour

	geoipCityDBPathEnvVar = "GEOIP_CITY_DB_PATH"
	geoipASNDBPathEnvVar  = "GEOIP_ASN_DB_PATH"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	enablePprofFlag := flag.Bool("enable-pprof", false, "enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	listenAddrFlag := flag.String("listen-addr", defaultListenAddr, "HTTP server listen address")

	// Database configuration
	duckLakeCatalogNameFlag := flag.String("ducklake-catalog-name", "dzlake", "Name of the DuckLake catalog (or set DUCKLAKE_CATALOG_NAME env var)")
	duckLakeCatalogURIFlag := flag.String("ducklake-catalog-uri", "file://.tmp/lake/catalog.sqlite", "URI to the DuckLake catalog (or set DUCKLAKE_CATALOG_URI env var)")
	duckLakeStorageURIFlag := flag.String("ducklake-storage-uri", "file://.tmp/lake/data", "URI to the DuckLake storage directory (or set DUCKLAKE_STORAGE_URI env var)")

	// GeoIP configuration
	geoipCityDBPathFlag := flag.String("geoip-city-db-path", defaultGeoipCityDBPath, "Path to MaxMind GeoIP2 City database file (or set MCP_GEOIP_CITY_DB_PATH env var)")
	geoipASNDBPathFlag := flag.String("geoip-asn-db-path", defaultGeoipASNDBPath, "Path to MaxMind GeoIP2 ASN database file (or set MCP_GEOIP_ASN_DB_PATH env var)")

	// Indexer configuration
	dzEnvFlag := flag.String("env", defaultDZEnv, "doublezero environment (devnet, testnet, mainnet-beta)")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment (devnet, testnet, mainnet-beta)")
	refreshIntervalFlag := flag.Duration("cache-ttl", defaultRefreshInterval, "cache TTL duration")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent operations")
	deviceUsageQueryWindowFlag := flag.Duration("device-usage-query-window", defaultDeviceUsageInfluxQueryWindow, "Query window for device usage (default: 1 hour)")
	deviceUsageRefreshIntervalFlag := flag.Duration("device-usage-refresh-interval", defaultDeviceUsageRefreshInterval, "Refresh interval for device usage (default: 5 minutes)")

	// Maintenance configuration
	maintenanceIntervalShortFlag := flag.Duration("maintenance-interval-short", defaultMaintenanceIntervalShort, "Interval for short maintenance tasks: flush_inlined_data, merge_adjacent_files (default: 30 minutes, set to 0 to disable)")
	maintenanceIntervalLongFlag := flag.Duration("maintenance-interval-long", defaultMaintenanceIntervalLong, "Interval for long maintenance tasks: rewrite_data_files, merge_adjacent_files, expire_snapshots, cleanup_old_files, delete_orphaned_files (default: 3 hours, set to 0 to disable)")
	maintenanceExpireOlderThanFlag := flag.Duration("maintenance-expire-older-than", defaultMaintenanceExpireOlderThan, "Age threshold for expiring snapshots (default: 7 days, set to 0 to disable)")

	flag.Parse()

	// Override flags with environment variables if set
	if envCatalogURI := os.Getenv("DUCKLAKE_CATALOG_URI"); envCatalogURI != "" {
		*duckLakeCatalogURIFlag = envCatalogURI
	}
	if envStorageURI := os.Getenv("DUCKLAKE_STORAGE_URI"); envStorageURI != "" {
		*duckLakeStorageURIFlag = envStorageURI
	}
	if envCatalogName := os.Getenv("DUCKLAKE_CATALOG_NAME"); envCatalogName != "" {
		*duckLakeCatalogNameFlag = envCatalogName
	}

	networkConfig, err := config.NetworkConfigForEnv(*dzEnvFlag)
	if err != nil {
		return fmt.Errorf("failed to get network config: %w", err)
	}

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnvFlag)
	if err != nil {
		return fmt.Errorf("failed to get solana network config: %w", err)
	}

	log := logger.New(*verboseFlag)

	// Set up signal handling with detailed logging
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Log which signal was received
	go func() {
		sig := <-sigCh
		log.Info("server: received signal", "signal", sig.String())
		cancel()
	}()

	if *enablePprofFlag {
		go func() {
			log.Info("starting pprof server", "address", "localhost:6060")
			err := http.ListenAndServe("localhost:6060", nil)
			if err != nil {
				log.Error("failed to start pprof server", "error", err)
			}
		}()
	}

	var metricsServerErrCh = make(chan error, 1)
	if *metricsAddrFlag != "" {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			listener, err := net.Listen("tcp", *metricsAddrFlag)
			if err != nil {
				log.Error("failed to start prometheus metrics server listener", "error", err)
				metricsServerErrCh <- err
				return
			}
			log.Info("prometheus metrics server listening", "address", listener.Addr().String())
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("failed to start prometheus metrics server", "error", err)
				metricsServerErrCh <- err
				return
			}
		}()
	}

	dzRPCClient := rpc.NewWithRetries(networkConfig.LedgerPublicRPCURL, nil)
	defer dzRPCClient.Close()
	serviceabilityClient := serviceability.New(dzRPCClient, networkConfig.ServiceabilityProgramID)
	telemetryClient := telemetry.New(log, dzRPCClient, nil, networkConfig.TelemetryProgramID)

	solanaRPC := rpc.NewWithRetries(solanaNetworkConfig.RPCURL, nil)
	defer solanaRPC.Close()

	// Initialize DuckLake database
	s3Config, err := duck.PrepareS3ConfigForStorageURI(ctx, log, *duckLakeStorageURIFlag)
	if err != nil {
		return err
	}
	log.Info("initializing ducklake database", "catalog", *duckLakeCatalogNameFlag, "catalogURI", duck.RedactedCatalogURI(*duckLakeCatalogURIFlag), "storageURI", duck.RedactedStorageURI(*duckLakeStorageURIFlag))
	db, err := duck.NewLake(ctx, log, *duckLakeCatalogNameFlag, *duckLakeCatalogURIFlag, *duckLakeStorageURIFlag, false, s3Config)
	if err != nil {
		return fmt.Errorf("failed to create DuckLake database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("failed to close DuckLake database", "error", err)
		}
	}()

	// Determine GeoIP database paths: flag takes precedence, then env var, then default
	geoipCityDBPath := *geoipCityDBPathFlag
	if geoipCityDBPath == defaultGeoipCityDBPath {
		if envPath := os.Getenv(geoipCityDBPathEnvVar); envPath != "" {
			geoipCityDBPath = envPath
		}
	}

	geoipASNDBPath := *geoipASNDBPathFlag
	if geoipASNDBPath == defaultGeoipASNDBPath {
		if envPath := os.Getenv(geoipASNDBPathEnvVar); envPath != "" {
			geoipASNDBPath = envPath
		}
	}

	// Initialize GeoIP resolver
	geoIPResolver, geoIPCloseFn, err := initializeGeoIP(geoipCityDBPath, geoipASNDBPath, log)
	if err != nil {
		return fmt.Errorf("failed to initialize GeoIP: %w", err)
	}
	defer func() {
		if err := geoIPCloseFn(); err != nil {
			log.Error("failed to close GeoIP resolver", "error", err)
		}
	}()

	// Initialize InfluxDB client from environment variables (optional)
	var influxDBClient dztelemusage.InfluxDBClient
	influxURL := os.Getenv("INFLUX_URL")
	influxToken := os.Getenv("INFLUX_TOKEN")
	influxBucket := os.Getenv("INFLUX_BUCKET")
	var deviceUsageQueryWindow time.Duration
	if *deviceUsageQueryWindowFlag == 0 {
		deviceUsageQueryWindow = defaultDeviceUsageInfluxQueryWindow
	} else {
		deviceUsageQueryWindow = *deviceUsageQueryWindowFlag
	}
	if influxURL != "" && influxToken != "" && influxBucket != "" {
		influxDBClient, err = dztelemusage.NewSDKInfluxDBClient(influxURL, influxToken, influxBucket)
		if err != nil {
			return fmt.Errorf("failed to create InfluxDB client: %w", err)
		}
		defer func() {
			if influxDBClient != nil {
				if closeErr := influxDBClient.Close(); closeErr != nil {
					log.Warn("failed to close InfluxDB client", "error", closeErr)
				}
			}
		}()
		log.Info("device usage (InfluxDB) client initialized")
	} else {
		log.Info("device usage (InfluxDB) environment variables not set, telemetry usage view will be disabled")
	}

	// Initialize server
	server, err := server.New(ctx, server.Config{
		ListenAddr:        *listenAddrFlag,
		ReadHeaderTimeout: 30 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		IndexerConfig: indexer.Config{
			Logger: log,
			Clock:  clockwork.NewRealClock(),
			DB:     db,

			RefreshInterval: *refreshIntervalFlag,
			MaxConcurrency:  *maxConcurrencyFlag,

			// GeoIP configuration
			GeoIPResolver: geoIPResolver,

			// Serviceability configuration
			ServiceabilityRPC: serviceabilityClient,

			// Telemetry configuration
			TelemetryRPC:           telemetryClient,
			DZEpochRPC:             dzRPCClient,
			InternetLatencyAgentPK: networkConfig.InternetLatencyCollectorPK,
			InternetDataProviders:  telemetryconfig.InternetTelemetryDataProviders,

			// Device usage configuration
			DeviceUsageInfluxClient:      influxDBClient,
			DeviceUsageInfluxBucket:      influxBucket,
			DeviceUsageInfluxQueryWindow: deviceUsageQueryWindow,
			DeviceUsageRefreshInterval:   *deviceUsageRefreshIntervalFlag,

			// Solana configuration
			SolanaRPC: solanaRPC,

			// Maintenance configuration
			MaintenanceIntervalShort: *maintenanceIntervalShortFlag,
			MaintenanceIntervalLong:  *maintenanceIntervalLongFlag,
			ExpireSnapshotsOlderThan: *maintenanceExpireOlderThanFlag,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		err := server.Run(ctx)
		if err != nil {
			serverErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("server: shutting down", "reason", ctx.Err())
		return nil
	case err := <-serverErrCh:
		log.Error("server: server error causing shutdown", "error", err)
		return err
	case err := <-metricsServerErrCh:
		log.Error("server: metrics server error causing shutdown", "error", err)
		return err
	}
}

func initializeGeoIP(cityDBPath, asnDBPath string, log *slog.Logger) (geoip.Resolver, func() error, error) {
	cityDB, err := geoip2.Open(cityDBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open GeoIP city database: %w", err)
	}

	asnDB, err := geoip2.Open(asnDBPath)
	if err != nil {
		cityDB.Close()
		return nil, nil, fmt.Errorf("failed to open GeoIP ASN database: %w", err)
	}

	metroDB, err := metrodb.New()
	if err != nil {
		cityDB.Close()
		asnDB.Close()
		return nil, nil, fmt.Errorf("failed to create metro database: %w", err)
	}

	resolver, err := geoip.NewResolver(log, cityDB, asnDB, metroDB)
	if err != nil {
		cityDB.Close()
		asnDB.Close()
		return nil, nil, fmt.Errorf("failed to create GeoIP resolver: %w", err)
	}

	return resolver, func() error {
		if err := cityDB.Close(); err != nil {
			return fmt.Errorf("failed to close city database: %w", err)
		}
		if err := asnDB.Close(); err != nil {
			return fmt.Errorf("failed to close ASN database: %w", err)
		}
		return nil
	}, nil
}
