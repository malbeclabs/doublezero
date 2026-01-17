package dztelemlatency

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type TelemetryRPC interface {
	GetDeviceLatencySamplesTail(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64, existingMaxIdx int) (*telemetry.DeviceLatencySamplesHeader, int, []uint32, error)
	GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

type EpochRPC interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
}

type ViewConfig struct {
	Logger                     *slog.Logger
	Clock                      clockwork.Clock
	TelemetryRPC               TelemetryRPC
	EpochRPC                   EpochRPC
	MaxConcurrency             int
	InternetLatencyAgentPK     solana.PublicKey
	InternetDataProviders      []string
	ClickHouse                 clickhouse.Client
	Serviceability             *dzsvc.View
	RefreshInterval            time.Duration
	ServiceabilityReadyTimeout time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.TelemetryRPC == nil {
		return errors.New("telemetry rpc is required")
	}
	if cfg.EpochRPC == nil {
		return errors.New("epoch rpc is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	if cfg.Serviceability == nil {
		return errors.New("serviceability view is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	if cfg.InternetLatencyAgentPK.IsZero() {
		return errors.New("internet latency agent pk is required")
	}
	if len(cfg.InternetDataProviders) == 0 {
		return errors.New("internet data providers are required")
	}
	if cfg.MaxConcurrency <= 0 {
		return errors.New("max concurrency must be greater than 0")
	}

	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	if cfg.ServiceabilityReadyTimeout <= 0 {
		cfg.ServiceabilityReadyTimeout = 2 * cfg.RefreshInterval
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	store     *Store
	readyOnce sync.Once
	readyCh   chan struct{}
	refreshMu sync.Mutex // prevents concurrent refreshes
}

func NewView(cfg ViewConfig) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := NewStore(StoreConfig{
		Logger:     cfg.Logger,
		ClickHouse: cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		store:   store,
		readyCh: make(chan struct{}),
	}

	return v, nil
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("telemetry/latency: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("telemetry/latency: initial refresh failed", "error", err)
		}
		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				if err := v.Refresh(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					v.log.Error("telemetry/latency: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Debug("telemetry/latency: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("telemetry/latency: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("telemetry").Observe(duration.Seconds())
		if err := recover(); err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
			panic(err)
		}
	}()

	if !v.cfg.Serviceability.Ready() {
		waitCtx, cancel := context.WithTimeout(ctx, v.cfg.ServiceabilityReadyTimeout)
		defer cancel()

		if err := v.cfg.Serviceability.WaitReady(waitCtx); err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
			return fmt.Errorf("serviceability view not ready: %w", err)
		}
	}

	devices, err := dzsvc.QueryCurrentDevices(ctx, v.log, v.cfg.ClickHouse)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
		return fmt.Errorf("failed to query devices: %w", err)
	}

	links, err := dzsvc.QueryCurrentLinks(ctx, v.log, v.cfg.ClickHouse)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
		return fmt.Errorf("failed to query links: %w", err)
	}

	// Refresh device-link latency samples
	if err := v.refreshDeviceLinkTelemetrySamples(ctx, devices, links); err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
		return fmt.Errorf("failed to refresh device-link latency samples: %w", err)
	}

	// Refresh internet-metro latency samples if configured
	if !v.cfg.InternetLatencyAgentPK.IsZero() && len(v.cfg.InternetDataProviders) > 0 {
		metros, err := dzsvc.QueryCurrentMetros(ctx, v.log, v.cfg.ClickHouse)
		if err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
			return fmt.Errorf("failed to query metros: %w", err)
		}

		if err := v.refreshInternetMetroLatencySamples(ctx, metros); err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("telemetry", "error").Inc()
			return fmt.Errorf("failed to refresh internet-metro latency samples: %w", err)
		}
	}

	// Signal readiness once (close channel) - safe to call multiple times
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("telemetry/latency: view is now ready")
	})

	metrics.ViewRefreshTotal.WithLabelValues("telemetry", "success").Inc()
	return nil
}

// Ready returns true if the view has completed at least one successful refresh
func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

// WaitReady waits for the view to be ready (has completed at least one successful refresh)
// It returns immediately if already ready, or blocks until ready or context is cancelled.
func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for telemetry view: %w", ctx.Err())
	}
}
