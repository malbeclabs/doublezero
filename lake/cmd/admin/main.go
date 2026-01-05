package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/latency"
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
	duckLakeCatalogNameFlag := flag.String("lake-catalog-name", "dzlake", "Name of the DuckLake catalog (or set DUCKLAKE_CATALOG_NAME env var)")
	duckLakeCatalogURIFlag := flag.String("lake-catalog-uri", "file://.tmp/lake/catalog.sqlite", "URI to the DuckLake catalog (or set LAKE_CATALOG_URI env var)")
	duckLakeStorageURIFlag := flag.String("ducklake-storage-uri", "file://.tmp/lake/data", "URI to the DuckLake storage directory (or set LAKE_STORAGE_URI env var)")

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
	execSQLFlag := flag.String("sql", "", "Execute arbitrary SQL query (omit value for interactive mode, use --sql-file for file input)")
	execSQLFileFlag := flag.String("sql-file", "", "Execute SQL from file")
	backfillValidToOnDeletesFlag := flag.Bool("backfill-valid-to-on-deletes", false, "Backfill valid_to field for SCD2 history rows where it wasn't set on delete")
	backfillValidToOnReinsertsFlag := flag.Bool("backfill-valid-to-on-reinserts", false, "Backfill valid_to field for SCD2 delete tombstones where it wasn't set on re-insert")
	deduplicateCurrentTableFlag := flag.Bool("deduplicate-current-table", false, "Remove duplicate rows from SCD2 current tables, keeping only the most recent row per primary key")
	backfillIPDVFlag := flag.Bool("backfill-ipdv", false, "Backfill ipdv_us (IPDV/jitter) column for telemetry latency fact tables")
	dumpSchemaFlag := flag.String("dump-schema", "", "Dump CREATE TABLE statements to file (e.g., 'schema/db_schema.sql')")

	allFlag := flag.Bool("all", false, "Apply to all items (for use with --cleanup-old-files or --delete-orphaned-files)")
	olderThanFlag := flag.String("older-than", "", "Older than this interval (e.g., '7 days', '1 week', '30 days', for use with --cleanup-old-files, --delete-orphaned-files, or --expire-snapshots)")
	dryRunFlag := flag.Bool("dry-run", false, "Dry run mode - show what would be done without actually executing (for use with --cleanup-old-files, --delete-orphaned-files, or --expire-snapshots)")

	// Check if --sql was provided without a value (before parsing, which would error)
	execSQLProvidedWithoutValue := false
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--sql" {
			// Check if it's the last arg or next arg is another flag
			if i+1 >= len(os.Args) || strings.HasPrefix(os.Args[i+1], "-") {
				execSQLProvidedWithoutValue = true
				// Set it to empty string so parsing doesn't error
				os.Args[i] = "--sql="
			}
			break
		}
	}

	flag.Parse()

	// Override flags with environment variables if set
	if envCatalogURI := os.Getenv("LAKE_CATALOG_URI"); envCatalogURI != "" {
		*duckLakeCatalogURIFlag = envCatalogURI
	}
	if envStorageURI := os.Getenv("LAKE_STORAGE_URI"); envStorageURI != "" {
		*duckLakeStorageURIFlag = envStorageURI
	}
	if envCatalogName := os.Getenv("DUCKLAKE_CATALOG_NAME"); envCatalogName != "" {
		*duckLakeCatalogNameFlag = envCatalogName
	}

	log := logger.New(*verboseFlag)

	// Check if sql flag was set (even if empty, for interactive mode)
	execSQLFlagSet := flag.Lookup("sql") != nil && flag.Lookup("sql").Changed
	if execSQLProvidedWithoutValue {
		execSQLFlagSet = true
		*execSQLFlag = ""
	}

	// Check that at least one command is specified
	if !*mergeFilesFlag && !*rewriteFilesFlag && !*cleanupOldFilesFlag && !*deleteOrphanedFilesFlag && !*expireSnapshotsFlag && !*flushInlinedDataFlag && !*checkpointFlag && *setExpireOlderThanFlag == "" && !execSQLFlagSet && *execSQLFileFlag == "" && !*backfillValidToOnDeletesFlag && !*backfillValidToOnReinsertsFlag && !*deduplicateCurrentTableFlag && !*backfillIPDVFlag && *dumpSchemaFlag == "" {
		return fmt.Errorf("must specify at least one command: --merge-adjacent-files, --rewrite-data-files, --cleanup-old-files, --delete-orphaned-files, --expire-snapshots, --flush-inlined-data, --checkpoint, --set-expire-older-than, --sql, --sql-file, --backfill-valid-to-on-deletes, --backfill-valid-to-on-reinserts, --deduplicate-current-table, --backfill-ipdv, or --dump-schema")
	}

	// Validate SQL execution flags
	if execSQLFlagSet && *execSQLFileFlag != "" {
		return fmt.Errorf("--sql and --sql-file cannot be used together")
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

	// Handle dump-schema command (needs database connection)
	if *dumpSchemaFlag != "" {
		// Prepare S3 config if using S3 storage
		s3Config, err := duck.PrepareS3ConfigForStorageURI(ctx, log, *duckLakeStorageURIFlag)
		if err != nil {
			return fmt.Errorf("failed to prepare S3 config: %w", err)
		}

		// Create lake connection - this requires the actual database to exist
		log.Info("initializing ducklake database", "catalog", *duckLakeCatalogNameFlag, "catalogURI", duck.RedactedCatalogURI(*duckLakeCatalogURIFlag), "storageURI", duck.RedactedStorageURI(*duckLakeStorageURIFlag))
		lake, err := duck.NewLake(ctx, log, *duckLakeCatalogNameFlag, *duckLakeCatalogURIFlag, *duckLakeStorageURIFlag, false, s3Config)
		if err != nil {
			return fmt.Errorf("failed to create lake connection (database must exist with tables): %w", err)
		}
		defer lake.Close()

		conn, err := lake.Conn(ctx)
		if err != nil {
			return fmt.Errorf("failed to get connection: %w", err)
		}
		defer conn.Close()

		return dumpSchema(ctx, conn, *dumpSchemaFlag)
	}

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

	if execSQLFlagSet || *execSQLFileFlag != "" {
		if execSQLFlagSet && *execSQLFlag == "" {
			// Interactive mode
			return runInteractiveSQL(ctx, conn, log, *verboseFlag)
		}

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

		return executeSQL(ctx, conn, log, sql)
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

	if *backfillValidToOnReinsertsFlag {
		// Get all SCD table configs
		configs := getAllSCDTableConfigs()

		if *dryRunFlag {
			log.Info("running backfill valid_to on re-inserts in dry-run mode - no changes will be made")
		}

		totalFixed := 0
		for _, cfg := range configs {
			log.Info("processing table for backfill", "table", cfg.TableBaseName, "dry_run", *dryRunFlag)

			fixedCount, err := duck.BackfillValidToOnReinserts(ctx, log, conn, cfg, *dryRunFlag)
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

	if *deduplicateCurrentTableFlag {
		// Get all SCD table configs
		configs := getAllSCDTableConfigs()

		if *dryRunFlag {
			log.Info("running deduplicate current table in dry-run mode - no changes will be made")
		}

		totalDeleted := 0
		for _, cfg := range configs {
			log.Info("processing table for deduplication", "table", cfg.TableBaseName, "dry_run", *dryRunFlag)

			deletedCount, err := duck.DeduplicateCurrentTable(ctx, log, conn, cfg, *dryRunFlag)
			if err != nil {
				return fmt.Errorf("failed to deduplicate table %s: %w", cfg.TableBaseName, err)
			}

			if *dryRunFlag {
				log.Info("dry run completed for table",
					"table", cfg.TableBaseName,
					"rows_to_delete", deletedCount)
			} else {
				log.Info("deduplication completed for table",
					"table", cfg.TableBaseName,
					"rows_deleted", deletedCount)
			}

			totalDeleted += deletedCount
		}

		if *dryRunFlag {
			log.Info("dry run completed for all tables", "total_rows_to_delete", totalDeleted)
			fmt.Printf("\nDry run completed. Total rows that would be deleted: %d\n", totalDeleted)
			fmt.Println("Run without --dry-run to apply the changes.")
		} else {
			log.Info("deduplication completed for all tables", "total_rows_deleted", totalDeleted)
			fmt.Printf("\nDeduplication completed. Total rows deleted: %d\n", totalDeleted)
		}
	}

	if *backfillIPDVFlag {
		if *dryRunFlag {
			log.Info("running backfill IPDV in dry-run mode - no changes will be made")
		}

		// Backfill device link latency samples
		log.Info("processing device link latency samples for IPDV backfill", "dry_run", *dryRunFlag)
		deviceLinkCount, err := dztelemlatency.BackfillIPDVDeviceLink(ctx, log, conn, *dryRunFlag)
		if err != nil {
			return fmt.Errorf("failed to backfill IPDV for device link latency samples: %w", err)
		}

		// Backfill internet metro latency samples
		log.Info("processing internet metro latency samples for IPDV backfill", "dry_run", *dryRunFlag)
		internetMetroCount, err := dztelemlatency.BackfillIPDVInternetMetro(ctx, log, conn, *dryRunFlag)
		if err != nil {
			return fmt.Errorf("failed to backfill IPDV for internet metro latency samples: %w", err)
		}

		totalFixed := deviceLinkCount + internetMetroCount

		if *dryRunFlag {
			log.Info("dry run completed for IPDV backfill",
				"device_link_rows_to_update", deviceLinkCount,
				"internet_metro_rows_to_update", internetMetroCount,
				"total_rows_to_update", totalFixed)
			fmt.Printf("\nDry run completed. Total rows that would be updated: %d\n", totalFixed)
			fmt.Printf("  Device link latency samples: %d\n", deviceLinkCount)
			fmt.Printf("  Internet metro latency samples: %d\n", internetMetroCount)
			fmt.Println("Run without --dry-run to apply the changes.")
		} else {
			log.Info("backfill completed for IPDV",
				"device_link_rows_updated", deviceLinkCount,
				"internet_metro_rows_updated", internetMetroCount,
				"total_rows_updated", totalFixed)
			fmt.Printf("\nBackfill completed. Total rows updated: %d\n", totalFixed)
			fmt.Printf("  Device link latency samples: %d\n", deviceLinkCount)
			fmt.Printf("  Internet metro latency samples: %d\n", internetMetroCount)
		}
	}

	return nil
}

// stripLeadingComments removes leading comment lines and empty lines from SQL
// This fixes issues where DuckDB misinterprets SQL that starts with comments
func stripLeadingComments(sql string) string {
	lines := strings.Split(sql, "\n")
	var result []string
	foundNonComment := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !foundNonComment {
			// Skip empty lines and comment lines until we find actual SQL
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			foundNonComment = true
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// dumpSchema queries the database for CREATE TABLE statements and writes them to a file
func dumpSchema(ctx context.Context, conn duck.Connection, outputPath string) error {
	// Get list of tables from information_schema
	rows, err := conn.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = current_schema()
		ORDER BY table_name
	`)
	if err != nil {
		return fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating tables: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("-- Database schema for catalog tables\n")
	sb.WriteString("-- Generated by: lake admin --dump-schema\n")
	sb.WriteString("-- This file is used to initialize test databases\n\n")

	// For each table, get its CREATE TABLE statement
	for _, tableName := range tables {
		// Try DESCRIBE first to get column info, then reconstruct CREATE TABLE
		// DuckDB doesn't have SHOW CREATE TABLE, so we reconstruct from information_schema
		createStmt, err := reconstructCreateTable(ctx, conn, tableName)
		if err != nil {
			return fmt.Errorf("failed to reconstruct CREATE TABLE for %s: %w", tableName, err)
		}
		sb.WriteString(createStmt)
		sb.WriteString(";\n\n")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(outputPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	fmt.Printf("Schema dumped to %s (%d tables)\n", outputPath, len(tables))
	return nil
}

// reconstructCreateTable reconstructs a CREATE TABLE statement from information_schema
func reconstructCreateTable(ctx context.Context, conn duck.Connection, tableName string) (string, error) {
	// Query column information from information_schema
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = '%s'
		ORDER BY ordinal_position
	`, tableName))
	if err != nil {
		return "", fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var colName, dataType, isNullable sql.NullString
		var colDefault sql.NullString
		if err := rows.Scan(&colName, &dataType, &isNullable, &colDefault); err != nil {
			return "", fmt.Errorf("failed to scan column: %w", err)
		}

		if !colName.Valid || !dataType.Valid {
			continue
		}

		colDef := fmt.Sprintf("    %s %s", colName.String, dataType.String)
		if isNullable.Valid && isNullable.String == "NO" {
			colDef += " NOT NULL"
		}
		if colDefault.Valid && colDefault.String != "" {
			colDef += " DEFAULT " + colDefault.String
		}
		columns = append(columns, colDef)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating columns: %w", err)
	}

	if len(columns) == 0 {
		return "", fmt.Errorf("no columns found for table %s", tableName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", tableName))
	sb.WriteString(strings.Join(columns, ",\n"))
	sb.WriteString("\n)")

	return sb.String(), nil
}

// executeSQL executes a SQL query or statement and prints the results
func executeSQL(ctx context.Context, conn duck.Connection, log *slog.Logger, sql string) error {
	// Strip leading comments to avoid DuckDB parsing issues
	sql = stripLeadingComments(sql)

	// Try to determine if it's a query (SELECT) or exec (INSERT/UPDATE/DELETE/CALL/etc)
	sqlUpper := strings.ToUpper(strings.TrimSpace(sql))
	isQuery := strings.HasPrefix(sqlUpper, "SELECT") || strings.HasPrefix(sqlUpper, "WITH") || strings.HasPrefix(sqlUpper, "SHOW") || strings.HasPrefix(sqlUpper, "DESCRIBE") || strings.HasPrefix(sqlUpper, "DESC") || strings.HasPrefix(sqlUpper, "EXPLAIN")

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

		// Collect all rows first to calculate column widths
		var allRows [][]string
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
			allRows = append(allRows, rowValues)
			rowCount++
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating rows: %w", err)
		}

		// Calculate column widths
		colWidths := make([]int, len(columns))
		for i, col := range columns {
			colWidths[i] = len(col)
		}
		for _, row := range allRows {
			for i, val := range row {
				if len(val) > colWidths[i] {
					colWidths[i] = len(val)
				}
			}
		}

		fmt.Println() // Add newline before result table
		// Print header
		var headerParts []string
		for i, col := range columns {
			headerParts = append(headerParts, fmt.Sprintf("%-*s", colWidths[i], col))
		}
		fmt.Println(strings.Join(headerParts, " | "))

		// Print separator line
		var sepParts []string
		for _, width := range colWidths {
			sepParts = append(sepParts, strings.Repeat("-", width))
		}
		fmt.Println(strings.Join(sepParts, "-+-"))

		// Print rows
		for _, row := range allRows {
			var rowParts []string
			for i, val := range row {
				rowParts = append(rowParts, fmt.Sprintf("%-*s", colWidths[i], val))
			}
			fmt.Println(strings.Join(rowParts, " | "))
		}
		fmt.Println() // Add newline after result table

		log.Info("SQL query completed", "rows", rowCount)
	} else {
		result, err := conn.ExecContext(ctx, sql)
		if err != nil {
			return fmt.Errorf("failed to execute SQL: %w", err)
		}
		rowsAffected, _ := result.RowsAffected()
		log.Info("SQL executed successfully", "rows_affected", rowsAffected)
	}
	return nil
}

// runInteractiveSQL runs an interactive SQL prompt loop
func runInteractiveSQL(ctx context.Context, conn duck.Connection, log *slog.Logger, verbose bool) error {
	// Create a context that cancels on Ctrl-C
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	scanner := bufio.NewScanner(os.Stdin)
	var sqlLines []string

	// Channel to signal when input is ready
	inputChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Goroutine to read input
	go func() {
		for {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					errChan <- err
					return
				}
				// EOF
				close(inputChan)
				return
			}
			inputChan <- scanner.Text()
		}
	}()

	fmt.Println("Interactive SQL mode (Ctrl-C to exit)")
	fmt.Println() // Add newline before first prompt for separation
	fmt.Print("sql> ")

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nExiting interactive SQL mode")
			return nil
		case err := <-errChan:
			return fmt.Errorf("error reading input: %w", err)
		case line, ok := <-inputChan:
			if !ok {
				// Channel closed, stdin EOF
				return nil
			}

			line = strings.TrimRight(line, " \t")

			// Check for empty line (execute accumulated SQL)
			if line == "" && len(sqlLines) > 0 {
				sql := strings.Join(sqlLines, "\n")
				sqlLines = nil

				if err := executeSQL(ctx, conn, log, sql); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}
				fmt.Println() // Add newline before prompt for separation
				fmt.Print("sql> ")
				continue
			}

			// Check for semicolon at end (execute immediately)
			if strings.HasSuffix(line, ";") {
				sqlLines = append(sqlLines, line)
				sql := strings.Join(sqlLines, "\n")
				// Remove trailing semicolon
				sql = strings.TrimSuffix(sql, ";")
				sql = strings.TrimSpace(sql)
				sqlLines = nil

				if sql != "" {
					if err := executeSQL(ctx, conn, log, sql); err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					}
				}
				fmt.Println() // Add newline before prompt for separation
				fmt.Print("sql> ")
				continue
			}

			// Accumulate line for multi-line queries
			if line != "" {
				sqlLines = append(sqlLines, line)
			} else {
				// Empty line with no accumulated SQL, just show prompt
				fmt.Print("sql> ")
			}
		}
	}
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
