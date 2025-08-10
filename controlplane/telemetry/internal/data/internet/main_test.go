package data_test

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/lmittmann/tint"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

var (
	logger *slog.Logger
)

func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	if verbose {
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.RFC3339,
			AddSource:  true,
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	os.Exit(m.Run())
}

type mockServiceabilityClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

type mockTelemetryClient struct {
	GetInternetLatencySamplesFunc func(ctx context.Context, dataProvider string, originLocationPK, targetLocationPK, collectorPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

func (m *mockTelemetryClient) GetInternetLatencySamples(ctx context.Context, dataProvider string, originLocationPK, targetLocationPK, collectorPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return m.GetInternetLatencySamplesFunc(ctx, dataProvider, originLocationPK, targetLocationPK, collectorPK, epoch)
}

type mockProvider struct {
	GetCircuitsFunc                     func(context.Context) ([]data.Circuit, error)
	GetCircuitLatenciesDownsampledFunc  func(context.Context, string, time.Time, time.Time, uint64, data.Unit, string) ([]stats.CircuitLatencyStat, error)
	GetCircuitLatenciesForTimeRangeFunc func(context.Context, string, time.Time, time.Time, string) ([]stats.CircuitLatencySample, error)
	GetCircuitLatenciesForEpochFunc     func(context.Context, string, uint64, string) ([]stats.CircuitLatencySample, error)
}

func (m *mockProvider) GetCircuits(ctx context.Context) ([]data.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockProvider) GetCircuitLatenciesDownsampled(ctx context.Context, circuit string, from, to time.Time, max uint64, unit data.Unit, dataProvider string) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesDownsampledFunc(ctx, circuit, from, to, max, unit, dataProvider)
}

func (m *mockProvider) GetCircuitLatenciesForTimeRange(ctx context.Context, circuit string, from, to time.Time, dataProvider string) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForTimeRangeFunc(ctx, circuit, from, to, dataProvider)
}

func (m *mockProvider) GetCircuitLatenciesForEpoch(ctx context.Context, circuit string, epoch uint64, dataProvider string) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForEpochFunc(ctx, circuit, epoch, dataProvider)
}

type mockEpochFinder struct {
	ApproximateAtTimeFunc func(ctx context.Context, target time.Time) (uint64, error)
}

func (m *mockEpochFinder) ApproximateAtTime(ctx context.Context, target time.Time) (uint64, error) {
	return m.ApproximateAtTimeFunc(ctx, target)
}
