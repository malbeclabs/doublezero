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
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
)

const (
	defaultStateDir                     = "/var/lib/doublezero-internet-latency-collector/state"
	defaultAtlasProbesPerLocation       = 1
	defaultRipeAtlasSamplingInterval    = 6 * time.Minute
	defaultRipeAtlasMeasurementInterval = 1 * time.Hour
	defaultRipeAtlasExportInterval      = 6 * time.Minute
	defaultWheresitupSamplingInterval   = 6 * time.Minute
	defaultLedgerSubmissionInterval     = 1 * time.Minute
	defaultWheresitupStateFile          = "wheresitup_jobs_to_process.json"
	defaultLogLevel                     = "info"
)

var (
	env                          string
	keypairPath                  string
	stateDir                     string
	logLevel                     string
	locationFile                 string
	dryRun                       bool
	wheresitupStateFile          string
	ripeatlasProbesPerLocation   int
	ripeatlasMeasurementInterval time.Duration
	ledgerSubmissionInterval     time.Duration
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

		solanaRPCClient = solanarpc.New(networkConfig.LedgerPublicRPCURL)
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
RIPE Atlas measurements hourly, and exports RIPE Atlas results every 2 minutes.`,
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		log.Info("Operation started: run_continuous_collector",
			slog.String("wheresitup_interval", defaultWheresitupSamplingInterval.String()),
			slog.Bool("dry_run", dryRun),
			slog.String("env", env),
			slog.String("serviceability_program_id", networkConfig.ServiceabilityProgramID.String()),
		)

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
		exporter, err := exporter.NewBufferedLedgerExporter(exporter.BufferedLedgerExporterConfig{
			Logger:             log,
			Serviceability:     serviceabilityClient,
			Telemetry:          telemetryClient,
			SubmissionInterval: ledgerSubmissionInterval,
			OracleAgentPK:      keypair.PublicKey(),
			DataProviderSamplingIntervals: map[exporter.DataProviderName]time.Duration{
				exporter.DataProviderNameWheresitup: defaultWheresitupSamplingInterval,
				exporter.DataProviderNameRIPEAtlas:  defaultRipeAtlasSamplingInterval,
			},
			EpochFinder: epochFinder,
		})
		if err != nil {
			log.Error("failed to create exporter", "error", err)
			os.Exit(1)
		}

		// Create data provider collectors.
		ripeatlasCollector := ripeatlas.NewCollector(log, exporter, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})
		wheresitupCollector := wheresitup.NewCollector(log, exporter, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

		config := collector.Config{
			Logger:     log,
			Wheresitup: wheresitupCollector,
			RipeAtlas:  ripeatlasCollector,

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

		c, err := collector.New(config)
		if err != nil {
			log.Error("Operation failed: new_collector", "error", err)
			os.Exit(1)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		var wg sync.WaitGroup
		var errCh = make(chan error, 2)
		wg.Add(2)

		// Start the ledger exporter.
		go func() {
			defer wg.Done()
			if err := exporter.Run(ctx); err != nil {
				log.Error("failed to run exporter", "error", err)
				cancel()
				errCh <- err
			}
		}()

		// Start the collector.
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

		ripeCollector := ripeatlas.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

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

		ripeCollector := ripeatlas.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

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

		ripeCollector := ripeatlas.NewCollector(log, nil, env, func(ctx context.Context) []collector.LocationMatch {
			return collector.GetLocations(ctx, log, serviceabilityClient)
		})

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

func init() {
	rootCmd.PersistentFlags().StringVar(&env, "env", "", "Environment to run in (devnet, testnet, mainnet-beta)")
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

	ripeatlasCreateMeasurementsCmd.Flags().IntVar(&ripeatlasProbesPerLocation, "probes-per-location", defaultAtlasProbesPerLocation, "Number of RIPE Atlas probes to associate with each DoubleZero location")

	cobra.EnableCommandSorting = false

	rootCmd.AddCommand(ripeatlasCmd)
	rootCmd.AddCommand(wheresitupCmd)
	rootCmd.AddCommand(runCmd)

	ripeatlasCmd.AddCommand(ripeatlasListProbesCmd)
	ripeatlasCmd.AddCommand(ripeatlasListMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasCreateMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasClearMeasurementsCmd)

	wheresitupCmd.AddCommand(wheresitupListSourcesCmd)
	wheresitupCmd.AddCommand(wheresitupListJobsCmd)
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
