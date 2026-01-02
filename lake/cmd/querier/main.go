package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
	"github.com/malbeclabs/doublezero/lake/pkg/querier/metrics"
	"github.com/malbeclabs/doublezero/lake/pkg/querier/server"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultHTTPListenAddr     = "0.0.0.0:3011"
	defaultPostgresListenAddr = "0.0.0.0:5432"
	defaultReadHeaderTimeout  = 30 * time.Second
	defaultShutdownTimeout    = 10 * time.Second
	defaultMetricsAddr        = "0.0.0.0:0"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	httpListenAddrFlag := flag.String("http-listen-addr", defaultHTTPListenAddr, "HTTP server listen address")
	postgresListenAddrFlag := flag.String("postgres-listen-addr", defaultPostgresListenAddr, "PostgreSQL wire protocol server listen address (set to empty string to disable)")
	readHeaderTimeoutFlag := flag.Duration("read-header-timeout", defaultReadHeaderTimeout, "HTTP read header timeout")
	shutdownTimeoutFlag := flag.Duration("shutdown-timeout", defaultShutdownTimeout, "Server shutdown timeout")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")

	// DuckLake configuration
	duckLakeCatalogNameFlag := flag.String("lake-catalog-name", "dzlake", "Name of the DuckLake catalog (or set DUCKLAKE_CATALOG_NAME env var)")
	duckLakeCatalogURIFlag := flag.String("lake-catalog-uri", "file://.tmp/lake/catalog.sqlite", "URI to the DuckLake catalog (or set LAKE_CATALOG_URI env var)")
	duckLakeStorageURIFlag := flag.String("ducklake-storage-uri", "file://.tmp/lake/data", "URI to the DuckLake storage directory (or set LAKE_STORAGE_URI env var)")
	duckLakeMemoryLimitFlag := flag.String("lake-memory-limit", "", "Memory limit for DuckDB connections (e.g., '22GB', '16GB'). If not set, uses DuckDB default (or set LAKE_MEMORY_LIMIT env var)")
	duckLakeTempDirectoryFlag := flag.String("lake-temp-dir", "", "Temp directory for DuckDB connections (e.g., '/tmp/duckdb_tmp'). If not set, uses DuckDB default (or set LAKE_TEMP_DIR env var)")
	duckLakeMaxTempDirectorySizeFlag := flag.String("lake-max-temp-dir-size", "", "Max temp directory size for DuckDB connections (e.g., '200GB', '100GB'). If not set, uses DuckDB default (or set LAKE_MAX_TEMP_DIR_SIZE env var)")
	duckLakeThreadsFlag := flag.String("lake-threads", "", "Number of threads for DuckDB connections (e.g., '1', '4', '8'). If not set, uses DuckDB default (or set LAKE_THREADS env var)")
	duckLakeMaxOpenConnsFlag := flag.Int("lake-max-open-conns", 0, "Maximum number of open connections in the pool (default: 32, or set LAKE_MAX_OPEN_CONNS env var)")
	duckLakeMaxIdleConnsFlag := flag.Int("lake-max-idle-conns", 0, "Maximum number of idle connections in the pool (default: 8, or set LAKE_MAX_IDLE_CONNS env var)")
	duckLakeConnMaxLifetimeFlag := flag.String("lake-conn-max-lifetime", "", "Maximum amount of time a connection may be reused (e.g., '10m', '1h'). Default: 10m (or set LAKE_CONN_MAX_LIFETIME env var)")
	duckLakeConnMaxIdleTimeFlag := flag.String("lake-conn-max-idle-time", "", "Maximum amount of time a connection may be idle (e.g., '2m', '30s'). Default: 2m (or set LAKE_CONN_MAX_IDLE_TIME env var)")

	flag.Parse()

	// Override flags with environment variables if set
	if envCatalogURI := os.Getenv("LAKE_CATALOG_URI"); envCatalogURI != "" {
		*duckLakeCatalogURIFlag = envCatalogURI
	}
	if envStorageURI := os.Getenv("LAKE_STORAGE_URI"); envStorageURI != "" {
		*duckLakeStorageURIFlag = envStorageURI
	}
	if envCatalogName := os.Getenv("DUCKLAKE_CATALOG_NAME"); envCatalogName != "" {
		*duckLakeCatalogNameFlag = envCatalogName
	}
	if envMemoryLimit := os.Getenv("LAKE_MEMORY_LIMIT"); envMemoryLimit != "" {
		*duckLakeMemoryLimitFlag = envMemoryLimit
	}
	if envTempDirectory := os.Getenv("LAKE_TEMP_DIR"); envTempDirectory != "" {
		*duckLakeTempDirectoryFlag = envTempDirectory
	}
	if envMaxTempDirectorySize := os.Getenv("LAKE_MAX_TEMP_DIR_SIZE"); envMaxTempDirectorySize != "" {
		*duckLakeMaxTempDirectorySizeFlag = envMaxTempDirectorySize
	}
	if envThreads := os.Getenv("LAKE_THREADS"); envThreads != "" {
		*duckLakeThreadsFlag = envThreads
	}
	if envMaxOpenConns := os.Getenv("LAKE_MAX_OPEN_CONNS"); envMaxOpenConns != "" {
		var val int
		if _, err := fmt.Sscanf(envMaxOpenConns, "%d", &val); err == nil {
			*duckLakeMaxOpenConnsFlag = val
		}
	}
	if envMaxIdleConns := os.Getenv("LAKE_MAX_IDLE_CONNS"); envMaxIdleConns != "" {
		var val int
		if _, err := fmt.Sscanf(envMaxIdleConns, "%d", &val); err == nil {
			*duckLakeMaxIdleConnsFlag = val
		}
	}
	if envConnMaxLifetime := os.Getenv("LAKE_CONN_MAX_LIFETIME"); envConnMaxLifetime != "" {
		*duckLakeConnMaxLifetimeFlag = envConnMaxLifetime
	}
	if envConnMaxIdleTime := os.Getenv("LAKE_CONN_MAX_IDLE_TIME"); envConnMaxIdleTime != "" {
		*duckLakeConnMaxIdleTimeFlag = envConnMaxIdleTime
	}

	log := logger.New(*verboseFlag)

	// Set up signal handling
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

	// Initialize DuckLake catalog database
	s3Config, err := duck.PrepareS3ConfigForStorageURI(ctx, log, *duckLakeStorageURIFlag)
	if err != nil {
		return fmt.Errorf("failed to prepare S3 config: %w", err)
	}
	var lakeConfig *duck.LakeConfig
	if *duckLakeMemoryLimitFlag != "" || *duckLakeTempDirectoryFlag != "" || *duckLakeMaxTempDirectorySizeFlag != "" || *duckLakeThreadsFlag != "" ||
		*duckLakeMaxOpenConnsFlag > 0 || *duckLakeMaxIdleConnsFlag > 0 || *duckLakeConnMaxLifetimeFlag != "" || *duckLakeConnMaxIdleTimeFlag != "" {
		lakeConfig = &duck.LakeConfig{}
		if *duckLakeMemoryLimitFlag != "" {
			lakeConfig.MemoryLimit = *duckLakeMemoryLimitFlag
		}
		if *duckLakeTempDirectoryFlag != "" {
			lakeConfig.TempDirectory = *duckLakeTempDirectoryFlag
		}
		if *duckLakeMaxTempDirectorySizeFlag != "" {
			lakeConfig.MaxTempDirectorySize = *duckLakeMaxTempDirectorySizeFlag
		}
		if *duckLakeThreadsFlag != "" {
			lakeConfig.Threads = *duckLakeThreadsFlag
		}
		if *duckLakeMaxOpenConnsFlag > 0 {
			val := *duckLakeMaxOpenConnsFlag
			lakeConfig.MaxOpenConns = &val
		}
		if *duckLakeMaxIdleConnsFlag > 0 {
			val := *duckLakeMaxIdleConnsFlag
			lakeConfig.MaxIdleConns = &val
		}
		if *duckLakeConnMaxLifetimeFlag != "" {
			if d, err := time.ParseDuration(*duckLakeConnMaxLifetimeFlag); err == nil {
				lakeConfig.ConnMaxLifetime = &d
			} else {
				return fmt.Errorf("invalid duration for --lake-conn-max-lifetime: %w", err)
			}
		}
		if *duckLakeConnMaxIdleTimeFlag != "" {
			if d, err := time.ParseDuration(*duckLakeConnMaxIdleTimeFlag); err == nil {
				lakeConfig.ConnMaxIdleTime = &d
			} else {
				return fmt.Errorf("invalid duration for --lake-conn-max-idle-time: %w", err)
			}
		}
	}
	var db duck.DB
	if lakeConfig != nil {
		db, err = duck.NewLakeWithConfig(ctx, log, *duckLakeCatalogNameFlag, *duckLakeCatalogURIFlag, *duckLakeStorageURIFlag, true, lakeConfig, s3Config)
	} else {
		db, err = duck.NewLake(ctx, log, *duckLakeCatalogNameFlag, *duckLakeCatalogURIFlag, *duckLakeStorageURIFlag, true, s3Config)
	}
	if err != nil {
		return fmt.Errorf("failed to create DuckLake: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("failed to close DuckLake", "error", err)
		}
	}()
	log.Info("using DuckLake database", "catalogName", *duckLakeCatalogNameFlag, "catalogURI", duck.RedactedCatalogURI(*duckLakeCatalogURIFlag), "storageURI", duck.RedactedStorageURI(*duckLakeStorageURIFlag), "memoryLimit", *duckLakeMemoryLimitFlag, "tempDirectory", *duckLakeTempDirectoryFlag, "maxTempDirectorySize", *duckLakeMaxTempDirectorySizeFlag, "threads", *duckLakeThreadsFlag)

	// Create HTTP listener
	httpListener, err := net.Listen("tcp", *httpListenAddrFlag)
	if err != nil {
		return fmt.Errorf("failed to create HTTP listener: %w", err)
	}
	defer httpListener.Close()

	// Create PostgreSQL listener (optional)
	var postgresListener net.Listener
	if *postgresListenAddrFlag != "" {
		postgresListener, err = net.Listen("tcp", *postgresListenAddrFlag)
		if err != nil {
			return fmt.Errorf("failed to create PostgreSQL listener: %w", err)
		}
		defer postgresListener.Close()
		log.Info("PostgreSQL wire protocol enabled", "address", *postgresListenAddrFlag)
	} else {
		log.Info("PostgreSQL wire protocol disabled")
	}

	// Initialize querier server
	srv, err := server.New(ctx, server.Config{
		HTTPListener:      httpListener,
		PostgresListener:  postgresListener,
		ReadHeaderTimeout: *readHeaderTimeoutFlag,
		ShutdownTimeout:   *shutdownTimeoutFlag,
		QuerierConfig: querier.Config{
			Logger: log,
			DB:     db,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create querier server: %w", err)
	}

	// Start server
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- srv.Run(ctx)
	}()

	// Wait for shutdown signal or server error
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
