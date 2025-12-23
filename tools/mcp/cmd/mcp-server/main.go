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
	"syscall"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/jonboulle/clockwork"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/config"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/metrics"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/server"
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
	defaultDBPath       = ":memory:"
	defaultDBPathEnvVar = "MCP_DB_PATH"
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
	flag.Parse()

	networkConfig, err := config.NetworkConfigForEnv(*envFlag)
	if err != nil {
		return fmt.Errorf("failed to get network config: %w", err)
	}

	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(*solanaEnvFlag)
	if err != nil {
		return fmt.Errorf("failed to get solana network config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := newLogger(*verboseFlag)

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

	// Create directory for database file if it doesn't exist
	if dbPath != "" && dbPath != ":memory:" {
		dbDir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
		log.Info("using persistent database", "path", dbPath)
	} else {
		log.Info("using in-memory database")
	}

	db, err := duck.NewDB(dbPath, log)
	if err != nil {
		return fmt.Errorf("failed to create recoverable database: %w", err)
	}
	defer db.Close()

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
		return nil
	case err := <-serverErrCh:
		return err
	case err := <-metricsServerErrCh:
		return err
	}
}

func newLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(formatRFC3339Millis(t))
			}
			if s, ok := a.Value.Any().(string); ok && s == "" {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func formatRFC3339Millis(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ms := t.Nanosecond() / 1_000_000
	return fmt.Sprintf("%s.%03dZ", base, ms)
}
