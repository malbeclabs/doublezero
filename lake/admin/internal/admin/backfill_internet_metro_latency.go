package admin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/malbeclabs/doublezero/config"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
)

// BackfillInternetMetroLatencyConfig holds the configuration for the backfill command
type BackfillInternetMetroLatencyConfig struct {
	StartEpoch     int64 // -1 means auto-calculate from EndEpoch
	EndEpoch       int64 // -1 means use current epoch - 1
	MaxConcurrency int
	DryRun         bool
}

// BackfillInternetMetroLatency backfills internet metro latency data for a range of epochs
func BackfillInternetMetroLatency(
	log *slog.Logger,
	clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword string,
	clickhouseSecure bool,
	dzEnv string,
	cfg BackfillInternetMetroLatencyConfig,
) error {
	ctx := context.Background()

	// Get network config
	networkConfig, err := config.NetworkConfigForEnv(dzEnv)
	if err != nil {
		return fmt.Errorf("failed to get network config for env %q: %w", dzEnv, err)
	}

	// Connect to ClickHouse
	chDB, err := clickhouse.NewClient(ctx, log, clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword, clickhouseSecure)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer chDB.Close()

	// Create RPC clients
	dzRPCClient := rpc.NewWithRetries(networkConfig.LedgerPublicRPCURL, nil)
	defer dzRPCClient.Close()

	telemetryClient := telemetry.New(log, dzRPCClient, nil, networkConfig.TelemetryProgramID)

	// Create store early to query data boundaries
	store, err := dztelemlatency.NewStore(dztelemlatency.StoreConfig{
		Logger:     log,
		ClickHouse: chDB,
	})
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	// Query existing data boundaries
	bounds, err := store.GetInternetMetroLatencyBoundaries(ctx)
	if err != nil {
		fmt.Printf("Warning: failed to query data boundaries: %v\n", err)
	}

	// Get current epoch to determine range
	epochInfo, err := dzRPCClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	// Determine epoch range
	latestCompletedEpoch := int64(currentEpoch) - 1
	if latestCompletedEpoch < 0 {
		latestCompletedEpoch = 0
	}

	startEpoch := cfg.StartEpoch
	endEpoch := cfg.EndEpoch

	// Auto-calculate range if not specified
	if startEpoch < 0 && endEpoch < 0 {
		if bounds != nil && bounds.MaxEpoch != nil && bounds.MinEpoch != nil {
			// We have existing data
			if *bounds.MaxEpoch >= latestCompletedEpoch {
				// Data is up-to-date, backfill older epochs (before what we have)
				endEpoch = *bounds.MinEpoch - 1
				startEpoch = endEpoch - defaultBackfillEpochCount + 1
			} else {
				// Data is not up-to-date, continue from where we left off
				startEpoch = *bounds.MaxEpoch + 1
				endEpoch = latestCompletedEpoch
			}
		} else {
			// No existing data, use default lookback from current
			endEpoch = latestCompletedEpoch
			startEpoch = endEpoch - defaultBackfillEpochCount + 1
		}
	} else if endEpoch < 0 {
		endEpoch = latestCompletedEpoch
	} else if startEpoch < 0 {
		startEpoch = endEpoch - defaultBackfillEpochCount + 1
	}

	if startEpoch < 0 {
		startEpoch = 0
	}
	if endEpoch < 0 {
		endEpoch = 0
	}

	// Check if there's nothing to backfill
	if startEpoch > endEpoch {
		fmt.Printf("Backfill Internet Metro Latency\n")
		fmt.Printf("  Environment:      %s\n", dzEnv)
		fmt.Printf("  Current epoch:    %d (in progress)\n", currentEpoch)
		fmt.Printf("  Completed epochs: up to %d\n", latestCompletedEpoch)
		fmt.Println()
		fmt.Printf("Existing Data in ClickHouse:\n")
		if bounds != nil && bounds.RowCount > 0 {
			fmt.Printf("  Row count:        %d\n", bounds.RowCount)
			if bounds.MinTime != nil {
				fmt.Printf("  Time range:       %s - %s\n", bounds.MinTime.Format(time.RFC3339), bounds.MaxTime.Format(time.RFC3339))
			}
			if bounds.MinEpoch != nil {
				fmt.Printf("  Epoch range:      %d - %d\n", *bounds.MinEpoch, *bounds.MaxEpoch)
			}
		}
		fmt.Println()
		fmt.Printf("No epochs available to backfill.\n")
		fmt.Printf("To backfill specific epochs, use --start-epoch and --end-epoch flags.\n")
		return nil
	}

	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = defaultBackfillMaxConcurrency
	}

	fmt.Printf("Backfill Internet Metro Latency\n")
	fmt.Printf("  Environment:      %s\n", dzEnv)
	fmt.Printf("  Current epoch:    %d\n", currentEpoch)
	fmt.Printf("  Epoch range:      %d - %d (%d epochs)\n", startEpoch, endEpoch, endEpoch-startEpoch+1)
	fmt.Printf("  Max concurrency:  %d\n", maxConcurrency)
	fmt.Printf("  Data providers:   %v\n", telemetryconfig.InternetTelemetryDataProviders)
	fmt.Printf("  Agent PK:         %s\n", networkConfig.InternetLatencyCollectorPK.String())
	fmt.Printf("  Dry run:          %v\n", cfg.DryRun)
	fmt.Println()

	fmt.Printf("Existing Data in ClickHouse:\n")
	if bounds != nil && bounds.RowCount > 0 {
		fmt.Printf("  Row count:        %d\n", bounds.RowCount)
		if bounds.MinTime != nil {
			fmt.Printf("  Time range:       %s - %s\n", bounds.MinTime.Format(time.RFC3339), bounds.MaxTime.Format(time.RFC3339))
		}
		if bounds.MinEpoch != nil {
			fmt.Printf("  Epoch range:      %d - %d\n", *bounds.MinEpoch, *bounds.MaxEpoch)
		}
	} else {
		fmt.Printf("  (no existing data)\n")
	}
	fmt.Println()

	// Query current metros from ClickHouse
	metros, err := dzsvc.QueryCurrentMetros(ctx, log, chDB)
	if err != nil {
		return fmt.Errorf("failed to query metros: %w", err)
	}
	fmt.Printf("Found %d metros\n", len(metros))

	// Generate metro pairs
	metroPairs := dztelemlatency.GenerateMetroPairs(metros)
	fmt.Printf("Generated %d metro pairs\n", len(metroPairs))

	if cfg.DryRun {
		fmt.Println("[DRY RUN] Would fetch and insert samples for the above configuration")
		return nil
	}

	var totalSamples int64

	// Process epochs one at a time for better progress visibility
	for e := startEpoch; e <= endEpoch; e++ {
		epoch := uint64(e)
		fmt.Printf("Processing epoch %d...\n", epoch)

		result, err := store.BackfillInternetMetroLatencyForEpoch(
			ctx,
			telemetryClient,
			metroPairs,
			telemetryconfig.InternetTelemetryDataProviders,
			networkConfig.InternetLatencyCollectorPK,
			epoch,
			maxConcurrency,
		)
		if err != nil {
			return err
		}

		totalSamples += int64(result.SampleCount)
		if result.SampleCount > 0 {
			fmt.Printf("  Epoch %d: inserted %d samples\n", epoch, result.SampleCount)
		} else {
			fmt.Printf("  Epoch %d: no samples found\n", epoch)
		}
	}

	fmt.Printf("\nBackfill completed: %d total samples inserted\n", totalSamples)
	return nil
}
