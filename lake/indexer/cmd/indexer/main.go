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

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/config"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/indexer"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/server"
	"github.com/malbeclabs/doublezero/lake/utils/pkg/logger"
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
	migrationsEnableFlag := flag.Bool("migrations-enable", false, "enable ClickHouse migrations on startup")

	// ClickHouse configuration
	clickhouseAddrFlag := flag.String("clickhouse-addr", "", "ClickHouse server address (e.g., localhost:9000, or set CLICKHOUSE_ADDR_TCP env var)")
	clickhouseDatabaseFlag := flag.String("clickhouse-database", "default", "ClickHouse database name (or set CLICKHOUSE_DATABASE env var)")
	clickhouseUsernameFlag := flag.String("clickhouse-username", "default", "ClickHouse username (or set CLICKHOUSE_USERNAME env var)")
	clickhousePasswordFlag := flag.String("clickhouse-password", "", "ClickHouse password (or set CLICKHOUSE_PASSWORD env var)")
	clickhouseSecureFlag := flag.Bool("clickhouse-secure", false, "Enable TLS for ClickHouse Cloud (or set CLICKHOUSE_SECURE=true env var)")

	// Neo4j configuration (optional)
	neo4jURIFlag := flag.String("neo4j-uri", "", "Neo4j server URI (e.g., bolt://localhost:7687, or set NEO4J_URI env var)")
	neo4jDatabaseFlag := flag.String("neo4j-database", "neo4j", "Neo4j database name (or set NEO4J_DATABASE env var)")
	neo4jUsernameFlag := flag.String("neo4j-username", "neo4j", "Neo4j username (or set NEO4J_USERNAME env var)")
	neo4jPasswordFlag := flag.String("neo4j-password", "", "Neo4j password (or set NEO4J_PASSWORD env var)")
	neo4jMigrationsEnableFlag := flag.Bool("neo4j-migrations-enable", false, "Enable Neo4j migrations on startup")

	// GeoIP configuration
	geoipCityDBPathFlag := flag.String("geoip-city-db-path", defaultGeoipCityDBPath, "Path to MaxMind GeoIP2 City database file (or set MCP_GEOIP_CITY_DB_PATH env var)")
	geoipASNDBPathFlag := flag.String("geoip-asn-db-path", defaultGeoipASNDBPath, "Path to MaxMind GeoIP2 ASN database file (or set MCP_GEOIP_ASN_DB_PATH env var)")

	// Indexer configuration
	dzEnvFlag := flag.String("dz-env", defaultDZEnv, "DZ ledger environment (devnet, testnet, mainnet-beta)")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment (devnet, testnet, mainnet-beta)")
	refreshIntervalFlag := flag.Duration("cache-ttl", defaultRefreshInterval, "cache TTL duration")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent operations")
	deviceUsageQueryWindowFlag := flag.Duration("device-usage-query-window", defaultDeviceUsageInfluxQueryWindow, "Query window for device usage (default: 1 hour)")
	deviceUsageRefreshIntervalFlag := flag.Duration("device-usage-refresh-interval", defaultDeviceUsageRefreshInterval, "Refresh interval for device usage (default: 5 minutes)")

	// ISIS configuration (requires Neo4j, enabled by default when Neo4j is configured)
	isisEnabledFlag := flag.Bool("isis-enabled", true, "Enable IS-IS sync from S3 (or set ISIS_ENABLED env var)")
	isisS3BucketFlag := flag.String("isis-s3-bucket", "doublezero-mn-beta-isis-db", "S3 bucket for IS-IS dumps (or set ISIS_S3_BUCKET env var)")
	isisS3RegionFlag := flag.String("isis-s3-region", "us-east-1", "AWS region for IS-IS S3 bucket (or set ISIS_S3_REGION env var)")
	isisRefreshIntervalFlag := flag.Duration("isis-refresh-interval", 30*time.Second, "Refresh interval for IS-IS sync (or set ISIS_REFRESH_INTERVAL env var)")

	// Readiness configuration
	skipReadyWaitFlag := flag.Bool("skip-ready-wait", false, "Skip waiting for views to be ready (for preview/dev environments)")

	flag.Parse()

	// Override flags with environment variables if set
	if envClickhouseAddr := os.Getenv("CLICKHOUSE_ADDR_TCP"); envClickhouseAddr != "" {
		*clickhouseAddrFlag = envClickhouseAddr
	}
	if envClickhouseDatabase := os.Getenv("CLICKHOUSE_DATABASE"); envClickhouseDatabase != "default" {
		*clickhouseDatabaseFlag = envClickhouseDatabase
	}
	if envClickhouseUsername := os.Getenv("CLICKHOUSE_USERNAME"); envClickhouseUsername != "default" {
		*clickhouseUsernameFlag = envClickhouseUsername
	}
	if envClickhousePassword := os.Getenv("CLICKHOUSE_PASSWORD"); envClickhousePassword != "" {
		*clickhousePasswordFlag = envClickhousePassword
	}
	if os.Getenv("CLICKHOUSE_SECURE") == "true" {
		*clickhouseSecureFlag = true
	}
	if envDZEnv := os.Getenv("DZ_ENV"); envDZEnv != "" {
		*dzEnvFlag = envDZEnv
	}

	// Override Neo4j flags with environment variables if set
	if envNeo4jURI := os.Getenv("NEO4J_URI"); envNeo4jURI != "" {
		*neo4jURIFlag = envNeo4jURI
	}
	if envNeo4jDatabase := os.Getenv("NEO4J_DATABASE"); envNeo4jDatabase != "" {
		*neo4jDatabaseFlag = envNeo4jDatabase
	}
	if envNeo4jUsername := os.Getenv("NEO4J_USERNAME"); envNeo4jUsername != "" {
		*neo4jUsernameFlag = envNeo4jUsername
	}
	if envNeo4jPassword := os.Getenv("NEO4J_PASSWORD"); envNeo4jPassword != "" {
		*neo4jPasswordFlag = envNeo4jPassword
	}

	// Override ISIS flags with environment variables if set
	if envISISEnabled := os.Getenv("ISIS_ENABLED"); envISISEnabled != "" {
		*isisEnabledFlag = envISISEnabled == "true"
	}
	if envISISBucket := os.Getenv("ISIS_S3_BUCKET"); envISISBucket != "" {
		*isisS3BucketFlag = envISISBucket
	}
	if envISISRegion := os.Getenv("ISIS_S3_REGION"); envISISRegion != "" {
		*isisS3RegionFlag = envISISRegion
	}
	if envISISRefreshInterval := os.Getenv("ISIS_REFRESH_INTERVAL"); envISISRefreshInterval != "" {
		if d, err := time.ParseDuration(envISISRefreshInterval); err == nil {
			*isisRefreshIntervalFlag = d
		}
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

	// Initialize ClickHouse client (required)
	if *clickhouseAddrFlag == "" {
		return fmt.Errorf("clickhouse-addr is required")
	}
	log.Debug("clickhouse client initializing", "addr", *clickhouseAddrFlag, "database", *clickhouseDatabaseFlag, "username", *clickhouseUsernameFlag, "secure", *clickhouseSecureFlag)
	clickhouseDB, err := clickhouse.NewClient(ctx, log, *clickhouseAddrFlag, *clickhouseDatabaseFlag, *clickhouseUsernameFlag, *clickhousePasswordFlag, *clickhouseSecureFlag)
	if err != nil {
		return fmt.Errorf("failed to create ClickHouse client: %w", err)
	}
	defer func() {
		if err := clickhouseDB.Close(); err != nil {
			log.Error("failed to close ClickHouse database", "error", err)
		}
	}()
	log.Info("clickhouse client initialized", "addr", *clickhouseAddrFlag, "database", *clickhouseDatabaseFlag)

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

	// Initialize Neo4j client (optional)
	var neo4jClient neo4j.Client
	if *neo4jURIFlag != "" {
		neo4jClient, err = neo4j.NewClient(ctx, log, *neo4jURIFlag, *neo4jDatabaseFlag, *neo4jUsernameFlag, *neo4jPasswordFlag)
		if err != nil {
			return fmt.Errorf("failed to create Neo4j client: %w", err)
		}
		defer func() {
			if neo4jClient != nil {
				if closeErr := neo4jClient.Close(ctx); closeErr != nil {
					log.Warn("failed to close Neo4j client", "error", closeErr)
				}
			}
		}()

		log.Info("Neo4j client initialized", "uri", *neo4jURIFlag, "database", *neo4jDatabaseFlag)
	} else {
		log.Info("Neo4j URI not set, graph store will be disabled")
	}

	// Initialize server
	server, err := server.New(ctx, server.Config{
		ListenAddr:        *listenAddrFlag,
		ReadHeaderTimeout: 30 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		VersionInfo: server.VersionInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
		IndexerConfig: indexer.Config{
			Logger:           log,
			Clock:            clockwork.NewRealClock(),
			ClickHouse:       clickhouseDB,
			MigrationsEnable: *migrationsEnableFlag,
			MigrationsConfig: clickhouse.MigrationConfig{
				Addr:     *clickhouseAddrFlag,
				Database: *clickhouseDatabaseFlag,
				Username: *clickhouseUsernameFlag,
				Password: *clickhousePasswordFlag,
				Secure:   *clickhouseSecureFlag,
			},

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

			// Neo4j configuration
			Neo4j:                 neo4jClient,
			Neo4jMigrationsEnable: *neo4jMigrationsEnableFlag,
			Neo4jMigrationsConfig: neo4j.MigrationConfig{
				URI:      *neo4jURIFlag,
				Database: *neo4jDatabaseFlag,
				Username: *neo4jUsernameFlag,
				Password: *neo4jPasswordFlag,
			},

			// ISIS configuration
			ISISEnabled:         *isisEnabledFlag,
			ISISS3Bucket:        *isisS3BucketFlag,
			ISISS3Region:        *isisS3RegionFlag,
			ISISRefreshInterval: *isisRefreshIntervalFlag,

			// Readiness configuration
			SkipReadyWait: *skipReadyWaitFlag,
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
