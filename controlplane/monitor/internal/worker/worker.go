package worker

import (
	"context"
	"log/slog"

	twozoracle "github.com/malbeclabs/doublezero/controlplane/monitor/internal/2z-oracle"
	devicetelemetry "github.com/malbeclabs/doublezero/controlplane/monitor/internal/device-telemetry"
	internettelemetry "github.com/malbeclabs/doublezero/controlplane/monitor/internal/internet-telemetry"
	"github.com/malbeclabs/doublezero/controlplane/monitor/internal/serviceability"
	solbalance "github.com/malbeclabs/doublezero/controlplane/monitor/internal/sol-balance"
	"github.com/prometheus/client_golang/prometheus"
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

	serviceabilityWatcher, err := serviceability.NewServiceabilityWatcher(&serviceability.Config{
		Logger:          cfg.Logger,
		Serviceability:  cfg.Serviceability,
		Interval:        cfg.Interval,
		SlackWebhookURL: cfg.SlackWebhookURL,
		AllowOwnUsers:   cfg.AllowOwnUsers,
		InfluxWriter:    cfg.InfluxWriter,
		Env:             cfg.Env,
		LedgerRPCClient: cfg.LedgerRPCClient,
		SolanaRPCClient: cfg.SolanaRPCClient,
	})
	if err != nil {
		return nil, err
	}

	deviceTelemetryMetrics := devicetelemetry.NewMetrics()
	deviceTelemetryMetrics.Register(prometheus.DefaultRegisterer)
	deviceTelemetryWatcher, err := devicetelemetry.NewDeviceTelemetryWatcher(&devicetelemetry.Config{
		Logger:          cfg.Logger,
		Metrics:         deviceTelemetryMetrics,
		LedgerRPCClient: cfg.LedgerRPCClient,
		Serviceability:  cfg.Serviceability,
		Telemetry:       cfg.Telemetry,
		Interval:        cfg.Interval,
	})
	if err != nil {
		return nil, err
	}

	internetTelemetryMetrics := internettelemetry.NewMetrics()
	internetTelemetryMetrics.Register(prometheus.DefaultRegisterer)
	internetTelemetryWatcher, err := internettelemetry.NewInternetTelemetryWatcher(&internettelemetry.Config{
		Logger:                     cfg.Logger,
		Metrics:                    internetTelemetryMetrics,
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
		serviceabilityWatcher,
		deviceTelemetryWatcher,
		internetTelemetryWatcher,
	}

	if cfg.TwoZOracleClient != nil {
		twoZOracleWatcher, err := twozoracle.NewTwoZOracleWatcher(&twozoracle.Config{
			Logger:   cfg.Logger,
			Interval: cfg.TwoZOracleInterval,
			Client:   cfg.TwoZOracleClient,
		})
		if err != nil {
			return nil, err
		}
		watchers = append(watchers, twoZOracleWatcher)
	}

	if len(cfg.SolBalanceAccounts) > 0 {
		solBalanceWatcher, err := solbalance.NewSolBalanceWatcher(&solbalance.Config{
			Logger:    cfg.Logger,
			Interval:  cfg.SolBalanceInterval,
			RPCClient: cfg.SolBalanceRPCClient,
			Accounts:  cfg.SolBalanceAccounts,
			Threshold: cfg.SolBalanceThreshold,
		})
		if err != nil {
			return nil, err
		}
		watchers = append(watchers, solBalanceWatcher)
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
