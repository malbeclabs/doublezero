package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type CollectorConfig struct {
	WheresitupCollectionInterval time.Duration
	RipeAtlasMeasurementInterval time.Duration
	RipeAtlasExportInterval      time.Duration
	DryRun                       bool
	ProcessedJobsFile            string
	StateDir                     string
	OutputDir                    string
	ProbesPerLocation            int
}

type WheresitupCollectorInterface interface {
	Run(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error
}

type RipeAtlasCollectorInterface interface {
	Run(ctx context.Context, dryRun bool, probesPerLocation int, stateDir, outputDir string, measurementInterval, exportInterval time.Duration) error
}

func Run(ctx context.Context, logger *slog.Logger, ripeCollector RipeAtlasCollectorInterface, wheresitupCollector WheresitupCollectorInterface, config CollectorConfig) error {

	logger.Info("Starting continuous collector",
		slog.String("wheresitup_interval", config.WheresitupCollectionInterval.String()),
		slog.String("ripe_atlas_measurement_interval", config.RipeAtlasMeasurementInterval.String()),
		slog.String("ripe_atlas_export_interval", config.RipeAtlasExportInterval.String()),
		slog.String("state_dir", config.StateDir),
		slog.String("output_dir", config.OutputDir))

	// Create a cancellable context for early termination
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Wheresitup job creation and export
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := wheresitupCollector.Run(ctx, config.WheresitupCollectionInterval, config.DryRun, config.ProcessedJobsFile, config.StateDir, config.OutputDir); err != nil {
			errChan <- fmt.Errorf("wheresitup collector error: %w", err)
			cancel() // Cancel other goroutines on error
		}
	}()

	// Ripe Atlas measurement creation and export
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ripeCollector.Run(ctx, config.DryRun, config.ProbesPerLocation, config.StateDir, config.OutputDir, config.RipeAtlasMeasurementInterval, config.RipeAtlasExportInterval); err != nil {
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
			logger.Warn("failed to run collectors", slog.Any("error", err))
			return fmt.Errorf("failed to run collectors: %w", err)
		}
	}

	// Check if context was cancelled
	if ctx.Err() != nil {
		logger.Info("Received shutdown signal, collectors stopped")
		return ctx.Err()
	}

	return nil
}
