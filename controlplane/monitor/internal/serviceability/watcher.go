package serviceability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	watcherName = "serviceability"
)

type ServiceabilityWatcher struct {
	log *slog.Logger
	cfg *Config
}

func NewServiceabilityWatcher(cfg *Config) (*ServiceabilityWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ServiceabilityWatcher{
		log: cfg.Logger.With("watcher", watcherName),
		cfg: cfg,
	}, nil
}

func (w *ServiceabilityWatcher) Name() string {
	return watcherName
}

func (w *ServiceabilityWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	err := w.Tick(ctx)
	if err != nil {
		w.log.Error("failed to tick", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("context done, stopping")
			return nil
		case <-ticker.C:
			err := w.Tick(ctx)
			if err != nil {
				w.log.Error("failed to tick", "error", err)
			}
		}
	}
}

func (w *ServiceabilityWatcher) Tick(ctx context.Context) error {
	data, err := w.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData).Inc()
		return err
	}

	version := programVersionString(data.ProgramConfig.Version)
	MetricProgramBuildInfo.WithLabelValues(version).Set(1)

	w.log.Debug("serviceability data", "program_version", version)

	return nil
}

func programVersionString(version serviceability.ProgramVersion) string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}
