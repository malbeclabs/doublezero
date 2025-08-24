package worker

import (
	"context"
	"log/slog"

	devicetelemetry "github.com/malbeclabs/doublezero/controlplane/monitor/internal/device-telemetry"
	internettelemetry "github.com/malbeclabs/doublezero/controlplane/monitor/internal/internet-telemetry"
)

type Watcher interface {
	Name() string
	Run(ctx context.Context) error
}

type Worker struct {
	log *slog.Logger
	cfg *Config

	watchers []Watcher
}

func New(cfg *Config) (*Worker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	deviceTelemetryWatcher, err := devicetelemetry.NewDeviceTelemetryWatcher(&devicetelemetry.Config{
		Logger:          cfg.Logger,
		LedgerRPCClient: cfg.LedgerRPCClient,
		Serviceability:  cfg.Serviceability,
		Telemetry:       cfg.Telemetry,
		Interval:        cfg.Interval,
	})
	if err != nil {
		return nil, err
	}

	internetTelemetryWatcher, err := internettelemetry.NewInternetTelemetryWatcher(&internettelemetry.Config{
		Logger:                     cfg.Logger,
		LedgerRPCClient:            cfg.LedgerRPCClient,
		Serviceability:             cfg.Serviceability,
		Telemetry:                  cfg.Telemetry,
		Interval:                   cfg.Interval,
		InternetLatencyCollectorPK: cfg.InternetLatencyCollectorPK,
	})
	if err != nil {
		return nil, err
	}

	watchers := []Watcher{
		deviceTelemetryWatcher,
		internetTelemetryWatcher,
	}
	return &Worker{
		log:      cfg.Logger,
		cfg:      cfg,
		watchers: watchers,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Starting worker")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, watcher := range w.watchers {
		go func(watcher Watcher) {
			name := watcher.Name()
			w.log.Info("Starting watcher", "name", name)
			err := watcher.Run(ctx)
			if err != nil {
				w.log.Error("Failed to run watcher", "name", name, "error", err)
				cancel()
			}
		}(watcher)
	}

	<-ctx.Done()
	w.log.Info("Shutting down worker")

	return nil
}
