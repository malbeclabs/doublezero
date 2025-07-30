package collector

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type WheresitupCollectorInterface interface {
	Run(ctx context.Context, interval time.Duration, dryRun bool, jobIDsFile, stateDir, outputDir string) error
}

type RipeAtlasCollectorInterface interface {
	Run(ctx context.Context, dryRun bool, probesPerLocation int, stateDir, outputDir string, measurementInterval, exportInterval time.Duration) error
}

type Config struct {
	Logger *slog.Logger

	RipeAtlas  RipeAtlasCollectorInterface
	Wheresitup WheresitupCollectorInterface

	WheresitupCollectionInterval time.Duration
	RipeAtlasMeasurementInterval time.Duration
	RipeAtlasExportInterval      time.Duration
	DryRun                       bool
	ProcessedJobsFile            string
	StateDir                     string
	OutputDir                    string
	ProbesPerLocation            int
	MetricsAddr                  string
}

func (cfg *Config) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.Wheresitup == nil {
		return errors.New("wheresitup collector is required")
	}
	if cfg.RipeAtlas == nil {
		return errors.New("ripe atlas collector is required")
	}
	if cfg.WheresitupCollectionInterval <= 0 {
		return errors.New("wheresitup collection interval must be greater than 0")
	}
	if cfg.RipeAtlasMeasurementInterval <= 0 {
		return errors.New("ripe atlas measurement interval must be greater than 0")
	}
	if cfg.RipeAtlasExportInterval <= 0 {
		return errors.New("ripe atlas export interval must be greater than 0")
	}
	if cfg.ProbesPerLocation <= 0 {
		return errors.New("probes per location must be greater than 0")
	}
	if cfg.ProcessedJobsFile == "" {
		return errors.New("processed jobs file is required")
	}
	if cfg.StateDir == "" {
		return errors.New("state directory is required")
	}
	if cfg.OutputDir == "" {
		return errors.New("output directory is required")
	}
	return nil
}

type Collector struct {
	log *slog.Logger
	cfg Config
}

func New(cfg Config) (*Collector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Collector{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}
