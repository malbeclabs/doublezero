package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/metrics"
)

// startMaintenanceTasks starts periodic ducklake maintenance operations
func (i *Indexer) startMaintenanceTasks(ctx context.Context) {
	// Run short maintenance tasks (flush_inlined_data, merge_adjacent_files) on a schedule
	if i.cfg.MaintenanceIntervalShort > 0 {
		go func() {
			ticker := time.NewTicker(i.cfg.MaintenanceIntervalShort)
			defer ticker.Stop()

			// Run immediately on startup
			if err := i.runShortMaintenance(ctx); err != nil {
				i.log.Error("failed to run short maintenance on startup", "error", err)
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := i.runShortMaintenance(ctx); err != nil {
						i.log.Error("failed to run short maintenance", "error", err)
					}
				}
			}
		}()
	}

	// Run long maintenance tasks (flush_inlined_data, rewrite_data_files, merge_adjacent_files, expire_snapshots, cleanup_old_files, delete_orphaned_files) on a schedule
	if i.cfg.MaintenanceIntervalLong > 0 {
		go func() {
			ticker := time.NewTicker(i.cfg.MaintenanceIntervalLong)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := i.runLongMaintenance(ctx); err != nil {
						i.log.Error("failed to run long maintenance", "error", err)
					}
				}
			}
		}()
	}
}

func (i *Indexer) runShortMaintenance(ctx context.Context) error {
	start := time.Now()
	catalogName := i.cfg.DB.Catalog()
	i.log.Info("maintenance/short: running short maintenance tasks", "catalog", catalogName)

	// Get the Lake instance to run maintenance
	lake, ok := i.cfg.DB.(*duck.Lake)
	if !ok {
		return fmt.Errorf("DB is not a Lake instance")
	}

	if err := lake.RunShortMaintenance(ctx); err != nil {
		metrics.MaintenanceOperationTotal.WithLabelValues("short_maintenance", "error").Inc()
		return err
	}

	duration := time.Since(start)
	metrics.MaintenanceOperationDuration.WithLabelValues("short_maintenance").Observe(duration.Seconds())
	metrics.MaintenanceOperationTotal.WithLabelValues("short_maintenance", "success").Inc()
	i.log.Info("maintenance/short: successfully completed short maintenance", "catalog", catalogName, "duration", duration)
	return nil
}

func (i *Indexer) runLongMaintenance(ctx context.Context) error {
	start := time.Now()
	catalogName := i.cfg.DB.Catalog()
	i.log.Info("maintenance/long: running checkpoint", "catalog", catalogName)

	// Get the Lake instance to run maintenance
	lake, ok := i.cfg.DB.(*duck.Lake)
	if !ok {
		return fmt.Errorf("DB is not a Lake instance")
	}

	if err := lake.Checkpoint(ctx, i.cfg.ExpireSnapshotsOlderThan); err != nil {
		metrics.MaintenanceOperationTotal.WithLabelValues("long_maintenance", "error").Inc()
		return err
	}

	duration := time.Since(start)
	metrics.MaintenanceOperationDuration.WithLabelValues("long_maintenance").Observe(duration.Seconds())
	metrics.MaintenanceOperationTotal.WithLabelValues("long_maintenance", "success").Inc()
	i.log.Info("maintenance/long: successfully completed checkpoint", "catalog", catalogName, "duration", duration)
	return nil
}
