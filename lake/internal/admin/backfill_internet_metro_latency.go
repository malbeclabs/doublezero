package admin

import (
	"context"
	"fmt"
	"log/slog"

	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/malbeclabs/doublezero/config"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/latency"
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
	chDB, err := clickhouse.NewClient(ctx, log, clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer chDB.Close()

	// Create RPC clients
	dzRPCClient := rpc.NewWithRetries(networkConfig.LedgerPublicRPCURL, nil)
	defer dzRPCClient.Close()

	telemetryClient := telemetry.New(log, dzRPCClient, nil, networkConfig.TelemetryProgramID)

	// Get current epoch to determine range
	epochInfo, err := dzRPCClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	// Determine epoch range
	endEpoch := cfg.EndEpoch
	if endEpoch < 0 {
		endEpoch = int64(currentEpoch) - 1
	}
	if endEpoch < 0 {
		endEpoch = 0
	}

	startEpoch := cfg.StartEpoch
	if startEpoch < 0 {
		startEpoch = endEpoch - defaultBackfillEpochCount + 1
	}
	if startEpoch < 0 {
		startEpoch = 0
	}

	if startEpoch > endEpoch {
		return fmt.Errorf("start epoch (%d) must be <= end epoch (%d)", startEpoch, endEpoch)
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
		fmt.Println("\n[DRY RUN] Would fetch and insert samples for the above configuration")
		return nil
	}

	// Create store for writing
	store, err := dztelemlatency.NewStore(dztelemlatency.StoreConfig{
		Logger:     log,
		ClickHouse: chDB,
	})
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
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
