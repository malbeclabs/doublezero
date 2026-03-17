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
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
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
	GetDeviceLatencySamplesFunc func(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}

func (m *mockTelemetryClient) GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
	return m.GetDeviceLatencySamplesFunc(ctx, originDevicePubKey, targetDevicePubKey, linkPubKey, epoch)
}

type mockProvider struct {
	GetCircuitsFunc           func(context.Context) ([]data.Circuit, error)
	GetCircuitLatenciesFunc   func(context.Context, data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error)
	GetSummaryForCircuitsFunc func(context.Context, data.GetSummaryForCircuitsConfig) ([]data.CircuitSummary, error)
}

func (m *mockProvider) GetCircuits(ctx context.Context) ([]data.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockProvider) GetCircuitLatencies(ctx context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesFunc(ctx, cfg)
}

func (m *mockProvider) GetSummaryForCircuits(ctx context.Context, cfg data.GetSummaryForCircuitsConfig) ([]data.CircuitSummary, error) {
	return m.GetSummaryForCircuitsFunc(ctx, cfg)
}

type mockEpochFinder struct {
	ApproximateAtTimeFunc func(ctx context.Context, target time.Time) (uint64, error)
}

func (m *mockEpochFinder) ApproximateAtTime(ctx context.Context, target time.Time) (uint64, error) {
	return m.ApproximateAtTimeFunc(ctx, target)
}
