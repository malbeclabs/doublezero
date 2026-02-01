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
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
)

const (
	defaultCircuitsCacheTTL               = 5 * time.Minute
	defaultCurrentEpochLatenciesCacheTTL  = 1 * time.Minute
	defaultHistoricEpochLatenciesCacheTTL = 24 * time.Hour

	defaultGetCircuitLatenciesPoolSize = 64
)

type Unit string

const (
	UnitMillisecond Unit = "ms"
	UnitMicrosecond Unit = "us"
)

type EpochRange struct {
	From uint64
	To   uint64
}

type TimeRange struct {
	From time.Time
	To   time.Time
}

type GetCircuitLatenciesConfig struct {
	Epochs       *EpochRange
	Time         *TimeRange
	MaxPoints    uint64
	Interval     time.Duration
	Unit         Unit
	Circuit      string
	DataProvider string
}

type Provider interface {
	GetCircuits(ctx context.Context) ([]Circuit, error)
	GetCircuitLatencies(ctx context.Context, cfg GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error)
}

type provider struct {
	log *slog.Logger
	cfg *ProviderConfig

	cache   *ttlcache.Cache[string, any]
	cacheMu sync.RWMutex

	getCircuitLatenciesPool pond.ResultPool[*CircuitLatenciesWithHeader]
}

type ProviderConfig struct {
	Logger               *slog.Logger
	ServiceabilityClient ServiceabilityClient
	TelemetryClient      TelemetryClient
	EpochFinder          epoch.Finder
	AgentPK              solana.PublicKey

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
	if c.AgentPK.IsZero() {
		return errors.New("agent PK is required")
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

	getCircuitLatenciesPool := pond.NewResultPool[*CircuitLatenciesWithHeader](cfg.GetCircuitLatenciesPoolSize)

	return &provider{
		log:   cfg.Logger,
		cfg:   cfg,
		cache: cache,

		getCircuitLatenciesPool: getCircuitLatenciesPool,
	}, nil
}

type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type TelemetryClient interface {
	GetInternetLatencySamples(ctx context.Context, collectorOraclePK solana.PublicKey, dataProvider string, originExchangePK, targetExchangePK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}
