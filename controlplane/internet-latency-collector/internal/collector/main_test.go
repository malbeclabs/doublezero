package collector

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/lmittmann/tint"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
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

type mockServiceabilityClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	if m.GetProgramDataFunc == nil {
		return &serviceability.ProgramData{}, nil
	}
	return m.GetProgramDataFunc(ctx)
}
