package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/spf13/cobra"

	"github.com/malbeclabs/doublezero/config"
	collector "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
	ripeatlas "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/ripeatlas"
	wheresitup "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/wheresitup"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/telemetry/migrations"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
	dzrpc "github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
)

const (
	defaultStateDir                     = "/var/lib/doublezero-internet-latency-collector/state"
	defaultAtlasProbesPerLocation       = 1
	defaultRipeAtlasSamplingInterval    = 10 * time.Minute
	defaultRipeAtlasMeasurementInterval = 1 * time.Hour
	defaultRipeAtlasExportInterval      = 10 * time.Minute
	defaultWheresitupSamplingInterval   = 6 * time.Minute
	defaultLedgerSubmissionInterval     = 1 * time.Minute
	defaultWheresitupStateFile          = "wheresitup_jobs_to_process.json"
	defaultLogLevel                     = "info"
	cloudNodeFileEnvVar                 = "DZ_ILC_CLOUD_NODE_FILE"
	cloudNodeFileUsage                  = "JSON file of cloud regions to measure (code, cloud, lat, lng, atlas_probe_ids, ping_target); enables cloud mode. Falls back to " + cloudNodeFileEnvVar
	defaultClickhouseAddr               = "localhost:9440"
	defaultClickhouseDB                 = "default"
	defaultClickhouseUser               = "default"

	// A partial ClickHouse batch needs an external tick: the exporter has no timer of its own.
	defaultCloudFlushInterval = 30 * time.Second

	// defaultLedgerRPCTimeout bounds each individual ledger RPC request. The default solana-go
	// client uses a 5-minute timeout, which lets a request block long enough for a fetched
	// (finalized) blockhash to expire before the send's preflight runs, surfacing as
	// BlockhashNotFound. A short timeout fails fast so the submitter retries with a fresh
	// blockhash. It is set above the observed slow-but-healthy latency (~12s) yet well under the
	// blockhash validity window (~56s).
	defaultLedgerRPCTimeout = 15 * time.Second

	// defaultLedgerRPCMaxConns caps concurrent connections to the ledger RPC host. The solana-go
	// default of 9 is far below the submitter's concurrency (100), so submissions queue for a
	// connection and their blockhashes go stale in flight. Size the pool above the submitter
	// concurrency to avoid that bottleneck.
	defaultLedgerRPCMaxConns = 128
)

var (
	env                          string
	keypairPath                  string
	stateDir                     string
	logLevel                     string
	locationFile                 string
	cloudNodeFile                string
	dryRun                       bool
	wheresitupStateFile          string
	ripeatlasProbesPerLocation   int
	ripeatlasMeasurementInterval time.Duration
	ledgerSubmissionInterval     time.Duration
	ledgerRPCTimeout             time.Duration
	ledgerRPCMaxConns            int
	metricsAddr                  string

	version = "dev"
	commit  = "none"
	date    = "unknown"

	ErrEnvRequired = errors.New("env is required")

	networkConfig        *config.NetworkConfig
	solanaRPCClient      *solanarpc.Client
	serviceabilityClient *serviceability.Client
)

var rootCmd = &cobra.Command{
	Use:   "doublezero-internet-latency-collector",
	Short: "DoubleZero internet latency collector",
	Long: `DoubleZero collector gathers internet latency data from RIPE Atlas
and Wheresitup services for the DoubleZero network.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		var err error
		networkConfig, err = validateNetworkConfig(env)
		if err != nil {
			log.Error("failed to validate network config", "error", err)
			os.Exit(1)
		}

		solanaRPCClient = newLedgerRPCClient(networkConfig.LedgerPublicRPCURL, ledgerRPCTimeout, ledgerRPCMaxConns)
		serviceabilityClient = serviceability.New(solanaRPCClient, networkConfig.ServiceabilityProgramID)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("doublezero-collector %s (commit: %s, built: %s)\n", version, commit, date)
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run ongoing data collection operations (service mode)",
	Long: `Run continuous collector that creates WhereItUp jobs every interval,
RIPE Atlas measurements hourly, and exports RIPE Atlas results periodically.`,
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		log.Info("Operation started: run_continuous_collector",
			slog.String("wheresitup_interval", defaultWheresitupSamplingInterval.String()),
			slog.Bool("dry_run", dryRun),
			slog.String("env", env),
			slog.String("serviceability_program_id", networkConfig.ServiceabilityProgramID.String()),
		)

		// A cloud node file selects cloud mode: RIPE Atlas alone, to ClickHouse, no keypair.
		nodeFile := cloudNodeFilePath()
		cloudMode := nodeFile != ""

		var exp exporter.Exporter
		var ledgerExporter *exporter.BufferedLedgerExporter
		var cloudExporter *exporter.ClickHouseExporter

		if cloudMode {
			var err error
			cloudExporter, err = newCloudExporter(log)
			if err != nil {
				log.Error("failed to create clickhouse exporter", "error", err)
				os.Exit(1)
			}
			exp = cloudExporter
		} else {
			// Validate oracle agent keypair path.
			if keypairPath == "" {
				log.Error("keypair path is required")
				os.Exit(1)
			}
			if _, err := os.Stat(keypairPath); os.IsNotExist(err) {
				log.Error("oracle agent keypair does not exist", "path", keypairPath)
				os.Exit(1)
			}
			keypair, err := solana.PrivateKeyFromSolanaKeygenFile(keypairPath)
			if err != nil {
				log.Error("Failed to load oracle agent keypair", "error", err)
				os.Exit(1)
			}

			// Create exporter.
			telemetryClient := telemetry.New(log, solanaRPCClient, &keypair, networkConfig.TelemetryProgramID)
			epochFinder, err := epoch.NewFinder(log, solanaRPCClient)
			if err != nil {
				log.Error("failed to create epoch finder", "error", err)
				os.Exit(1)
			}
			ledgerExporter, err = exporter.NewBufferedLedgerExporter(exporter.BufferedLedgerExporterConfig{
				Logger:             log,
				Serviceability:     serviceabilityClient,
				Telemetry:          telemetryClient,
				SubmissionInterval: ledgerSubmissionInterval,
				OracleAgentPK:      keypair.PublicKey(),
				DataProviderSamplingIntervals: map[exporter.DataProviderName]time.Duration{
					exporter.DataProviderNameWheresitup: defaultWheresitupSamplingInterval,
					exporter.DataProviderNameRIPEAtlas:  defaultRipeAtlasSamplingInterval,
				},
				EpochFinder:    epochFinder,
				AttemptTimeout: 2 * ledgerRPCTimeout,
			})
			if err != nil {
				log.Error("failed to create exporter", "error", err)
				os.Exit(1)
			}
			exp = ledgerExporter
		}

		config, err := newCollectorConfig(log, exp, nodeFile)
		if err != nil {
			log.Error("Operation failed: new_collector_config", "error", err)
			os.Exit(1)
		}

		c, err := collector.New(config)
		if err != nil {
			log.Error("Operation failed: new_collector", "error", err)
			os.Exit(1)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		var wg sync.WaitGroup
		var errCh = make(chan error, 2)

		// Start the record sink: the ledger submission loop, or the ClickHouse flush ticker.
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cloudMode {
				runCloudFlushLoop(ctx, log, cloudExporter, defaultCloudFlushInterval)
				return
			}
			if err := ledgerExporter.Run(ctx); err != nil {
				log.Error("failed to run exporter", "error", err)
				cancel()
				errCh <- err
			}
		}()

		// Start the collector.
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Run(ctx); err != nil {
				log.Error("Operation failed: run_continuous_collector", "error", err)
				cancel()
				errCh <- err
			}
			log.Info("Operation completed: run_continuous_collector")
		}()

		wg.Wait()

		// Both goroutines can hand records to the exporter, so it closes only after both stop.
		if cloudExporter != nil {
			if err := cloudExporter.Close(); err != nil {
				log.Warn("failed to close clickhouse exporter",
					slog.String("error", err.Error()),
					slog.Int("buffered", cloudExporter.Buffered()))
			}
		}

		close(errCh)
		if err := <-errCh; err != nil {
			log.Error("run failed", "error", err)
			os.Exit(1)
		}
	},
}

var ripeatlasCmd = &cobra.Command{
	Use:   "atlas",
	Short: "Interactive RIPE Atlas commands",
	Long:  `Commands for managing RIPE Atlas probes and measurements.`,
}

var ripeatlasListProbesCmd = &cobra.Command{
	Use:   "list-probes",
	Short: "List nearest RIPE Atlas probes for each location",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: list_ripeatlas_probes")

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		locations := loadLocations(ctx, log, serviceabilityClient)
		if locations == nil {
			return
		}

		ripeCollector := ripeatlas.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

		if err := ripeCollector.ListAtlasProbes(ctx, locations); err != nil {
			if ctx.Err() != nil {
				log.Info("Operation cancelled by signal")
				return
			}
			log.Error("Operation failed: ripeatlas_probe_discovery", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: list_ripeatlas_probes")
	},
}

var ripeatlasListMeasurementsCmd = &cobra.Command{
	Use:   "list-measurements",
	Short: "List all RIPE Atlas measurements in CSV format",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: list_ripeatlas_measurements")

		ripeCollector, err := newRipeAtlasCollector(log, nil, cloudNodeFilePath())
		if err != nil {
			log.Error("Operation failed: new_ripeatlas_collector", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if err := ripeCollector.ListMeasurements(context.Background()); err != nil {
			log.Error("Operation failed: list_ripeatlas_measurements", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: list_ripeatlas_measurements")
	},
}

var ripeatlasCreateMeasurementsCmd = &cobra.Command{
	Use:   "create-measurements",
	Short: "Create RIPE Atlas measurements between location pairs",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		ripeCollector, err := newRipeAtlasCollector(log, nil, cloudNodeFilePath())
		if err != nil {
			log.Error("Operation failed: new_ripeatlas_collector", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if err := ripeCollector.RunRipeAtlasMeasurementCreation(context.Background(), dryRun, ripeatlasProbesPerLocation, stateDir, defaultRipeAtlasSamplingInterval); err != nil {
			log.Error("Operation failed: create_ripeatlas_measurements", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: create_ripeatlas_measurements")
	},
}

var ripeatlasClearMeasurementsCmd = &cobra.Command{
	Use:   "clear-measurements",
	Short: "Clear all existing RIPE Atlas measurements",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: clear_atlas_measurements")

		ripeCollector, err := newRipeAtlasCollector(log, nil, cloudNodeFilePath())
		if err != nil {
			log.Error("Operation failed: new_ripeatlas_collector", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if err := ripeCollector.ClearAllMeasurements(context.Background()); err != nil {
			log.Error("Operation failed: clear_ripeatlas_measurements", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: clear_ripeatlas_measurements")
	},
}

// Wheresitup commands
var wheresitupCmd = &cobra.Command{
	Use:   "wheresitup",
	Short: "Interactive Wheresitup commands",
	Long:  `Commands for managing Wheresitup sources and jobs.`,
}

var wheresitupListSourcesCmd = &cobra.Command{
	Use:   "list-sources",
	Short: "List nearest Wheresitup sources for each location",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: list_wheresitup_sources")

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		locations := loadLocations(ctx, log, serviceabilityClient)
		if locations == nil {
			return
		}

		wheresitupCollector := wheresitup.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

		if err := wheresitupCollector.PrintSources(ctx, locations); err != nil {
			if ctx.Err() != nil {
				log.Info("Operation cancelled by signal")
				return
			}
			log.Error("Operation failed: wheresitup_discovery", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: list_wheresitup_sources")
	},
}

var wheresitupListJobsCmd = &cobra.Command{
	Use:   "list-jobs",
	Short: "List existing Wheresitup jobs",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: list_wheresitup_jobs")

		wheresitupCollector := wheresitup.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

		if err := wheresitupCollector.ListJobs(context.Background()); err != nil {
			log.Error("Operation failed: list_wheresitup_jobs", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: list_wheresitup_jobs")
	},
}

func cloudNodeFilePath() string {
	if cloudNodeFile != "" {
		return cloudNodeFile
	}
	return os.Getenv(cloudNodeFileEnvVar)
}

func loadCloudNodes(logger *slog.Logger, filename string) ([]ripeatlas.CloudNode, error) {
	jsonNodes, err := collector.LoadNodesFromJSON(logger, filename)
	if err != nil {
		return nil, err
	}

	nodes := make([]ripeatlas.CloudNode, 0, len(jsonNodes))
	for _, node := range jsonNodes {
		nodes = append(nodes, ripeatlas.CloudNode{
			Code:          node.Code,
			Cloud:         node.Cloud,
			Latitude:      node.Latitude,
			Longitude:     node.Longitude,
			AtlasProbeIDs: node.AtlasProbeIDs,
			PingTarget:    node.PingTarget,
		})
	}

	return nodes, nil
}

// newRipeAtlasCollector builds the collector for the configured mode. Cloud mode has its own
// measurement tag, description prefix and state file.
func newRipeAtlasCollector(log *slog.Logger, exp exporter.Exporter, nodeFile string) (*ripeatlas.Collector, error) {
	if nodeFile == "" {
		return ripeatlas.NewCollector(log, exp, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		}), nil
	}

	nodes, err := loadCloudNodes(log, nodeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cloud node file %s: %w", nodeFile, err)
	}

	log.Info("Running in cloud mode",
		slog.String("node_file", nodeFile),
		slog.Int("node_count", len(nodes)),
		slog.String("state_file", ripeatlas.CloudTimestampFileName))

	return ripeatlas.NewCloudCollector(log, exp, env, nodes), nil
}

func newCollectorConfig(log *slog.Logger, exp exporter.Exporter, nodeFile string) (collector.Config, error) {
	ripeatlasCollector, err := newRipeAtlasCollector(log, exp, nodeFile)
	if err != nil {
		return collector.Config{}, err
	}

	cfg := collector.Config{
		Logger:    log,
		RipeAtlas: ripeatlasCollector,
		CloudMode: nodeFile != "",

		WheresitupSamplingInterval:   defaultWheresitupSamplingInterval,
		RipeAtlasSamplingInterval:    defaultRipeAtlasSamplingInterval,
		RipeAtlasMeasurementInterval: ripeatlasMeasurementInterval,
		RipeAtlasExportInterval:      defaultRipeAtlasExportInterval,
		DryRun:                       dryRun,
		ProcessedJobsFile:            wheresitupStateFile,
		StateDir:                     stateDir,
		ProbesPerLocation:            ripeatlasProbesPerLocation,
		MetricsAddr:                  metricsAddr,
	}

	if nodeFile == "" {
		cfg.Wheresitup = wheresitup.NewCollector(log, exp, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})
	}

	return cfg, nil
}

type clickhouseConfig struct {
	Addr          string
	Database      string
	Username      string
	Password      string
	Table         string
	TLSDisabled   bool
	RunMigrations bool
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func loadClickHouseConfig() clickhouseConfig {
	return clickhouseConfig{
		Addr:          getenv("CLICKHOUSE_ADDR", defaultClickhouseAddr),
		Database:      getenv("CLICKHOUSE_DB", defaultClickhouseDB),
		Username:      getenv("CLICKHOUSE_USER", defaultClickhouseUser),
		Password:      getenv("CLICKHOUSE_PASS", ""),
		Table:         getenv("CLICKHOUSE_TABLE", exporter.DefaultClickHouseTable),
		TLSDisabled:   getenv("CLICKHOUSE_TLS_DISABLED", "") == "true",
		RunMigrations: getenv("CLICKHOUSE_RUN_MIGRATIONS", "") == "true",
	}
}

func newCloudExporter(log *slog.Logger) (*exporter.ClickHouseExporter, error) {
	cfg := loadClickHouseConfig()

	log.Info("Creating cloud exporter",
		slog.String("addr", cfg.Addr),
		slog.String("database", cfg.Database),
		slog.String("table", cfg.Table),
		slog.Bool("tls_disabled", cfg.TLSDisabled),
		slog.Bool("run_migrations", cfg.RunMigrations))

	if cfg.RunMigrations {
		if err := migrations.RunMigrations(cfg.Addr, cfg.Database, cfg.Username, cfg.Password, !cfg.TLSDisabled, log); err != nil {
			return nil, fmt.Errorf("failed to run clickhouse migrations: %w", err)
		}
	}

	return exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:      log,
		Addr:        cfg.Addr,
		Database:    cfg.Database,
		Username:    cfg.Username,
		Password:    cfg.Password,
		TLSDisabled: cfg.TLSDisabled,
		Table:       cfg.Table,
	})
}

func runCloudFlushLoop(ctx context.Context, log *slog.Logger, exp *exporter.ClickHouseExporter, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := exp.Flush(ctx); err != nil {
				log.Warn("failed to flush clickhouse exporter",
					slog.String("error", err.Error()),
					slog.Int("buffered", exp.Buffered()))
			}
		}
	}
}

func loadLocations(ctx context.Context, logger *slog.Logger, serviceabilityClient *serviceability.Client) []collector.LocationMatch {
	if locationFile != "" {
		logger.Info("Loading locations from JSON file", slog.String("file", locationFile))
		jsonLocations, err := collector.LoadLocationsFromJSON(logger, locationFile)
		if err != nil {
			logger.Error("Operation failed: load_locations_json",
				slog.String("error", err.Error()),
				slog.String("file", locationFile))
			return nil
		}
		var locations []collector.LocationMatch
		for _, loc := range jsonLocations {
			locations = append(locations, collector.LocationMatch{
				LocationCode: loc.Code,
				Latitude:     loc.Latitude,
				Longitude:    loc.Longitude,
			})
		}
		return locations
	} else {
		logger.Info("Loading locations from blockchain")
		return collector.GetLocations(ctx, logger, serviceabilityClient)
	}
}

// newLedgerRPCClient builds a Solana RPC client with a bounded per-request timeout and a
// connection pool sized for the submitter's concurrency. The default client uses a
// 5-minute timeout and caps connections per host at 9, which under concurrent submission lets a
// fetched finalized blockhash (valid only ~56s) expire while the send is queued or in flight,
// surfacing as a BlockhashNotFound preflight failure.
func newLedgerRPCClient(url string, timeout time.Duration, maxConns int) *solanarpc.Client {
	return dzrpc.New(url, dzrpc.Options{
		RequestTimeout:  timeout,
		MaxConnsPerHost: maxConns,
	})
}

func init() {
	rootCmd.PersistentFlags().StringVar(&env, "env", "", "Environment to run in (devnet, testnet, mainnet-beta)")
	rootCmd.PersistentFlags().DurationVar(&ledgerRPCTimeout, "ledger-rpc-timeout", defaultLedgerRPCTimeout, "Per-request timeout for ledger RPC calls; bounds how long a request may block so a stale blockhash fails fast and retries")
	rootCmd.PersistentFlags().IntVar(&ledgerRPCMaxConns, "ledger-rpc-max-conns", defaultLedgerRPCMaxConns, "Maximum concurrent connections to the ledger RPC host")
	rootCmd.PersistentFlags().StringVar(&keypairPath, "keypair", "", "Path to keypair for publishing metrics")
	rootCmd.PersistentFlags().StringVar(&stateDir, "state-dir", defaultStateDir, "Directory to store state files (timestamps, processed job IDs)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", defaultLogLevel, "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&locationFile, "location-file", "", "CSV file containing locations (LocationCode,Latitude,Longitude)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Log what would be created without actually creating measurements or jobs")

	runCmd.Flags().StringVar(&wheresitupStateFile, "wheresitup-job-state-file", defaultWheresitupStateFile, "File to track processed Wheresitup job IDs (JSON format)")
	runCmd.Flags().IntVar(&ripeatlasProbesPerLocation, "ripeatlas-probes-per-location", defaultAtlasProbesPerLocation, "Number of RIPE Atlas probes to associate with each DoubleZero location")
	runCmd.Flags().DurationVar(&ripeatlasMeasurementInterval, "ripeatlas-measurement-interval", defaultRipeAtlasMeasurementInterval, "Interval at which to run RIPE Atlas measurements")
	runCmd.Flags().DurationVar(&ledgerSubmissionInterval, "ledger-submission-interval", defaultLedgerSubmissionInterval, "Interval at which to submit metrics to the ledger")
	runCmd.Flags().StringVar(&metricsAddr, "metrics-addr", "127.0.0.1:2113", "Address to bind the metrics server to")
	runCmd.Flags().StringVar(&cloudNodeFile, "cloud-node-file", "", cloudNodeFileUsage)

	ripeatlasListMeasurementsCmd.Flags().StringVar(&cloudNodeFile, "cloud-node-file", "", cloudNodeFileUsage)
	ripeatlasCreateMeasurementsCmd.Flags().IntVar(&ripeatlasProbesPerLocation, "probes-per-location", defaultAtlasProbesPerLocation, "Number of RIPE Atlas probes to associate with each DoubleZero location")
	ripeatlasCreateMeasurementsCmd.Flags().StringVar(&cloudNodeFile, "cloud-node-file", "", cloudNodeFileUsage)
	ripeatlasClearMeasurementsCmd.Flags().StringVar(&cloudNodeFile, "cloud-node-file", "", cloudNodeFileUsage)

	nodefileCmd.PersistentFlags().StringVar(&nodeFilePath, "node-file", defaultNodeFilePath, "Path to the cloud region node file")
	nodefileCmd.PersistentFlags().BoolVar(&nodeFileSkipPing, "skip-ping", false, "Check only that each ping target is still published, without sending a ping")

	cobra.EnableCommandSorting = false

	rootCmd.AddCommand(ripeatlasCmd)
	rootCmd.AddCommand(wheresitupCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(nodefileCmd)

	ripeatlasCmd.AddCommand(ripeatlasListProbesCmd)
	ripeatlasCmd.AddCommand(ripeatlasListMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasCreateMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasClearMeasurementsCmd)

	wheresitupCmd.AddCommand(wheresitupListSourcesCmd)
	wheresitupCmd.AddCommand(wheresitupListJobsCmd)

	nodefileCmd.AddCommand(nodefileGenerateCmd)
	nodefileCmd.AddCommand(nodefileVerifyCmd)
}

func main() {
	// Set build info metric
	metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)

	// Add version command last so it appears after auto-generated commands
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func validateNetworkConfig(env string) (*config.NetworkConfig, error) {
	if env == "" {
		return nil, ErrEnvRequired
	}

	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}
	return networkConfig, nil
}
