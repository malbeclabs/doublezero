package data_test

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/lmittmann/tint"
	devicedata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	inetdata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
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

type mockDeviceProvider struct {
	GetCircuitsFunc                     func(context.Context) ([]devicedata.Circuit, error)
	GetCircuitLatenciesDownsampledFunc  func(context.Context, string, time.Time, time.Time, uint64, devicedata.Unit) ([]stats.CircuitLatencyStat, error)
	GetCircuitLatenciesForTimeRangeFunc func(context.Context, string, time.Time, time.Time) ([]stats.CircuitLatencySample, error)
	GetCircuitLatenciesForEpochFunc     func(context.Context, string, uint64) ([]stats.CircuitLatencySample, error)
}

func (m *mockDeviceProvider) GetCircuits(ctx context.Context) ([]devicedata.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockDeviceProvider) GetCircuitLatenciesDownsampled(ctx context.Context, circuit string, from, to time.Time, max uint64, unit devicedata.Unit) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesDownsampledFunc(ctx, circuit, from, to, max, unit)
}

func (m *mockDeviceProvider) GetCircuitLatenciesForTimeRange(ctx context.Context, circuit string, from, to time.Time) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForTimeRangeFunc(ctx, circuit, from, to)
}

func (m *mockDeviceProvider) GetCircuitLatenciesForEpoch(ctx context.Context, circuit string, epoch uint64) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForEpochFunc(ctx, circuit, epoch)
}

type mockInternetProvider struct {
	GetCircuitsFunc                     func(context.Context) ([]inetdata.Circuit, error)
	GetCircuitLatenciesDownsampledFunc  func(context.Context, string, time.Time, time.Time, uint64, inetdata.Unit, string) ([]stats.CircuitLatencyStat, error)
	GetCircuitLatenciesForTimeRangeFunc func(context.Context, string, time.Time, time.Time, string) ([]stats.CircuitLatencySample, error)
	GetCircuitLatenciesForEpochFunc     func(context.Context, string, uint64, string) ([]stats.CircuitLatencySample, error)
}

func (m *mockInternetProvider) GetCircuits(ctx context.Context) ([]inetdata.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockInternetProvider) GetCircuitLatenciesDownsampled(ctx context.Context, circuit string, from, to time.Time, max uint64, unit inetdata.Unit, dataProvider string) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesDownsampledFunc(ctx, circuit, from, to, max, unit, dataProvider)
}

func (m *mockInternetProvider) GetCircuitLatenciesForTimeRange(ctx context.Context, circuit string, from, to time.Time, dataProvider string) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForTimeRangeFunc(ctx, circuit, from, to, dataProvider)
}

func (m *mockInternetProvider) GetCircuitLatenciesForEpoch(ctx context.Context, circuit string, epoch uint64, dataProvider string) ([]stats.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForEpochFunc(ctx, circuit, epoch, dataProvider)
}
