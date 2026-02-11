package geoprobe

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCoordinatorConfig() *CoordinatorConfig {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	keypair := solana.NewWallet().PrivateKey
	devicePK := solana.NewWallet().PublicKey()

	mockServiceability := newMockServiceabilityClient()
	mockServiceability.setDevicePK(devicePK)

	return &CoordinatorConfig{
		Logger:               logger,
		InitialProbes:        nil,
		ProbeUpdateCh:        nil,
		Interval:             10 * time.Millisecond,
		ProbeTimeout:         100 * time.Millisecond,
		Keypair:              keypair,
		LocalDevicePK:        devicePK,
		ServiceabilityClient: mockServiceability,
		RPCClient:            newMockRPCClient(),
		ManagementNamespace:  "",
	}
}

func TestNewCoordinator(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()

	coordinator, err := NewCoordinator(cfg)

	require.NoError(t, err)
	require.NotNil(t, coordinator)
	assert.NotNil(t, coordinator.log)
	assert.NotNil(t, coordinator.cfg)
	assert.NotNil(t, coordinator.pinger)
	assert.NotNil(t, coordinator.publisher)
	assert.NotNil(t, coordinator.probes)
	assert.Empty(t, coordinator.probes)
}

func TestNewCoordinator_WithInitialProbes(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}

	coordinator, err := NewCoordinator(cfg)

	require.NoError(t, err)
	require.NotNil(t, coordinator)
	assert.Len(t, coordinator.probes, 2)
}

func TestNewCoordinator_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*CoordinatorConfig)
		wantErr string
	}{
		{"nil config", func(_ *CoordinatorConfig) {}, "config is required"},
		{"nil logger", func(c *CoordinatorConfig) { c.Logger = nil }, "logger is required"},
		{"zero interval", func(c *CoordinatorConfig) { c.Interval = 0 }, "interval must be greater than 0"},
		{"zero probe timeout", func(c *CoordinatorConfig) { c.ProbeTimeout = 0 }, "probe timeout must be greater than 0"},
		{"nil keypair", func(c *CoordinatorConfig) { c.Keypair = nil }, "keypair is required"},
		{"zero device pk", func(c *CoordinatorConfig) { c.LocalDevicePK = solana.PublicKey{} }, "local device pubkey is required"},
		{"nil serviceability", func(c *CoordinatorConfig) { c.ServiceabilityClient = nil }, "serviceability client is required"},
		{"nil rpc client", func(c *CoordinatorConfig) { c.RPCClient = nil }, "rpc client is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *CoordinatorConfig
			if tt.name == "nil config" {
				cfg = nil
			} else {
				cfg = newTestCoordinatorConfig()
				tt.modify(cfg)
			}
			coordinator, err := NewCoordinator(cfg)
			assert.Error(t, err)
			assert.Nil(t, coordinator)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCoordinator_HandleProbeUpdate_Add(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx := context.Background()
	newProbes := []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}

	coordinator.handleProbeUpdate(ctx, newProbes)

	assert.Len(t, coordinator.probes, 2)
	assert.Contains(t, coordinator.probes, "127.0.0.1:12345")
	assert.Contains(t, coordinator.probes, "192.0.2.1:12346")
}

func TestCoordinator_HandleProbeUpdate_Remove(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)
	require.Len(t, coordinator.probes, 2)

	ctx := context.Background()
	newProbes := []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
	}

	coordinator.handleProbeUpdate(ctx, newProbes)

	assert.Len(t, coordinator.probes, 1)
	assert.Contains(t, coordinator.probes, "127.0.0.1:12345")
	assert.NotContains(t, coordinator.probes, "192.0.2.1:12346")
}

func TestCoordinator_HandleProbeUpdate_Mixed(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)
	require.Len(t, coordinator.probes, 2)

	ctx := context.Background()
	newProbes := []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "198.51.100.1", Port: 12347},
	}

	coordinator.handleProbeUpdate(ctx, newProbes)

	assert.Len(t, coordinator.probes, 2)
	assert.Contains(t, coordinator.probes, "127.0.0.1:12345")
	assert.Contains(t, coordinator.probes, "198.51.100.1:12347")
	assert.NotContains(t, coordinator.probes, "192.0.2.1:12346")
}

func TestCoordinator_HandleProbeUpdate_Empty(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)
	require.Len(t, coordinator.probes, 2)

	ctx := context.Background()
	newProbes := []ProbeAddress{}

	coordinator.handleProbeUpdate(ctx, newProbes)

	assert.Empty(t, coordinator.probes)
}

func TestCoordinator_RunMeasurementCycle_Empty(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx := context.Background()

	coordinator.runMeasurementCycle(ctx)
}

func TestCoordinator_RunMeasurementCycle_WithProbes(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx := context.Background()

	coordinator.runMeasurementCycle(ctx)
}

func TestCoordinator_Run_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = coordinator.Run(ctx)
	assert.NoError(t, err)
}

func TestCoordinator_Run_ProbeUpdates(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.ProbeUpdateCh = make(chan []ProbeAddress, 1)
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	newProbes := []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
	}
	cfg.ProbeUpdateCh <- newProbes

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err = coordinator.Run(ctx)
	assert.NoError(t, err)

	assert.Len(t, coordinator.probes, 1)
	assert.Contains(t, coordinator.probes, "127.0.0.1:12345")
}

func TestCoordinator_Run_ProbeUpdateChannelClosed(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.ProbeUpdateCh = make(chan []ProbeAddress, 1)
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	close(cfg.ProbeUpdateCh)

	err = coordinator.Run(ctx)
	assert.NoError(t, err)
}

func TestCoordinator_Run_MeasurementCycles(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.Interval = 20 * time.Millisecond
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = coordinator.Run(ctx)
	assert.NoError(t, err)
}

func TestCoordinator_Close(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.InitialProbes = []ProbeAddress{
		{Host: "127.0.0.1", Port: 12345},
		{Host: "192.0.2.1", Port: 12346},
	}
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	err = coordinator.Close()
	assert.NoError(t, err)
}

func TestCoordinator_Close_WithoutInitialProbes(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	err = coordinator.Close()
	assert.NoError(t, err)
}

func TestCoordinator_Concurrent_HandleProbeUpdate(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx := context.Background()

	done := make(chan bool)
	go func() {
		for i := 0; i < 10; i++ {
			newProbes := []ProbeAddress{
				{Host: "127.0.0.1", Port: uint16(12345 + i)},
			}
			coordinator.handleProbeUpdate(ctx, newProbes)
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 5; i++ {
			coordinator.runMeasurementCycle(ctx)
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	<-done
	<-done

	assert.GreaterOrEqual(t, len(coordinator.probes), 0)
}

func TestCoordinator_Run_MultipleProbeUpdates(t *testing.T) {
	t.Parallel()

	cfg := newTestCoordinatorConfig()
	cfg.ProbeUpdateCh = make(chan []ProbeAddress, 10)
	coordinator, err := NewCoordinator(cfg)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cfg.ProbeUpdateCh <- []ProbeAddress{
			{Host: "127.0.0.1", Port: 12345},
		}

		time.Sleep(20 * time.Millisecond)
		cfg.ProbeUpdateCh <- []ProbeAddress{
			{Host: "127.0.0.1", Port: 12345},
			{Host: "192.0.2.1", Port: 12346},
		}

		time.Sleep(20 * time.Millisecond)
		cfg.ProbeUpdateCh <- []ProbeAddress{
			{Host: "192.0.2.1", Port: 12346},
		}
	}()

	err = coordinator.Run(ctx)
	assert.NoError(t, err)

	assert.Len(t, coordinator.probes, 1)
	assert.Contains(t, coordinator.probes, "192.0.2.1:12346")
}
