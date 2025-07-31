package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (c *Collector) Run(ctx context.Context) error {
	c.log.Info("Starting continuous collector",
		slog.String("wheresitup_interval", c.cfg.WheresitupCollectionInterval.String()),
		slog.String("ripe_atlas_measurement_interval", c.cfg.RipeAtlasMeasurementInterval.String()),
		slog.String("ripe_atlas_export_interval", c.cfg.RipeAtlasExportInterval.String()),
		slog.String("state_dir", c.cfg.StateDir),
		slog.String("output_dir", c.cfg.OutputDir))

	// Create a cancellable context for early termination
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start Prometheus metrics endpoint
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		metricsAddr := c.cfg.MetricsAddr
		if metricsAddr == "" {
			metricsAddr = "127.0.0.1:2113"
		}
		c.log.Info("Starting metrics server", slog.String("address", metricsAddr))
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			c.log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Wheresitup job creation and export
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.cfg.Wheresitup.Run(ctx, c.cfg.WheresitupCollectionInterval, c.cfg.DryRun, c.cfg.ProcessedJobsFile, c.cfg.StateDir, c.cfg.OutputDir); err != nil {
			errChan <- fmt.Errorf("wheresitup collector error: %w", err)
			cancel() // Cancel other goroutines on error
		}
	}()

	// Ripe Atlas measurement creation and export
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.cfg.RipeAtlas.Run(ctx, c.cfg.DryRun, c.cfg.ProbesPerLocation, c.cfg.StateDir, c.cfg.OutputDir, c.cfg.RipeAtlasMeasurementInterval, c.cfg.RipeAtlasExportInterval); err != nil {
			errChan <- fmt.Errorf("ripe atlas collector error: %w", err)
			cancel() // Cancel other goroutines on error
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			c.log.Warn("failed to run collectors", slog.Any("error", err))
			return fmt.Errorf("failed to run collectors: %w", err)
		}
	}

	// Check if context was cancelled
	if ctx.Err() != nil {
		c.log.Info("Received shutdown signal, collectors stopped")
		return ctx.Err()
	}

	return nil
}
