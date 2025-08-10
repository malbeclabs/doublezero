package data

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jellydator/ttlcache/v3"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/epoch"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

const (
	defaultCircuitsCacheTTL               = 5 * time.Minute
	defaultCurrentEpochLatenciesCacheTTL  = 1 * time.Minute
	defaultHistoricEpochLatenciesCacheTTL = 24 * time.Hour

	defaultGetCircuitLatenciesPoolSize = 16
)

type Unit string

const (
	UnitMillisecond Unit = "ms"
	UnitMicrosecond Unit = "us"
)

type Provider interface {
	GetCircuits(ctx context.Context) ([]Circuit, error)
	GetCircuitLatenciesForTimeRange(ctx context.Context, circuitCode string, from, to time.Time) ([]stats.CircuitLatencySample, error)
	GetCircuitLatenciesDownsampled(ctx context.Context, circuitCode string, from, to time.Time, maxPoints uint64, unit Unit) ([]stats.CircuitLatencyStat, error)
	GetCircuitLatenciesForEpoch(ctx context.Context, circuitCode string, epoch uint64) ([]stats.CircuitLatencySample, error)
}

type provider struct {
	cfg *ProviderConfig

	cache   *ttlcache.Cache[string, any]
	cacheMu sync.RWMutex

	getCircuitLatenciesPool pond.ResultPool[[]stats.CircuitLatencySample]
}

type ProviderConfig struct {
	Logger               *slog.Logger
	ServiceabilityClient ServiceabilityClient
	TelemetryClient      TelemetryClient
	EpochFinder          epoch.Finder

	CircuitsCacheTTL               time.Duration
	HistoricEpochLatenciesCacheTTL time.Duration
	CurrentEpochLatenciesCacheTTL  time.Duration
	GetCircuitLatenciesPoolSize    int
}

func (c *ProviderConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.ServiceabilityClient == nil {
		return errors.New("serviceability client is required")
	}
	if c.TelemetryClient == nil {
		return errors.New("telemetry client is required")
	}
	if c.EpochFinder == nil {
		return errors.New("epoch finder is required")
	}
	if c.CircuitsCacheTTL == 0 {
		c.CircuitsCacheTTL = defaultCircuitsCacheTTL
	}
	if c.CurrentEpochLatenciesCacheTTL == 0 {
		c.CurrentEpochLatenciesCacheTTL = defaultCurrentEpochLatenciesCacheTTL
	}
	if c.HistoricEpochLatenciesCacheTTL == 0 {
		c.HistoricEpochLatenciesCacheTTL = defaultHistoricEpochLatenciesCacheTTL
	}
	if c.GetCircuitLatenciesPoolSize == 0 {
		c.GetCircuitLatenciesPoolSize = defaultGetCircuitLatenciesPoolSize
	}
	return nil
}

func NewProvider(cfg *ProviderConfig) (*provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cache := ttlcache.New(
		ttlcache.WithTTL[string, any](cfg.CircuitsCacheTTL),
	)

	getCircuitLatenciesPool := pond.NewResultPool[[]stats.CircuitLatencySample](cfg.GetCircuitLatenciesPoolSize)

	return &provider{
		cfg:   cfg,
		cache: cache,

		getCircuitLatenciesPool: getCircuitLatenciesPool,
	}, nil
}

type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type TelemetryClient interface {
	GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}
