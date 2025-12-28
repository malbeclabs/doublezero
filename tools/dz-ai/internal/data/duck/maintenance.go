package duck

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CleanupOptions holds options for cleanup_old_files
type CleanupOptions struct {
	CleanupAll bool
	OlderThan  time.Duration // If > 0, use this duration
	DryRun     bool
}

// DeleteOrphanedOptions holds options for delete_orphaned_files
type DeleteOrphanedOptions struct {
	CleanupAll bool
	OlderThan  time.Duration // If > 0, use this duration
	DryRun     bool
}

// ExpireSnapshotsOptions holds options for expire_snapshots
type ExpireSnapshotsOptions struct {
	OlderThan time.Duration // If 0, use global config
	DryRun    bool
}

// SnapshotInfo represents a snapshot that would be expired (for dry-run results)
type SnapshotInfo struct {
	ID            string
	Time          string
	TimeValue     time.Time
	SchemaVersion string
	Changes       string
}

// ExpireSnapshotsResult holds the result of a dry-run expire_snapshots operation
type ExpireSnapshotsResult struct {
	Snapshots    []SnapshotInfo
	EarliestTime *time.Time
	LatestTime   *time.Time
}

// RunShortMaintenance runs the short maintenance tasks:
// 1. Flush inlined data
// 2. Merge adjacent files
func (l *Lake) RunShortMaintenance(ctx context.Context) error {
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// 1. Flush inlined data
	l.log.Info("maintenance/short: flushing inlined data", "catalog", l.catalog)
	flushSQL := fmt.Sprintf("CALL ducklake_flush_inlined_data('%s')", l.catalog)
	if _, err := conn.ExecContext(ctx, flushSQL); err != nil {
		return fmt.Errorf("failed to flush inlined data: %w", err)
	}
	l.log.Info("maintenance/short: successfully flushed inlined data", "catalog", l.catalog)

	// 2. Merge adjacent files
	if err := l.MergeAdjacentFiles(ctx); err != nil {
		return fmt.Errorf("failed to merge adjacent files: %w", err)
	}

	l.log.Info("maintenance/short: successfully completed short maintenance", "catalog", l.catalog)
	return nil
}

// Checkpoint runs the checkpoint maintenance tasks:
// 1. Flush inlined data
// 2. Rewrite data files
// 3. Merge adjacent files
// 4. Expire snapshots
// 5. Cleanup old files
// 6. Delete orphaned files
//
// If expireOlderThan is 0, the global DuckLake expire_older_than option will be used.
func (l *Lake) Checkpoint(ctx context.Context, expireOlderThan time.Duration) error {
	// 1. Flush inlined data
	if err := l.FlushInlinedData(ctx); err != nil {
		return fmt.Errorf("failed to flush inlined data: %w", err)
	}

	// 2. Rewrite data files
	if err := l.RewriteDataFiles(ctx, 0.95); err != nil {
		return fmt.Errorf("failed to rewrite data files: %w", err)
	}

	// 3. Merge adjacent files
	if err := l.MergeAdjacentFiles(ctx); err != nil {
		return fmt.Errorf("failed to merge adjacent files: %w", err)
	}

	// 4. Expire snapshots
	if _, err := l.ExpireSnapshots(ctx, ExpireSnapshotsOptions{OlderThan: expireOlderThan}); err != nil {
		return fmt.Errorf("failed to expire snapshots: %w", err)
	}

	// 5. Cleanup old files
	if err := l.CleanupOldFiles(ctx, CleanupOptions{CleanupAll: true}); err != nil {
		return fmt.Errorf("failed to cleanup old files: %w", err)
	}

	// 6. Delete orphaned files
	if err := l.DeleteOrphanedFiles(ctx, DeleteOrphanedOptions{CleanupAll: true}); err != nil {
		return fmt.Errorf("failed to delete orphaned files: %w", err)
	}

	l.log.Info("checkpoint: successfully completed all tasks", "catalog", l.catalog)
	return nil
}

// FlushInlinedData runs ducklake_flush_inlined_data for the catalog
func (l *Lake) FlushInlinedData(ctx context.Context) error {
	l.log.Info("flushing inlined data", "catalog", l.catalog)
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	flushSQL := fmt.Sprintf("CALL ducklake_flush_inlined_data('%s')", l.catalog)
	if _, err := conn.ExecContext(ctx, flushSQL); err != nil {
		return fmt.Errorf("failed to flush inlined data: %w", err)
	}
	l.log.Info("successfully flushed inlined data", "catalog", l.catalog)
	return nil
}

// MergeAdjacentFiles runs ducklake_merge_adjacent_files for the catalog
func (l *Lake) MergeAdjacentFiles(ctx context.Context) error {
	l.log.Info("running merge_adjacent_files", "catalog", l.catalog)
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	mergeSQL := fmt.Sprintf("CALL ducklake_merge_adjacent_files('%s')", l.catalog)
	if _, err := conn.ExecContext(ctx, mergeSQL); err != nil {
		return fmt.Errorf("failed to merge adjacent files: %w", err)
	}
	l.log.Info("successfully merged adjacent files", "catalog", l.catalog)
	return nil
}

// RewriteDataFilesForTable runs ducklake_rewrite_data_files for a specific table
func (l *Lake) RewriteDataFilesForTable(ctx context.Context, tableName string, deleteThreshold float64) error {
	l.log.Info("rewriting data files for table", "catalog", l.catalog, "table", tableName, "delete_threshold", deleteThreshold)
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	rewriteSQL := fmt.Sprintf("CALL ducklake_rewrite_data_files('%s', '%s', delete_threshold => %f)",
		l.catalog, tableName, deleteThreshold)
	if _, err := conn.ExecContext(ctx, rewriteSQL); err != nil {
		return fmt.Errorf("failed to rewrite data files for table %s: %w", tableName, err)
	}
	l.log.Info("successfully rewrote data files for table", "catalog", l.catalog, "table", tableName)
	return nil
}

// RewriteDataFiles runs ducklake_rewrite_data_files for all tables in the catalog
func (l *Lake) RewriteDataFiles(ctx context.Context, deleteThreshold float64) error {
	l.log.Info("running rewrite_data_files", "catalog", l.catalog, "delete_threshold", deleteThreshold)
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Get all table names from the catalog
	rows, err := conn.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_catalog = ? AND table_schema = ?
		ORDER BY table_name
	`, l.catalog, l.schema)
	if err != nil {
		return fmt.Errorf("failed to query table names: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating table names: %w", err)
	}

	if len(tableNames) == 0 {
		l.log.Info("no tables found to rewrite", "catalog", l.catalog)
		return nil
	}

	// Rewrite each table
	successCount := 0
	errorCount := 0
	for _, tableName := range tableNames {
		l.log.Info("rewriting data files for table", "catalog", l.catalog, "table", tableName)
		rewriteSQL := fmt.Sprintf("CALL ducklake_rewrite_data_files('%s', '%s', delete_threshold => %f)",
			l.catalog, tableName, deleteThreshold)
		if _, err := conn.ExecContext(ctx, rewriteSQL); err != nil {
			l.log.Error("failed to rewrite data files for table", "catalog", l.catalog, "table", tableName, "error", err)
			errorCount++
			// Continue with other tables even if one fails
			continue
		}
		l.log.Info("successfully rewrote data files for table", "catalog", l.catalog, "table", tableName)
		successCount++
	}

	if errorCount > 0 && successCount == 0 {
		return fmt.Errorf("failed to rewrite data files for all tables")
	}
	l.log.Info("completed rewrite_data_files for all tables", "catalog", l.catalog, "table_count", len(tableNames), "success", successCount, "errors", errorCount)
	return nil
}

// ExpireSnapshots runs ducklake_expire_snapshots for the catalog.
// If opts.OlderThan is 0, the global DuckLake expire_older_than option will be used.
// If opts.DryRun is true, returns the snapshots that would be expired.
func (l *Lake) ExpireSnapshots(ctx context.Context, opts ExpireSnapshotsOptions) (*ExpireSnapshotsResult, error) {
	conn, err := l.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	var expireSQL string
	var options []string

	if opts.OlderThan > 0 {
		intervalStr := formatDurationForSQL(opts.OlderThan)
		options = append(options, fmt.Sprintf("older_than => now() - INTERVAL '%s'", intervalStr))
		l.log.Info("expiring snapshots", "catalog", l.catalog, "older_than", opts.OlderThan, "dry_run", opts.DryRun)
	} else {
		l.log.Info("expiring snapshots using global config", "catalog", l.catalog, "dry_run", opts.DryRun)
	}

	if opts.DryRun {
		options = append(options, "dry_run => true")
	}

	if len(options) > 0 {
		expireSQL = fmt.Sprintf("CALL ducklake_expire_snapshots('%s', %s)", l.catalog, strings.Join(options, ", "))
	} else {
		expireSQL = fmt.Sprintf("CALL ducklake_expire_snapshots('%s')", l.catalog)
	}

	if opts.DryRun {
		// Use QueryContext to capture results
		rows, err := conn.QueryContext(ctx, expireSQL)
		if err != nil {
			return nil, fmt.Errorf("failed to expire snapshots (dry run): %w", err)
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get columns: %w", err)
		}

		var snapshots []SnapshotInfo
		var earliestTime, latestTime *time.Time

		for rows.Next() {
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, fmt.Errorf("failed to scan row: %w", err)
			}

			var snapshotID, snapshotTime, schemaVersion, changesStr string
			var snapshotTimeValue time.Time

			for i, col := range columns {
				val := values[i]
				var valStr string
				if val == nil {
					valStr = ""
				} else {
					switch v := val.(type) {
					case []byte:
						valStr = string(v)
					case time.Time:
						valStr = v.Format(time.RFC3339)
						if col == "snapshot_time" {
							snapshotTimeValue = v
						}
					case map[string]any:
						if col == "changes" {
							var parts []string
							for k, v := range v {
								parts = append(parts, fmt.Sprintf("%s: %v", k, v))
							}
							valStr = strings.Join(parts, ", ")
						} else {
							valStr = fmt.Sprintf("%v", v)
						}
					default:
						valStr = fmt.Sprintf("%v", v)
					}
				}

				switch col {
				case "snapshot_id":
					snapshotID = valStr
				case "snapshot_time":
					snapshotTime = valStr
				case "schema_version":
					schemaVersion = valStr
				case "changes":
					changesStr = valStr
				}
			}

			if !snapshotTimeValue.IsZero() {
				if earliestTime == nil || snapshotTimeValue.Before(*earliestTime) {
					t := snapshotTimeValue
					earliestTime = &t
				}
				if latestTime == nil || snapshotTimeValue.After(*latestTime) {
					t := snapshotTimeValue
					latestTime = &t
				}
			}

			snapshots = append(snapshots, SnapshotInfo{
				ID:            snapshotID,
				Time:          snapshotTime,
				TimeValue:     snapshotTimeValue,
				SchemaVersion: schemaVersion,
				Changes:       changesStr,
			})
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating rows: %w", err)
		}

		return &ExpireSnapshotsResult{
			Snapshots:    snapshots,
			EarliestTime: earliestTime,
			LatestTime:   latestTime,
		}, nil
	}

	// Non-dry-run: execute the command
	if _, err := conn.ExecContext(ctx, expireSQL); err != nil {
		return nil, fmt.Errorf("failed to expire snapshots: %w", err)
	}
	l.log.Info("successfully expired snapshots", "catalog", l.catalog)
	return nil, nil
}

// CleanupOldFiles runs ducklake_cleanup_old_files for the catalog
func (l *Lake) CleanupOldFiles(ctx context.Context, opts CleanupOptions) error {
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	var options []string
	if opts.CleanupAll {
		options = append(options, "cleanup_all => true")
	} else if opts.OlderThan > 0 {
		intervalStr := formatDurationForSQL(opts.OlderThan)
		options = append(options, fmt.Sprintf("older_than => now() - INTERVAL '%s'", intervalStr))
	}

	if opts.DryRun {
		options = append(options, "dry_run => true")
	}

	var cleanupSQL string
	if len(options) > 0 {
		cleanupSQL = fmt.Sprintf("CALL ducklake_cleanup_old_files('%s', %s)", l.catalog, strings.Join(options, ", "))
	} else {
		cleanupSQL = fmt.Sprintf("CALL ducklake_cleanup_old_files('%s')", l.catalog)
	}

	if opts.DryRun {
		l.log.Info("dry run: cleaning up old files", "catalog", l.catalog, "cleanup_all", opts.CleanupAll, "older_than", opts.OlderThan)
	} else {
		l.log.Info("cleaning up old files", "catalog", l.catalog, "cleanup_all", opts.CleanupAll, "older_than", opts.OlderThan)
	}

	if _, err := conn.ExecContext(ctx, cleanupSQL); err != nil {
		return fmt.Errorf("failed to cleanup old files: %w", err)
	}

	if opts.DryRun {
		l.log.Info("dry run completed - check output for files that would be cleaned up", "catalog", l.catalog)
	} else {
		l.log.Info("successfully cleaned up old files", "catalog", l.catalog)
	}
	return nil
}

// DeleteOrphanedFiles runs ducklake_delete_orphaned_files for the catalog
func (l *Lake) DeleteOrphanedFiles(ctx context.Context, opts DeleteOrphanedOptions) error {
	conn, err := l.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	var options []string
	if opts.CleanupAll {
		options = append(options, "cleanup_all => true")
	} else if opts.OlderThan > 0 {
		intervalStr := formatDurationForSQL(opts.OlderThan)
		options = append(options, fmt.Sprintf("older_than => now() - INTERVAL '%s'", intervalStr))
	}

	if opts.DryRun {
		options = append(options, "dry_run => true")
	}

	var deleteSQL string
	if len(options) > 0 {
		deleteSQL = fmt.Sprintf("CALL ducklake_delete_orphaned_files('%s', %s)", l.catalog, strings.Join(options, ", "))
	} else {
		deleteSQL = fmt.Sprintf("CALL ducklake_delete_orphaned_files('%s')", l.catalog)
	}

	if opts.DryRun {
		if opts.CleanupAll {
			l.log.Info("dry run: deleting all orphaned files", "catalog", l.catalog)
		} else {
			l.log.Info("dry run: deleting orphaned files older than interval", "catalog", l.catalog, "older_than", opts.OlderThan)
		}
	} else {
		if opts.CleanupAll {
			l.log.Info("deleting all orphaned files", "catalog", l.catalog)
		} else {
			l.log.Info("deleting orphaned files older than interval", "catalog", l.catalog, "older_than", opts.OlderThan)
		}
	}

	if _, err := conn.ExecContext(ctx, deleteSQL); err != nil {
		return fmt.Errorf("failed to delete orphaned files: %w", err)
	}

	if opts.DryRun {
		l.log.Info("dry run completed - check output for files that would be deleted", "catalog", l.catalog)
	} else {
		l.log.Info("successfully deleted orphaned files", "catalog", l.catalog)
	}
	return nil
}

// formatDurationForSQL converts a Go time.Duration to a SQL INTERVAL string
func formatDurationForSQL(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day", days))
		if days > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour", hours))
		if hours > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute", minutes))
		if minutes > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if seconds > 0 && len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d second", seconds))
		if seconds > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if len(parts) == 0 {
		return "0 seconds"
	}
	return strings.Join(parts, " ")
}
