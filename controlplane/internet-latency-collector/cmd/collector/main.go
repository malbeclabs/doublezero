package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	collector "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	ripeatlas "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/ripeatlas"
	wheresitup "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/wheresitup"
)

const (
	defaultStateDir                     = "/var/lib/doublezero-internet-latency-collector/state"
	defaultOutputDir                    = "/var/lib/doublezero-internet-latency-collector/output"
	defaultAtlasProbesPerLocation       = 2
	defaultWheresitupCollectionInterval = 2 * time.Minute
	defaultWheresitupStateFile          = "wheresitup_jobs_to_process.json"
	defaultLogLevel                     = "info"
)

var (
	stateDir                     string
	outputDir                    string
	logLevel                     string
	locationFile                 string
	dryRun                       bool
	wheresitupStateFile          string
	ripeatlasProbesPerLocation   int
	wheresitupCollectionInterval time.Duration

	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "doublezero-internet-latency-collector",
	Short: "DoubleZero internet latency collector",
	Long: `DoubleZero collector gathers internet latency data from RIPE Atlas
and Wheresitup services for the DoubleZero network.`,
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
			slog.String("wheresitup_interval", wheresitupCollectionInterval.String()),
			slog.Bool("dry_run", dryRun))

		ripeatlasCollector := ripeatlas.NewCollector(log)

		wheresitupCollector := wheresitup.NewCollector(log)

		config := collector.Config{
			Logger:     log,
			Wheresitup: wheresitupCollector,
			RipeAtlas:  ripeatlasCollector,

			WheresitupCollectionInterval: wheresitupCollectionInterval,
			RipeAtlasMeasurementInterval: 1 * time.Hour,   // Create measurements hourly
			RipeAtlasExportInterval:      2 * time.Minute, // Export results every 2 minutes
			DryRun:                       dryRun,
			ProcessedJobsFile:            wheresitupStateFile,
			StateDir:                     stateDir,
			OutputDir:                    outputDir,
			ProbesPerLocation:            ripeatlasProbesPerLocation,
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		c, err := collector.New(config)
		if err != nil {
			log.Error("Operation failed: new_collector", "error", err)
			os.Exit(1)
		}
		if err := c.Run(ctx); err != nil {
			log.Error("Operation failed: run_continuous_collector", "error", err)
			os.Exit(1)
		}
		log.Info("Operation completed: run_continuous_collector")
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

		locations := loadLocations(ctx, log)
		if locations == nil {
			return
		}

		ripeCollector := ripeatlas.NewCollector(log)

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

		ripeCollector := ripeatlas.NewCollector(log)

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
		ripeCollector := ripeatlas.NewCollector(log)

		if err := ripeCollector.RunRipeAtlasMeasurementCreation(context.Background(), dryRun, ripeatlasProbesPerLocation, outputDir, stateDir); err != nil {
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

		ripeCollector := ripeatlas.NewCollector(log)

		if err := ripeCollector.ClearAllMeasurements(context.Background()); err != nil {
			log.Error("Operation failed: clear_ripeatlas_measurements", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: clear_ripeatlas_measurements")
	},
}

var ripeatlasExportMeasurementsCmd = &cobra.Command{
	Use:   "export-measurements",
	Short: "Export RIPE Atlas measurement results to CSV",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))
		log.Info("Operation started: export_ripeatlas_measurements",
			slog.String("state_dir", stateDir),
			slog.String("output_dir", outputDir))

		ripeCollector := ripeatlas.NewCollector(log)

		if err := ripeCollector.ExportMeasurementResults(context.Background(), stateDir, outputDir); err != nil {
			log.Error("Operation failed: export_ripeatlas_measurements", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: export_ripeatlas_measurements")
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

		locations := loadLocations(ctx, log)
		if locations == nil {
			return
		}

		wheresitupCollector := wheresitup.NewCollector(log)

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

		wheresitupCollector := wheresitup.NewCollector(log)

		if err := wheresitupCollector.ListJobs(context.Background()); err != nil {
			log.Error("Operation failed: list_wheresitup_jobs", slog.String("error", err.Error()))
			os.Exit(1)
		}
		log.Info("Operation completed: list_wheresitup_jobs")
	},
}

func loadLocations(ctx context.Context, logger *slog.Logger) []collector.LocationMatch {
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
		return collector.GetLocations(ctx, logger)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&stateDir, "state-dir", defaultStateDir, "Directory to store state files (timestamps, processed job IDs)")
	rootCmd.PersistentFlags().StringVar(&outputDir, "output-dir", defaultOutputDir, "Directory to store output files (measurement results)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", defaultLogLevel, "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&locationFile, "location-file", "", "CSV file containing locations (LocationCode,Latitude,Longitude)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Log what would be created without actually creating measurements or jobs")

	runCmd.Flags().DurationVar(&wheresitupCollectionInterval, "wheresitup-collection-interval", defaultWheresitupCollectionInterval, "Interval for continuous Wheresitup job creation (e.g., 2m, 1h30m)")
	runCmd.Flags().StringVar(&wheresitupStateFile, "wheresitup-job-state-file", defaultWheresitupStateFile, "File to track processed Wheresitup job IDs (JSON format)")
	runCmd.Flags().IntVar(&ripeatlasProbesPerLocation, "ripeatlas-probes-per-location", defaultAtlasProbesPerLocation, "Number of RIPE Atlas probes to associate with each DoubleZero location")

	ripeatlasCreateMeasurementsCmd.Flags().IntVar(&ripeatlasProbesPerLocation, "probes-per-location", defaultAtlasProbesPerLocation, "Number of RIPE Atlas probes to associate with each DoubleZero location")

	cobra.EnableCommandSorting = false

	rootCmd.AddCommand(ripeatlasCmd)
	rootCmd.AddCommand(wheresitupCmd)
	rootCmd.AddCommand(runCmd)

	ripeatlasCmd.AddCommand(ripeatlasListProbesCmd)
	ripeatlasCmd.AddCommand(ripeatlasListMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasCreateMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasClearMeasurementsCmd)
	ripeatlasCmd.AddCommand(ripeatlasExportMeasurementsCmd)

	wheresitupCmd.AddCommand(wheresitupListSourcesCmd)
	wheresitupCmd.AddCommand(wheresitupListJobsCmd)
}

func main() {
	// Add version command last so it appears after auto-generated commands
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
