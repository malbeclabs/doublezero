package admin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
)

const (
	defaultUsageBackfillDays     = 1
	defaultUsageBackfillInterval = 1 * time.Hour   // Process in 1-hour chunks
	defaultUsageQueryDelay       = 5 * time.Second // Delay between InfluxDB queries to avoid rate limits
)

// BackfillDeviceInterfaceCountersConfig holds the configuration for the backfill command
type BackfillDeviceInterfaceCountersConfig struct {
	StartTime     time.Time // Zero means auto-calculate from EndTime
	EndTime       time.Time // Zero means use now
	ChunkInterval time.Duration
	QueryDelay    time.Duration // Delay between InfluxDB queries
	DryRun        bool
}

// BackfillDeviceInterfaceCounters backfills device interface counters data for a time range
func BackfillDeviceInterfaceCounters(
	log *slog.Logger,
	clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword string,
	clickhouseSecure bool,
	influxDBHost, influxDBToken, influxDBBucket string,
	cfg BackfillDeviceInterfaceCountersConfig,
) error {
	ctx := context.Background()

	// Connect to ClickHouse
	chDB, err := clickhouse.NewClient(ctx, log, clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword, clickhouseSecure)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer chDB.Close()

	// Connect to InfluxDB
	influxClient, err := dztelemusage.NewSDKInfluxDBClient(influxDBHost, influxDBToken, influxDBBucket)
	if err != nil {
		return fmt.Errorf("failed to connect to InfluxDB: %w", err)
	}
	defer influxClient.Close()

	// Query existing data boundaries from ClickHouse first
	store, err := dztelemusage.NewStore(dztelemusage.StoreConfig{
		Logger:     log,
		ClickHouse: chDB,
	})
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	bounds, err := store.GetDataBoundaries(ctx)
	if err != nil {
		fmt.Printf("Warning: failed to query data boundaries: %v\n", err)
	}

	// Determine time range
	now := time.Now().UTC()
	defaultBackfillDuration := time.Duration(defaultUsageBackfillDays) * 24 * time.Hour
	// Consider data "up-to-date" if it's within 1 hour of now
	recentThreshold := now.Add(-1 * time.Hour)

	startTime := cfg.StartTime
	endTime := cfg.EndTime

	// Auto-calculate range if not specified
	if startTime.IsZero() && endTime.IsZero() {
		if bounds != nil && bounds.MaxTime != nil && bounds.MinTime != nil {
			// We have existing data
			if bounds.MaxTime.After(recentThreshold) {
				// Data is up-to-date, backfill older data (before what we have)
				endTime = *bounds.MinTime
				startTime = endTime.Add(-defaultBackfillDuration)
			} else {
				// Data is not up-to-date, continue from where we left off
				startTime = *bounds.MaxTime
				endTime = now
			}
		} else {
			// No existing data, use default lookback from now
			endTime = now
			startTime = endTime.Add(-defaultBackfillDuration)
		}
	} else if endTime.IsZero() {
		endTime = now
	} else if startTime.IsZero() {
		startTime = endTime.Add(-defaultBackfillDuration)
	}

	// Check if there's nothing to backfill
	if !startTime.Before(endTime) {
		fmt.Printf("Backfill Device Interface Counters\n")
		fmt.Printf("  InfluxDB host:   %s\n", influxDBHost)
		fmt.Printf("  InfluxDB bucket: %s\n", influxDBBucket)
		fmt.Println()
		fmt.Printf("Existing Data in ClickHouse:\n")
		if bounds != nil && bounds.RowCount > 0 {
			fmt.Printf("  Row count:       %d\n", bounds.RowCount)
			if bounds.MinTime != nil {
				fmt.Printf("  Time range:      %s - %s\n", bounds.MinTime.Format(time.RFC3339), bounds.MaxTime.Format(time.RFC3339))
			}
		}
		fmt.Println()
		fmt.Printf("No time range available to backfill.\n")
		fmt.Printf("To backfill specific time range, use --start-time and --end-time flags.\n")
		return nil
	}

	chunkInterval := cfg.ChunkInterval
	if chunkInterval <= 0 {
		chunkInterval = defaultUsageBackfillInterval
	}

	queryDelay := cfg.QueryDelay
	if queryDelay <= 0 {
		queryDelay = defaultUsageQueryDelay
	}

	fmt.Printf("Backfill Device Interface Counters\n")
	fmt.Printf("  Time range:      %s - %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	fmt.Printf("  Duration:        %s\n", endTime.Sub(startTime))
	fmt.Printf("  Chunk interval:  %s\n", chunkInterval)
	fmt.Printf("  Query delay:     %s\n", queryDelay)
	fmt.Printf("  InfluxDB host:   %s\n", influxDBHost)
	fmt.Printf("  InfluxDB bucket: %s\n", influxDBBucket)
	fmt.Printf("  Dry run:         %v\n", cfg.DryRun)
	fmt.Println()

	fmt.Printf("Existing Data in ClickHouse:\n")
	if bounds != nil && bounds.RowCount > 0 {
		fmt.Printf("  Row count:       %d\n", bounds.RowCount)
		if bounds.MinTime != nil {
			fmt.Printf("  Time range:      %s - %s\n", bounds.MinTime.Format(time.RFC3339), bounds.MaxTime.Format(time.RFC3339))
		}
	} else {
		fmt.Printf("  (no existing data)\n")
	}
	fmt.Println()

	if cfg.DryRun {
		fmt.Println("[DRY RUN] Would fetch and insert data for the above configuration")
		return nil
	}

	// Create view for backfill operations
	view, err := dztelemusage.NewView(dztelemusage.ViewConfig{
		Logger:          log,
		InfluxDB:        influxClient,
		Bucket:          influxDBBucket,
		ClickHouse:      chDB,
		RefreshInterval: 1 * time.Hour, // Not used for backfill
		QueryWindow:     1 * time.Hour, // Not used for backfill
	})
	if err != nil {
		return fmt.Errorf("failed to create view: %w", err)
	}

	var totalRowsQueried, totalRowsInserted int64

	// Process in chunks for better progress visibility and memory management
	chunkStart := startTime
	isFirstChunk := true
	for chunkStart.Before(endTime) {
		// Throttle queries to avoid hitting InfluxDB rate limits (skip delay for first chunk)
		if !isFirstChunk && queryDelay > 0 {
			time.Sleep(queryDelay)
		}
		isFirstChunk = false

		chunkEnd := chunkStart.Add(chunkInterval)
		if chunkEnd.After(endTime) {
			chunkEnd = endTime
		}

		fmt.Printf("Processing %s - %s...\n", chunkStart.Format(time.RFC3339), chunkEnd.Format(time.RFC3339))

		result, err := view.BackfillForTimeRange(ctx, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("failed to backfill chunk %s - %s: %w", chunkStart, chunkEnd, err)
		}

		totalRowsQueried += int64(result.RowsQueried)
		totalRowsInserted += int64(result.RowsInserted)

		if result.RowsInserted > 0 {
			fmt.Printf("  Queried %d rows, inserted %d rows\n", result.RowsQueried, result.RowsInserted)
		} else {
			fmt.Printf("  No data found\n")
		}

		chunkStart = chunkEnd
	}

	fmt.Printf("\nBackfill completed: queried %d total rows, inserted %d total rows\n", totalRowsQueried, totalRowsInserted)
	return nil
}
