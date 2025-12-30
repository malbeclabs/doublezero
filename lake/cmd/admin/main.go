package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/sol"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	verboseFlag := flag.Bool("verbose", false, "enable verbose (debug) logging")

	// Database configuration
	duckLakeCatalogNameFlag := flag.String("ducklake-catalog-name", "dzlake", "Name of the DuckLake catalog (or set DUCKLAKE_CATALOG_NAME env var)")
	duckLakeCatalogURIFlag := flag.String("ducklake-catalog-uri", "file://.tmp/lake/catalog.sqlite", "URI to the DuckLake catalog (or set DUCKLAKE_CATALOG_URI env var)")
	duckLakeStorageURIFlag := flag.String("ducklake-storage-uri", "file://.tmp/lake/data", "URI to the DuckLake storage directory (or set DUCKLAKE_STORAGE_URI env var)")

	// Command selection
	mergeFilesFlag := flag.Bool("merge-adjacent-files", false, "Merge adjacent parquet files to optimize storage")
	rewriteFilesFlag := flag.Bool("rewrite-data-files", false, "Rewrite data files with high delete ratios")
	tableNameFlag := flag.String("table", "", "Table name (optional for rewrite-data-files, if not provided all tables will be rewritten)")
	deleteThresholdFlag := flag.Float64("delete-threshold", 0.95, "Delete threshold for rewrite-data-files (default: 0.95, meaning files with >95% deleted rows)")

	cleanupOldFilesFlag := flag.Bool("cleanup-old-files", false, "Cleanup old files")
	deleteOrphanedFilesFlag := flag.Bool("delete-orphaned-files", false, "Delete orphaned files")
	expireSnapshotsFlag := flag.Bool("expire-snapshots", false, "Expire snapshots")
	flushInlinedDataFlag := flag.Bool("flush-inlined-data", false, "Flush inlined data to storage")
	checkpointFlag := flag.Bool("checkpoint", false, "Create a checkpoint")
	setExpireOlderThanFlag := flag.String("set-expire-older-than", "", "Set ducklake option expire_older_than (e.g., '30d', '7 days', '1 week')")
	execSQLFlag := flag.String("exec-sql", "", "Execute arbitrary SQL query (use --exec-sql-file for file input)")
	execSQLFileFlag := flag.String("exec-sql-file", "", "Execute SQL from file")
	backfillValidToOnDeletesFlag := flag.Bool("backfill-valid-to-on-deletes", false, "Backfill valid_to field for SCD2 history rows where it wasn't set on delete")

	allFlag := flag.Bool("all", false, "Apply to all items (for use with --cleanup-old-files or --delete-orphaned-files)")
	olderThanFlag := flag.String("older-than", "", "Older than this interval (e.g., '7 days', '1 week', '30 days', for use with --cleanup-old-files, --delete-orphaned-files, or --expire-snapshots)")
	dryRunFlag := flag.Bool("dry-run", false, "Dry run mode - show what would be done without actually executing (for use with --cleanup-old-files, --delete-orphaned-files, or --expire-snapshots)")

	flag.Parse()

	// Override flags with environment variables if set
	if envCatalogURI := os.Getenv("DUCKLAKE_CATALOG_URI"); envCatalogURI != "" {
		*duckLakeCatalogURIFlag = envCatalogURI
	}
	if envStorageURI := os.Getenv("DUCKLAKE_STORAGE_URI"); envStorageURI != "" {
		*duckLakeStorageURIFlag = envStorageURI
	}
	if envCatalogName := os.Getenv("DUCKLAKE_CATALOG_NAME"); envCatalogName != "" {
		*duckLakeCatalogNameFlag = envCatalogName
	}

	log := logger.New(*verboseFlag)

	// Check that at least one command is specified
	if !*mergeFilesFlag && !*rewriteFilesFlag && !*cleanupOldFilesFlag && !*deleteOrphanedFilesFlag && !*expireSnapshotsFlag && !*flushInlinedDataFlag && !*checkpointFlag && *setExpireOlderThanFlag == "" && *execSQLFlag == "" && *execSQLFileFlag == "" && !*backfillValidToOnDeletesFlag {
		return fmt.Errorf("must specify at least one command: --merge-adjacent-files, --rewrite-data-files, --cleanup-old-files, --delete-orphaned-files, --expire-snapshots, --flush-inlined-data, --checkpoint, --set-expire-older-than, --exec-sql, --exec-sql-file, or --backfill-valid-to-on-deletes")
	}

	// Validate SQL execution flags
	if *execSQLFlag != "" && *execSQLFileFlag != "" {
		return fmt.Errorf("--exec-sql and --exec-sql-file cannot be used together")
	}

	// Validate cleanup old files flags
	if *cleanupOldFilesFlag {
		if !*allFlag && *olderThanFlag == "" {
			return fmt.Errorf("--cleanup-old-files requires either --all or --older-than")
		}
		if *allFlag && *olderThanFlag != "" {
			return fmt.Errorf("--all and --older-than cannot be used together")
		}
	}

	// Validate orphaned files flags
	if *deleteOrphanedFilesFlag {
		if !*allFlag && *olderThanFlag == "" {
			return fmt.Errorf("--delete-orphaned-files requires either --all or --older-than")
		}
		if *allFlag && *olderThanFlag != "" {
			return fmt.Errorf("--all and --older-than cannot be used together")
		}
	}

	// Validate expire snapshots flags
	if *expireSnapshotsFlag {
		if *olderThanFlag == "" {
			return fmt.Errorf("--expire-snapshots requires --older-than")
		}
	}

	ctx := context.Background()

	// Prepare S3 config if using S3 storage
	s3Config, err := duck.PrepareS3ConfigForStorageURI(ctx, log, *duckLakeStorageURIFlag)
	if err != nil {
		return fmt.Errorf("failed to prepare S3 config: %w", err)
	}

	// Create lake connection
	log.Info("initializing ducklake database", "catalog", *duckLakeCatalogNameFlag, "catalogURI", duck.RedactedCatalogURI(*duckLakeCatalogURIFlag), "storageURI", duck.RedactedStorageURI(*duckLakeStorageURIFlag))
	lake, err := duck.NewLake(ctx, log, *duckLakeCatalogNameFlag, *duckLakeCatalogURIFlag, *duckLakeStorageURIFlag, false, s3Config)
	if err != nil {
		return fmt.Errorf("failed to create lake: %w", err)
	}
	defer lake.Close()

	conn, err := lake.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Execute requested commands
	if *mergeFilesFlag {
		if err := lake.MergeAdjacentFiles(ctx); err != nil {
			return fmt.Errorf("failed to merge adjacent files: %w", err)
		}
		log.Info("successfully merged adjacent files")
	}

	if *rewriteFilesFlag {
		if *tableNameFlag != "" {
			// Rewrite specific table
			if err := lake.RewriteDataFilesForTable(ctx, *tableNameFlag, *deleteThresholdFlag); err != nil {
				return fmt.Errorf("failed to rewrite data files: %w", err)
			}
		} else {
			// Rewrite all tables
			if err := lake.RewriteDataFiles(ctx, *deleteThresholdFlag); err != nil {
				return fmt.Errorf("failed to rewrite data files: %w", err)
			}
		}
	}

	if *cleanupOldFilesFlag {
		opts := duck.CleanupOptions{
			CleanupAll: *allFlag,
			DryRun:     *dryRunFlag,
		}
		if *olderThanFlag != "" {
			olderThanDuration, err := parseIntervalToDuration(*olderThanFlag)
			if err != nil {
				return fmt.Errorf("failed to parse older_than interval: %w", err)
			}
			opts.OlderThan = olderThanDuration
		}
		if !opts.CleanupAll && opts.OlderThan == 0 {
			return fmt.Errorf("--cleanup-old-files requires either --all or --older-than")
		}
		if err := lake.CleanupOldFiles(ctx, opts); err != nil {
			return fmt.Errorf("failed to cleanup old files: %w", err)
		}
	}

	if *deleteOrphanedFilesFlag {
		opts := duck.DeleteOrphanedOptions{
			CleanupAll: *allFlag,
			DryRun:     *dryRunFlag,
		}
		if *olderThanFlag != "" {
			olderThanDuration, err := parseIntervalToDuration(*olderThanFlag)
			if err != nil {
				return fmt.Errorf("failed to parse older_than interval: %w", err)
			}
			opts.OlderThan = olderThanDuration
		}
		if !opts.CleanupAll && opts.OlderThan == 0 {
			return fmt.Errorf("--delete-orphaned-files requires either --all or --older-than")
		}
		if err := lake.DeleteOrphanedFiles(ctx, opts); err != nil {
			return fmt.Errorf("failed to delete orphaned files: %w", err)
		}
	}

	if *expireSnapshotsFlag {
		opts := duck.ExpireSnapshotsOptions{
			DryRun: *dryRunFlag,
		}
		if *olderThanFlag != "" {
			olderThanDuration, err := parseIntervalToDuration(*olderThanFlag)
			if err != nil {
				return fmt.Errorf("failed to parse older_than interval: %w", err)
			}
			opts.OlderThan = olderThanDuration
		}

		result, err := lake.ExpireSnapshots(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to expire snapshots: %w", err)
		}

		// Display dry-run results if applicable
		if *dryRunFlag && result != nil {
			// Display summary and description
			fmt.Println("\n" + strings.Repeat("=", 100))
			fmt.Println("SNAPSHOTS THAT WOULD BE EXPIRED")
			fmt.Println(strings.Repeat("=", 100))
			fmt.Println("\nNote: Snapshots are point-in-time views of your data lake tables.")
			fmt.Println("      Expiring old snapshots removes historical versions but keeps current data.")

			// Show date range if available
			if result.EarliestTime != nil && result.LatestTime != nil {
				fmt.Printf("\nDate range: %s to %s\n",
					result.EarliestTime.Format("2006-01-02 15:04:05 UTC"),
					result.LatestTime.Format("2006-01-02 15:04:05 UTC"))
				if len(result.Snapshots) > 1 {
					duration := result.LatestTime.Sub(*result.EarliestTime)
					fmt.Printf("Time span: %s\n", formatDuration(duration))
				}
			}

			// Only show individual snapshots if verbose is enabled
			if *verboseFlag {
				fmt.Println(strings.Repeat("-", 100))
				for i, snap := range result.Snapshots {
					fmt.Printf("\n[%d] Snapshot ID: %s | Time: %s | Schema: %s\n",
						i+1, snap.ID, snap.Time, snap.SchemaVersion)
					if snap.Changes != "" {
						fmt.Printf("     Changes: %s\n", snap.Changes)
					}
				}
			}

			fmt.Println(strings.Repeat("-", 100))
			fmt.Printf("\nTotal: %d snapshot(s) would be expired", len(result.Snapshots))
			if !*verboseFlag {
				fmt.Printf(" (use --verbose to see details)")
			}
			fmt.Println()
			log.Info("dry run completed", "snapshots_that_would_be_expired", len(result.Snapshots))
		} else {
			log.Info("successfully expired snapshots")
		}
	}

	if *flushInlinedDataFlag {
		if err := lake.FlushInlinedData(ctx); err != nil {
			return fmt.Errorf("failed to flush inlined data: %w", err)
		}
	}

	if *checkpointFlag {
		var expireOlderThan time.Duration
		if *expireSnapshotsFlag && *olderThanFlag != "" {
			var err error
			expireOlderThan, err = parseIntervalToDuration(*olderThanFlag)
			if err != nil {
				return fmt.Errorf("failed to parse older_than interval: %w", err)
			}
		}
		if err := lake.Checkpoint(ctx, expireOlderThan); err != nil {
			return fmt.Errorf("failed to run checkpoint: %w", err)
		}
	}

	if *setExpireOlderThanFlag != "" {
		catalogName := lake.Catalog()
		log.Info("setting expire_older_than option", "value", *setExpireOlderThanFlag, "catalog", catalogName)
		setOptionSQL := fmt.Sprintf("CALL %s.set_option('expire_older_than', '%s')", catalogName, *setExpireOlderThanFlag)
		if _, err := conn.ExecContext(ctx, setOptionSQL); err != nil {
			return fmt.Errorf("failed to set expire_older_than option: %w", err)
		}
		log.Info("successfully set expire_older_than option", "value", *setExpireOlderThanFlag, "catalog", catalogName)
	}

	if *execSQLFlag != "" || *execSQLFileFlag != "" {
		var sql string
		if *execSQLFileFlag != "" {
			file, err := os.Open(*execSQLFileFlag)
			if err != nil {
				return fmt.Errorf("failed to open SQL file: %w", err)
			}
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil {
				return fmt.Errorf("failed to read SQL file: %w", err)
			}
			sql = string(content)
			log.Info("executing SQL from file", "file", *execSQLFileFlag)
		} else {
			sql = *execSQLFlag
			log.Info("executing SQL", "sql", sql)
		}

		// Try to determine if it's a query (SELECT) or exec (INSERT/UPDATE/DELETE/CALL/etc)
		sqlUpper := strings.ToUpper(strings.TrimSpace(sql))
		isQuery := strings.HasPrefix(sqlUpper, "SELECT") || strings.HasPrefix(sqlUpper, "WITH") || strings.HasPrefix(sqlUpper, "SHOW") || strings.HasPrefix(sqlUpper, "DESCRIBE") || strings.HasPrefix(sqlUpper, "DESC")

		if isQuery {
			rows, err := conn.QueryContext(ctx, sql)
			if err != nil {
				return fmt.Errorf("failed to execute SQL query: %w", err)
			}
			defer rows.Close()

			columns, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("failed to get columns: %w", err)
			}

			// Print column headers
			fmt.Println(strings.Join(columns, "\t"))

			// Print rows
			rowCount := 0
			for rows.Next() {
				values := make([]any, len(columns))
				valuePtrs := make([]any, len(columns))
				for i := range values {
					valuePtrs[i] = &values[i]
				}

				if err := rows.Scan(valuePtrs...); err != nil {
					return fmt.Errorf("failed to scan row: %w", err)
				}

				var rowValues []string
				for _, val := range values {
					var valStr string
					if val == nil {
						valStr = "NULL"
					} else {
						valStr = fmt.Sprintf("%v", val)
					}
					rowValues = append(rowValues, valStr)
				}
				fmt.Println(strings.Join(rowValues, "\t"))
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return fmt.Errorf("error iterating rows: %w", err)
			}

			log.Info("SQL query completed", "rows", rowCount)
		} else {
			result, err := conn.ExecContext(ctx, sql)
			if err != nil {
				return fmt.Errorf("failed to execute SQL: %w", err)
			}
			rowsAffected, _ := result.RowsAffected()
			log.Info("SQL executed successfully", "rows_affected", rowsAffected)
		}
	}

	if *backfillValidToOnDeletesFlag {
		// Get all SCD table configs
		configs := getAllSCDTableConfigs()

		if *dryRunFlag {
			log.Info("running backfill valid_to on deletes in dry-run mode - no changes will be made")
		}

		totalFixed := 0
		for _, cfg := range configs {
			log.Info("processing table for backfill", "table", cfg.TableBaseName, "dry_run", *dryRunFlag)

			fixedCount, err := duck.BackfillValidToOnDeletes(ctx, log, conn, cfg, *dryRunFlag)
			if err != nil {
				return fmt.Errorf("failed to backfill table %s: %w", cfg.TableBaseName, err)
			}

			if *dryRunFlag {
				log.Info("dry run completed for table",
					"table", cfg.TableBaseName,
					"rows_to_fix", fixedCount)
			} else {
				log.Info("backfill completed for table",
					"table", cfg.TableBaseName,
					"rows_fixed", fixedCount)
			}

			totalFixed += fixedCount
		}

		if *dryRunFlag {
			log.Info("dry run completed for all tables", "total_rows_to_fix", totalFixed)
			fmt.Printf("\nDry run completed. Total rows that would be fixed: %d\n", totalFixed)
			fmt.Println("Run without --dry-run to apply the changes.")
		} else {
			log.Info("backfill completed for all tables", "total_rows_fixed", totalFixed)
			fmt.Printf("\nBackfill completed. Total rows fixed: %d\n", totalFixed)
		}
	}

	return nil
}

// getAllSCDTableConfigs returns all SCD table configurations
// This matches the tables defined in lake/pkg/indexer/scd2_migrations.go
func getAllSCDTableConfigs() []duck.SCDTableConfig {
	return []duck.SCDTableConfig{
		// GeoIP
		mcpgeoip.SCD2ConfigGeoIPRecords(),
		// Solana
		sol.SCD2ConfigLeaderSchedule(),
		sol.SCD2ConfigVoteAccounts(),
		sol.SCD2ConfigGossipNodes(),
		// Serviceability
		dzsvc.SCD2ConfigContributors(),
		dzsvc.SCD2ConfigDevices(),
		dzsvc.SCD2ConfigUsers(),
		dzsvc.SCD2ConfigMetros(),
		dzsvc.SCD2ConfigLinks(),
	}
}

// parseIntervalToDuration parses a SQL interval string (e.g., "7 days", "1 week", "30 days") into a time.Duration
func parseIntervalToDuration(interval string) (time.Duration, error) {
	// Simple parser for common interval formats
	interval = strings.TrimSpace(interval)
	interval = strings.ToLower(interval)

	var days int
	var hours int
	var minutes int

	// Try to parse common patterns
	if strings.Contains(interval, "day") {
		_, err := fmt.Sscanf(interval, "%d day", &days)
		if err != nil {
			_, err = fmt.Sscanf(interval, "%d days", &days)
			if err != nil {
				return 0, fmt.Errorf("unable to parse days from interval: %q", interval)
			}
		}
	}
	if strings.Contains(interval, "week") {
		var weeks int
		_, err := fmt.Sscanf(interval, "%d week", &weeks)
		if err != nil {
			_, err = fmt.Sscanf(interval, "%d weeks", &weeks)
			if err != nil {
				return 0, fmt.Errorf("unable to parse weeks from interval: %q", interval)
			}
		}
		days = weeks * 7
	}
	if strings.Contains(interval, "hour") {
		_, err := fmt.Sscanf(interval, "%d hour", &hours)
		if err != nil {
			_, err = fmt.Sscanf(interval, "%d hours", &hours)
			if err != nil {
				return 0, fmt.Errorf("unable to parse hours from interval: %q", interval)
			}
		}
	}
	if strings.Contains(interval, "minute") {
		_, err := fmt.Sscanf(interval, "%d minute", &minutes)
		if err != nil {
			_, err = fmt.Sscanf(interval, "%d minutes", &minutes)
			if err != nil {
				return 0, fmt.Errorf("unable to parse minutes from interval: %q", interval)
			}
		}
	}

	if days == 0 && hours == 0 && minutes == 0 {
		return 0, fmt.Errorf("unable to parse interval: %q (expected format like '7 days', '1 week', '30 days')", interval)
	}

	return time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, nil
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day(s)", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour(s)", hours))
	}
	if minutes > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%d minute(s)", minutes))
	}
	if len(parts) == 0 {
		return "< 1 minute"
	}
	return strings.Join(parts, ", ")
}
