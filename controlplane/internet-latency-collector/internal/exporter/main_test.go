package exporter_test

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
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
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level: slog.LevelWarn,
		}))
	}

	os.Exit(m.Run())
}

type mockServiceabilityProgramClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (c *mockServiceabilityProgramClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return c.GetProgramDataFunc(ctx)
}

type mockTelemetryProgramClient struct {
	InitializeInternetLatencySamplesFunc func(ctx context.Context, config telemetry.InitializeInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
	WriteInternetLatencySamplesFunc      func(ctx context.Context, config telemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error)
	GetInternetLatencySamplesFunc        func(ctx context.Context, dataProviderName string, originExchangePK solana.PublicKey, targetExchangePK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

func (c *mockTelemetryProgramClient) InitializeInternetLatencySamples(ctx context.Context, config telemetry.InitializeInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return c.InitializeInternetLatencySamplesFunc(ctx, config)
}

func (c *mockTelemetryProgramClient) WriteInternetLatencySamples(ctx context.Context, config telemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return c.WriteInternetLatencySamplesFunc(ctx, config)
}

func (c *mockTelemetryProgramClient) GetInternetLatencySamples(ctx context.Context, dataProviderName string, originExchangePK solana.PublicKey, targetExchangePK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error) {
	return c.GetInternetLatencySamplesFunc(ctx, dataProviderName, originExchangePK, targetExchangePK, epoch)
}

type mockEpochFinder struct {
	ApproximateAtTimeFunc func(ctx context.Context, target time.Time) (uint64, error)
}

func (f *mockEpochFinder) ApproximateAtTime(ctx context.Context, target time.Time) (uint64, error) {
	return f.ApproximateAtTimeFunc(ctx, target)
}
