package admin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/clickhouse"
	dztelemusage "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/usage"
)

const (
	defaultUsageBackfillDays     = 7
	defaultUsageBackfillInterval = 1 * time.Hour // Process in 1-hour chunks
)

// BackfillDeviceIfaceUsageConfig holds the configuration for the backfill command
type BackfillDeviceIfaceUsageConfig struct {
	StartTime     time.Time // Zero means auto-calculate from EndTime
	EndTime       time.Time // Zero means use now
	ChunkInterval time.Duration
	DryRun        bool
}

// BackfillDeviceIfaceUsage backfills device interface usage data for a time range
func BackfillDeviceIfaceUsage(
	log *slog.Logger,
	clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword string,
	influxDBHost, influxDBToken, influxDBBucket string,
	cfg BackfillDeviceIfaceUsageConfig,
) error {
	ctx := context.Background()

	// Connect to ClickHouse
	chDB, err := clickhouse.NewClient(ctx, log, clickhouseAddr, clickhouseDatabase, clickhouseUsername, clickhousePassword)
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

	// Determine time range
	endTime := cfg.EndTime
	if endTime.IsZero() {
		endTime = time.Now().UTC()
	}

	startTime := cfg.StartTime
	if startTime.IsZero() {
		startTime = endTime.Add(-time.Duration(defaultUsageBackfillDays) * 24 * time.Hour)
	}

	if startTime.After(endTime) {
		return fmt.Errorf("start time (%s) must be before end time (%s)", startTime, endTime)
	}

	chunkInterval := cfg.ChunkInterval
	if chunkInterval <= 0 {
		chunkInterval = defaultUsageBackfillInterval
	}

	fmt.Printf("Backfill Device Interface Usage\n")
	fmt.Printf("  Time range:      %s - %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	fmt.Printf("  Duration:        %s\n", endTime.Sub(startTime))
	fmt.Printf("  Chunk interval:  %s\n", chunkInterval)
	fmt.Printf("  InfluxDB host:   %s\n", influxDBHost)
	fmt.Printf("  InfluxDB bucket: %s\n", influxDBBucket)
	fmt.Printf("  Dry run:         %v\n", cfg.DryRun)
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
	for chunkStart.Before(endTime) {
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
