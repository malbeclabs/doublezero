package collector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Collector_Validate(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	base := func() Config {
		return Config{
			Logger:    log,
			RipeAtlas: &MockRipeAtlasCollector{},

			RipeAtlasSamplingInterval:    10 * time.Minute,
			RipeAtlasMeasurementInterval: 1 * time.Hour,
			RipeAtlasExportInterval:      10 * time.Minute,
			ProbesPerLocation:            1,
			StateDir:                     t.TempDir(),
		}
	}

	tests := []struct {
		name    string
		mutate  func(cfg *Config)
		wantErr string
	}{
		{
			name:   "ripe atlas only",
			mutate: func(cfg *Config) {},
		},
		{
			name: "wheresitup only",
			mutate: func(cfg *Config) {
				cfg.RipeAtlas = nil
				cfg.Wheresitup = &MockWheresitupCollector{}
				cfg.WheresitupSamplingInterval = 6 * time.Minute
				cfg.ProcessedJobsFile = "jobs.json"
			},
		},
		{
			name: "both collectors",
			mutate: func(cfg *Config) {
				cfg.Wheresitup = &MockWheresitupCollector{}
				cfg.WheresitupSamplingInterval = 6 * time.Minute
				cfg.ProcessedJobsFile = "jobs.json"
			},
		},
		{
			name: "no collector",
			mutate: func(cfg *Config) {
				cfg.RipeAtlas = nil
			},
			wantErr: "at least one of the wheresitup and ripe atlas collectors is required",
		},
		{
			name: "cloud mode with ripe atlas only",
			mutate: func(cfg *Config) {
				cfg.CloudMode = true
			},
		},
		{
			name: "cloud mode without ripe atlas",
			mutate: func(cfg *Config) {
				cfg.CloudMode = true
				cfg.RipeAtlas = nil
				cfg.Wheresitup = &MockWheresitupCollector{}
				cfg.WheresitupSamplingInterval = 6 * time.Minute
				cfg.ProcessedJobsFile = "jobs.json"
			},
			wantErr: "ripe atlas collector is required in cloud mode",
		},
		{
			name: "cloud mode with wheresitup",
			mutate: func(cfg *Config) {
				cfg.CloudMode = true
				cfg.Wheresitup = &MockWheresitupCollector{}
				cfg.WheresitupSamplingInterval = 6 * time.Minute
				cfg.ProcessedJobsFile = "jobs.json"
			},
			wantErr: "wheresitup collector is not supported in cloud mode",
		},
		{
			name: "ripe atlas without sampling interval",
			mutate: func(cfg *Config) {
				cfg.RipeAtlasSamplingInterval = 0
			},
			wantErr: "ripe atlas sampling interval must be greater than 0",
		},
		{
			name: "wheresitup without processed jobs file",
			mutate: func(cfg *Config) {
				cfg.Wheresitup = &MockWheresitupCollector{}
				cfg.WheresitupSamplingInterval = 6 * time.Minute
			},
			wantErr: "processed jobs file is required",
		},
		{
			name: "no state dir",
			mutate: func(cfg *Config) {
				cfg.StateDir = ""
			},
			wantErr: "state directory is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestInternetLatency_Collector_Run_RipeAtlasOnly(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockRipe := &MockRipeAtlasCollector{}

	c, err := New(Config{
		Logger:    log,
		RipeAtlas: mockRipe,

		RipeAtlasSamplingInterval:    1 * time.Minute,
		RipeAtlasMeasurementInterval: 1 * time.Hour,
		RipeAtlasExportInterval:      2 * time.Minute,
		DryRun:                       true,
		StateDir:                     t.TempDir(),
		ProbesPerLocation:            1,
		MetricsAddr:                  "127.0.0.1:0",
	})
	require.NoError(t, err)

	require.NoError(t, c.Run(t.Context()))
	require.True(t, mockRipe.wasRunCalled(), "RIPE Atlas collector should have been called")
}

func TestInternetLatency_Collector_Run_WheresitupOnly(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	mockWheresitup := &MockWheresitupCollector{}

	c, err := New(Config{
		Logger:     log,
		Wheresitup: mockWheresitup,

		WheresitupSamplingInterval: 6 * time.Minute,
		DryRun:                     true,
		ProcessedJobsFile:          "jobs.json",
		StateDir:                   t.TempDir(),
		MetricsAddr:                "127.0.0.1:0",
	})
	require.NoError(t, err)

	require.NoError(t, c.Run(t.Context()))
	require.True(t, mockWheresitup.wasRunCalled(), "Wheresitup collector should have been called")
}
