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
	GetCircuitsFunc           func(context.Context) ([]devicedata.Circuit, error)
	GetCircuitLatenciesFunc   func(context.Context, devicedata.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error)
	GetSummaryForCircuitsFunc func(context.Context, devicedata.GetSummaryForCircuitsConfig) ([]devicedata.CircuitSummary, error)
}

func (m *mockDeviceProvider) GetCircuits(ctx context.Context) ([]devicedata.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockDeviceProvider) GetCircuitLatencies(ctx context.Context, cfg devicedata.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesFunc(ctx, cfg)
}

func (m *mockDeviceProvider) GetSummaryForCircuits(ctx context.Context, cfg devicedata.GetSummaryForCircuitsConfig) ([]devicedata.CircuitSummary, error) {
	return m.GetSummaryForCircuitsFunc(ctx, cfg)
}

type mockInternetProvider struct {
	GetCircuitsFunc         func(context.Context) ([]inetdata.Circuit, error)
	GetCircuitLatenciesFunc func(context.Context, inetdata.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error)
}

func (m *mockInternetProvider) GetCircuits(ctx context.Context) ([]inetdata.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockInternetProvider) GetCircuitLatencies(ctx context.Context, cfg inetdata.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesFunc(ctx, cfg)
}
