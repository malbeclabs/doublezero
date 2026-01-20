package data

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jellydator/ttlcache/v3"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
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

func (t *TimeRange) String() string {
	return fmt.Sprintf("%s to %s", t.From.Format(time.RFC3339), t.To.Format(time.RFC3339))
}

func (e *EpochRange) String() string {
	return fmt.Sprintf("%d to %d", e.From, e.To)
}

type GetCircuitLatenciesConfig struct {
	Epochs    *EpochRange
	Time      *TimeRange
	Interval  time.Duration
	MaxPoints uint64
	Unit      Unit
	Circuit   string
}

type GetSummaryForCircuitsConfig struct {
	Epochs   *EpochRange
	Time     *TimeRange
	Unit     Unit
	Circuits []string
}

type CircuitSummary struct {
	Circuit         string `json:"circuit"`
	LinkType        string `json:"link_type"`
	ContributorCode string `json:"contributor_code"`

	stats.CircuitLatencyStat `json:",inline"`
	CommittedRTT             float64 `json:"committed_rtt"`
	CommittedJitter          float64 `json:"committed_jitter"`

	// Committed RTT and jitter deltas calculated as: committed - measured
	// Positive values mean the measured values are greater than the committed values.
	CommittedRTTDelta    float64 `json:"committed_rtt_delta"`
	CommittedJitterDelta float64 `json:"committed_jitter_delta"`

	// Committed RTT and jitter change ratios calcuated as: committed - measured / committed
	// Positive values mean the measured values are greater than the committed values.
	CommittedRTTChangeRatio    float64 `json:"committed_rtt_change_ratio"`
	CommittedJitterChangeRatio float64 `json:"committed_jitter_change_ratio"`
}

type Provider interface {
	GetCircuits(ctx context.Context) ([]Circuit, error)
	GetCircuitLatencies(ctx context.Context, cfg GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error)
	GetSummaryForCircuits(ctx context.Context, cfg GetSummaryForCircuitsConfig) ([]CircuitSummary, error)
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
	GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}
