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
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/logger"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/metrics"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	defaultListenAddr      = "0.0.0.0:8010"
	defaultRefreshInterval = 60 * time.Second
	defaultMaxConcurrency  = 64
	defaultDZEnv           = config.EnvMainnetBeta
	defaultMetricsAddr     = "0.0.0.0:8080"
	// defaultDBPath          = ".tmp/mcp.duckdb"
	defaultDBPath          = ":memory:"
	defaultDuckDBSpillPath = ".tmp/duckdb-spill-tmp"
	defaultDBPathEnvVar    = "MCP_DB_PATH"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")
	envFlag := flag.String("env", defaultDZEnv, "doublezero environment (devnet, testnet, mainnet-beta)")
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "solana environment (devnet, testnet, mainnet-beta)")
	listenAddrFlag := flag.String("listen-addr", defaultListenAddr, "HTTP server listen address")
	refreshIntervalFlag := flag.Duration("cache-ttl", defaultRefreshInterval, "cache TTL duration")
	maxConcurrencyFlag := flag.Int("max-concurrency", defaultMaxConcurrency, "maximum number of concurrent operations")
	enablePprofFlag := flag.Bool("enable-pprof", false, "enable pprof server")
	metricsAddrFlag := flag.String("metrics-addr", defaultMetricsAddr, "Address to listen on for prometheus metrics")
	dbPathFlag := flag.String("db-path", defaultDBPath, "Path to DuckDB database file (empty for in-memory, or set MCP_DB_PATH env var)")
	dbSpillDirFlag := flag.String("db-spill-dir", defaultDuckDBSpillPath, "Path to DuckDB temporary spill directory")
	flag.Parse()

	networkConfig, err := config.NetworkConfigForEnv(*envFlag)
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

	// Determine database path: flag takes precedence, then env var, then default
	dbPath := *dbPathFlag
	if dbPath == "" {
		dbPath = os.Getenv(defaultDBPathEnvVar)
	}
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	// Initialize DuckDB database
	db, dbCloseFn, err := initializeDuckDB(dbPath, *dbSpillDirFlag, log)
	if err != nil {
		return fmt.Errorf("failed to initialize DuckDB: %w", err)
	}
	defer dbCloseFn()

	// Parse allowed tokens from environment variable (comma-separated)
	// Auth can be explicitly disabled with MCP_AUTH_DISABLED=true
	var allowedTokens []string
	authDisabled := os.Getenv("MCP_AUTH_DISABLED") == "true"

	if authDisabled {
		log.Info("mcp server: authentication explicitly disabled")
	} else if tokensEnv := os.Getenv("MCP_ALLOWED_TOKENS"); tokensEnv != "" {
		tokens := strings.Split(tokensEnv, ",")
		for _, token := range tokens {
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

	server, err := server.New(server.Config{
		Version:                version,
		ListenAddr:             *listenAddrFlag,
		Logger:                 log,
		Clock:                  clockwork.NewRealClock(),
		ServiceabilityRPC:      serviceabilityClient,
		TelemetryRPC:           telemetryClient,
		DZEpochRPC:             dzRPCClient,
		SolanaRPC:              solanaRPC,
		DB:                     db,
		RefreshInterval:        *refreshIntervalFlag,
		MaxConcurrency:         *maxConcurrencyFlag,
		InternetLatencyAgentPK: networkConfig.InternetLatencyCollectorPK,
		InternetDataProviders:  telemetryconfig.InternetTelemetryDataProviders,
		AllowedTokens:          allowedTokens,
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

func initializeDuckDB(dbPath string, spillDir string, log *slog.Logger) (duck.DB, func() error, error) {
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

	db, err := duck.NewDB(dbPath, log)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create recoverable database: %w", err)
	}

	if spillDir != "" {
		// Set DuckDB spill directory
		if _, err := db.Exec("PRAGMA temp_directory=?", spillDir); err != nil {
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
