package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
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
	defaultListenAddr                   = "0.0.0.0:8010"
	defaultRefreshInterval              = 60 * time.Second
	defaultMaxConcurrency               = 64
	defaultDZEnv                        = config.EnvMainnetBeta
	defaultMetricsAddr                  = "0.0.0.0:0"
	defaultEmbeddedDBPath               = ":memory:"
	defaultEmbeddedDBSpillDir           = ".tmp/embedded-duckdb-spill-tmp"
	defaultEmbeddedDBPathEnvVar         = "MCP_EMBEDDED_DB_PATH"
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

	// DuckDB configuration
	duckDBPathFlag := flag.String("duckdb-database-path", defaultEmbeddedDBPath, "Path to DuckDB database file (empty for in-memory, or set MCP_EMBEDDED_DB_PATH env var)")
	duckDBSpillDirFlag := flag.String("duckdb-spill-dir", defaultEmbeddedDBSpillDir, "Path to DuckDB temporary spill directory")

	// PostgreSQL connection configuration
	postgresURIFlag := flag.String("postgres-uri", "", "PostgreSQL connection URI (or set MCP_POSTGRES_URI env var). Format: postgres://user:password@host:port/database?sslmode=disable")

	// GeoIP configuration
	geoipCityDBPathFlag := flag.String("geoip-city-db-path", defaultGeoipCityDBPath, "Path to MaxMind GeoIP2 City database file (or set MCP_GEOIP_CITY_DB_PATH env var)")
	geoipASNDBPathFlag := flag.String("geoip-asn-db-path", defaultGeoipASNDBPath, "Path to MaxMind GeoIP2 ASN database file (or set MCP_GEOIP_ASN_DB_PATH env var)")

	// Indexer configuration
	indexerEnableFlag := flag.Bool("indexer-enable", false, "Enable in-process indexer with embedded database")
	dzEnvFlag := flag.String("env", defaultDZEnv, "doublezero environment (devnet, testnet, mainnet-beta)")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment (devnet, testnet, mainnet-beta)")
	refreshIntervalFlag := flag.Duration("cache-ttl", defaultRefreshInterval, "cache TTL duration")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent operations")
	deviceUsageQueryWindowFlag := flag.Duration("device-usage-query-window", defaultDeviceUsageInfluxQueryWindow, "Query window for device usage (default: 1 hour)")
	deviceUsageRefreshIntervalFlag := flag.Duration("device-usage-refresh-interval", defaultDeviceUsageRefreshInterval, "Refresh interval for device usage (default: 5 minutes)")

	flag.Parse()

	// Override flags with environment variables if set
	if envURI := os.Getenv("MCP_POSTGRES_URI"); envURI != "" {
		*postgresURIFlag = envURI
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

	// Parse allowed tokens from environment variable (comma-separated)
	// Auth can be explicitly disabled with MCP_AUTH_DISABLED=true
	var allowedTokens []string
	authDisabled := os.Getenv("MCP_AUTH_DISABLED") == "true"

	if authDisabled {
		log.Info("mcp server: authentication explicitly disabled")
	} else if tokensEnv := os.Getenv("MCP_ALLOWED_TOKENS"); tokensEnv != "" {
		for token := range strings.SplitSeq(tokensEnv, ",") {
			token = strings.TrimSpace(token)
			if token != "" {
				allowedTokens = append(allowedTokens, token)
			}
		}
		if len(allowedTokens) > 0 {
			log.Info("mcp server: token authentication enabled", "token_count", len(allowedTokens))
		}
	} else {
		log.Info("mcp server: authentication disabled (no tokens configured)")
	}

	var serverQuerier *querier.Querier
	var serverIndexer *indexer.Indexer
	if *indexerEnableFlag {
		dzRPCClient := rpc.NewWithRetries(networkConfig.LedgerPublicRPCURL, nil)
		defer dzRPCClient.Close()
		serviceabilityClient := serviceability.New(dzRPCClient, networkConfig.ServiceabilityProgramID)
		telemetryClient := telemetry.New(log, dzRPCClient, nil, networkConfig.TelemetryProgramID)

		solanaRPC := rpc.NewWithRetries(solanaNetworkConfig.RPCURL, nil)
		defer solanaRPC.Close()

		// Initialize database
		dbPath := *duckDBPathFlag
		if dbPath == "" || dbPath == defaultEmbeddedDBPath {
			if envPath := os.Getenv(defaultEmbeddedDBPathEnvVar); envPath != "" {
				dbPath = envPath
			}
		}
		if dbPath == "" {
			dbPath = defaultEmbeddedDBPath
		}

		log.Info("using embedded DuckDB database", "path", dbPath)
		db, dbCloseFn, err := initializeDuckDB(ctx, dbPath, *duckDBSpillDirFlag, log)
		if err != nil {
			return fmt.Errorf("failed to initialize DuckDB: %w", err)
		}
		defer func() {
			if err := dbCloseFn(); err != nil {
				log.Error("failed to close DuckDB", "error", err)
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

		// Initialize indexer
		indexer, err := indexer.New(ctx, indexer.Config{
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
		})
		if err != nil {
			return fmt.Errorf("failed to create indexer: %w", err)
		}

		// Initialize querier
		querier, err := querier.New(querier.Config{
			Logger: log,
			DB:     db,
			// Schemas: indexer.Schemas(),
		})
		if err != nil {
			return fmt.Errorf("failed to create querier: %w", err)
		}
		serverIndexer = indexer
		serverQuerier = querier

		log.Info("using embedded DuckDB database with internal indexer", "dbPath", dbPath, "dbSpillDir", *duckDBSpillDirFlag)
	} else {
		// Connect to querier server via PostgreSQL
		if *postgresURIFlag == "" {
			return fmt.Errorf("postgres URI is required when indexer is disabled (set --postgres-uri or MCP_POSTGRES_URI env var)")
		}

		// Create a PostgreSQL-backed duck.DB implementation
		db, err := server.NewPostgresDBForQuerier(ctx, *postgresURIFlag, log)
		if err != nil {
			return fmt.Errorf("failed to connect to querier server: %w", err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				log.Error("failed to close postgres connection", "error", err)
			}
		}()

		// Initialize querier with PostgreSQL connection to querier server
		querier, err := querier.New(querier.Config{
			Logger: log,
			DB:     db,
		})
		if err != nil {
			return fmt.Errorf("failed to create querier: %w", err)
		}
		serverQuerier = querier

		log.Info("using querier server via PostgreSQL", "uri", redactPostgresURI(*postgresURIFlag))
	}

	// Initialize MCP server
	server, err := server.New(ctx, server.Config{
		Version:       version,
		ListenAddr:    *listenAddrFlag,
		Logger:        log,
		Querier:       serverQuerier,
		Indexer:       serverIndexer,
		AllowedTokens: allowedTokens,
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

// redactPostgresURI redacts the password from a PostgreSQL URI for logging
func redactPostgresURI(uri string) string {
	// Simple redaction: replace password in postgres://user:password@host format
	if strings.Contains(uri, "@") {
		parts := strings.Split(uri, "@")
		if len(parts) == 2 {
			authPart := parts[0]
			if strings.Contains(authPart, ":") {
				authParts := strings.SplitN(authPart, ":", 3) // postgres://, user, password
				if len(authParts) >= 3 {
					authParts[2] = "REDACTED"
					return strings.Join(authParts, ":") + "@" + parts[1]
				}
			}
		}
	}
	return uri
}

func initializeDuckDB(ctx context.Context, dbPath string, spillDir string, log *slog.Logger) (duck.DB, func() error, error) {
	if spillDir != "" {
		// Convert spill directory to absolute path
		var err error
		spillDir, err = filepath.Abs(spillDir)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get absolute path for spill directory: %w", err)
		}

		// Delete DuckDB spill directory if it exists
		if _, err := os.Stat(spillDir); err == nil {
			log.Debug("deleting existing duckdb spill directory", "path", spillDir)
			if err := os.RemoveAll(spillDir); err != nil {
				return nil, nil, fmt.Errorf("failed to delete spill directory: %w", err)
			}
		}

		// Create DuckDB spill directory if it doesn't exist
		if err := os.MkdirAll(spillDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create spill directory: %w", err)
		}
	}

	// Create directory for DuckDB database file if it doesn't exist
	if dbPath != "" && dbPath != ":memory:" {
		dbDir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create database directory: %w", err)
		}
		log.Info("using persistent database", "path", dbPath)
	} else {
		log.Info("using in-memory database")
	}

	db, err := duck.NewDB(ctx, dbPath, log)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create recoverable database: %w", err)
	}

	if spillDir != "" {
		// Set DuckDB spill directory
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get connection: %w", err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "PRAGMA temp_directory=?", spillDir); err != nil {
			return nil, nil, fmt.Errorf("failed to set DuckDB spill directory: %w", err)
		}
		log.Debug("configured duckdb spill directory", "path", spillDir)
	}

	return db, func() error {
		if err := db.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
		if spillDir != "" {
			if err := os.RemoveAll(spillDir); err != nil {
				return fmt.Errorf("failed to delete spill directory: %w", err)
			}
		}
		return nil
	}, nil
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
