package dztelem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type TelemetryRPC interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePK, targetDevicePK, linkPK solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
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
	DB                         duck.DB
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
	if cfg.DB == nil {
		return errors.New("database is required")
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
		Logger: cfg.Logger,
		DB:     cfg.DB,
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

	if err := v.store.CreateTablesIfNotExists(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return v, nil
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("telemetry: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("telemetry: initial refresh failed", "error", err)
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
					v.log.Error("telemetry: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Info("telemetry: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("telemetry: refresh completed", "duration", duration.String())
	}()

	// Wait for serviceability view to be ready (has completed at least one refresh)
	if !v.cfg.Serviceability.Ready() {
		waitCtx, cancel := context.WithTimeout(ctx, v.cfg.ServiceabilityReadyTimeout)
		defer cancel()

		if err := v.cfg.Serviceability.WaitReady(waitCtx); err != nil {
			return fmt.Errorf("serviceability view not ready: %w", err)
		}
	}

	// Get devices, links, and contributors from View to compute circuits
	svcStore := v.cfg.Serviceability.Store()
	devices, err := svcStore.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}
	links, err := svcStore.GetLinks()
	if err != nil {
		return fmt.Errorf("failed to get links: %w", err)
	}
	contributors, err := svcStore.GetContributors()
	if err != nil {
		return fmt.Errorf("failed to get contributors: %w", err)
	}

	// Compute and refresh circuits from devices and links
	circuits := ComputeDeviceLinkCircuits(devices, links, contributors)
	if err := v.store.ReplaceDeviceLinkCircuits(ctx, circuits); err != nil {
		return fmt.Errorf("failed to refresh device-link circuits: %w", err)
	}

	// Refresh device-link telemetry samples
	if err := v.refreshDeviceLinkTelemetrySamples(ctx, circuits); err != nil {
		v.log.Warn("failed to refresh device-link telemetry samples", "error", err)
		// Don't fail the entire refresh if telemetry fails
	}

	// Refresh internet-metro latency samples if configured
	if !v.cfg.InternetLatencyAgentPK.IsZero() && len(v.cfg.InternetDataProviders) > 0 {
		metros, err := svcStore.GetMetros()
		if err != nil {
			v.log.Warn("failed to get metros for internet-metro samples", "error", err)
		} else {
			internetCircuits := ComputeInternetMetroCircuits(metros)
			if err := v.refreshInternetMetroLatencySamples(ctx, internetCircuits); err != nil {
				v.log.Warn("failed to refresh internet-metro telemetry samples", "error", err)
				// Don't fail the entire refresh if telemetry fails
			}
		}
	}

	// Signal readiness once (close channel) - safe to call multiple times
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("telemetry: view is now ready")
	})

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
