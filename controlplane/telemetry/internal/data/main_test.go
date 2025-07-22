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
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
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
	LoadFunc       func(ctx context.Context) error
	GetDevicesFunc func() []serviceability.Device
	GetLinksFunc   func() []serviceability.Link
}

func (m *mockServiceabilityClient) Load(ctx context.Context) error {
	return m.LoadFunc(ctx)
}

func (m *mockServiceabilityClient) GetDevices() []serviceability.Device {
	return m.GetDevicesFunc()
}

func (m *mockServiceabilityClient) GetLinks() []serviceability.Link {
	return m.GetLinksFunc()
}

type mockTelemetryClient struct {
	GetDeviceLatencySamplesFunc func(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error)
}

func (m *mockTelemetryClient) GetDeviceLatencySamples(ctx context.Context, originDevicePubKey, targetDevicePubKey, linkPubKey solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
	return m.GetDeviceLatencySamplesFunc(ctx, originDevicePubKey, targetDevicePubKey, linkPubKey, epoch)
}

type mockProvider struct {
	GetCircuitsFunc                    func(context.Context) ([]data.Circuit, error)
	GetCircuitLatenciesDownsampledFunc func(context.Context, string, time.Time, time.Time, uint64) ([]data.CircuitLatencyStat, error)
	GetCircuitLatenciesFunc            func(context.Context, string, time.Time, time.Time) ([]data.CircuitLatencySample, error)
	GetCircuitLatenciesForEpochFunc    func(context.Context, string, uint64) ([]data.CircuitLatencySample, error)
}

func (m *mockProvider) GetCircuits(ctx context.Context) ([]data.Circuit, error) {
	return m.GetCircuitsFunc(ctx)
}

func (m *mockProvider) GetCircuitLatenciesDownsampled(ctx context.Context, circuit string, from, to time.Time, max uint64) ([]data.CircuitLatencyStat, error) {
	return m.GetCircuitLatenciesDownsampledFunc(ctx, circuit, from, to, max)
}

func (m *mockProvider) GetCircuitLatencies(ctx context.Context, circuit string, from, to time.Time) ([]data.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesFunc(ctx, circuit, from, to)
}

func (m *mockProvider) GetCircuitLatenciesForEpoch(ctx context.Context, circuit string, epoch uint64) ([]data.CircuitLatencySample, error) {
	return m.GetCircuitLatenciesForEpochFunc(ctx, circuit, epoch)
}
