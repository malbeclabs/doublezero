package serviceability

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

func TestMonitor_Serviceability_Config(t *testing.T) {
	t.Parallel()

	valid := &Config{
		Logger: newTestLogger(t),
		Serviceability: &mockServiceabilityClient{
			GetProgramDataFunc: func(context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{}, nil
			}},
		Interval: 50 * time.Millisecond,
	}

	t.Run("valid config passes", func(t *testing.T) {
		require.NoError(t, valid.Validate())
	})

	t.Run("missing logger fails", func(t *testing.T) {
		c := *valid
		c.Logger = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing serviceability fails", func(t *testing.T) {
		c := *valid
		c.Serviceability = nil
		require.Error(t, c.Validate())
	})

	t.Run("missing interval fails", func(t *testing.T) {
		c := *valid
		c.Interval = 0
		require.Error(t, c.Validate())
	})
}

func newTestLogger(t *testing.T) *slog.Logger {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log = log.With("test", t.Name())
	return log
}

type mockServiceabilityClient struct {
	GetProgramDataFunc                              func(ctx context.Context) (*serviceability.ProgramData, error)
	GetMulticastPublisherBlockResourceExtensionFunc func(ctx context.Context) (*serviceability.ResourceExtension, error)
}

func (m *mockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func (m *mockServiceabilityClient) GetMulticastPublisherBlockResourceExtension(ctx context.Context) (*serviceability.ResourceExtension, error) {
	if m.GetMulticastPublisherBlockResourceExtensionFunc != nil {
		return m.GetMulticastPublisherBlockResourceExtensionFunc(ctx)
	}
	return nil, nil
}
