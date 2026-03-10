package geoprobe

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGeolocationClient struct {
	probes []geolocation.GeoProbe
	err    error
}

func (m *mockGeolocationClient) GetGeoProbes(_ context.Context) ([]geolocation.GeoProbe, error) {
	return m.probes, m.err
}

func newTestDiscoveryConfig() *DiscoveryConfig {
	return &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        &mockGeolocationClient{},
		LocalDevicePK: solana.NewWallet().PublicKey(),
		ProbeUpdateCh: make(chan []ProbeAddress, 1),
		Interval:      10 * time.Millisecond,
	}
}

func TestNewDiscovery(t *testing.T) {
	t.Parallel()

	cfg := newTestDiscoveryConfig()
	d, err := NewDiscovery(cfg)
	require.NoError(t, err)
	require.NotNil(t, d)
}

func TestNewDiscovery_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*DiscoveryConfig)
		wantErr string
	}{
		{"nil logger", func(c *DiscoveryConfig) { c.Logger = nil }, "logger is required"},
		{"nil client", func(c *DiscoveryConfig) { c.Client = nil }, "geolocation client is required"},
		{"zero device pk", func(c *DiscoveryConfig) { c.LocalDevicePK = solana.PublicKey{} }, "local device pubkey is required"},
		{"nil channel", func(c *DiscoveryConfig) { c.ProbeUpdateCh = nil }, "probe update channel is required"},
		{"zero interval", func(c *DiscoveryConfig) { c.Interval = 0 }, "interval must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestDiscoveryConfig()
			tt.modify(cfg)
			d, err := NewDiscovery(cfg)
			assert.Error(t, err)
			assert.Nil(t, d)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGeoProbeToAddress(t *testing.T) {
	t.Parallel()

	probe := &geolocation.GeoProbe{
		PublicIP:           [4]uint8{10, 0, 0, 1},
		LocationOffsetPort: 8923,
	}

	addr := GeoProbeToAddress(probe)
	assert.Equal(t, "10.0.0.1", addr.Host)
	assert.Equal(t, uint16(8923), addr.Port)
	assert.Equal(t, uint16(8925), addr.TWAMPPort)
}

func TestGeoProbeToAddress_ZeroIP(t *testing.T) {
	t.Parallel()

	probe := &geolocation.GeoProbe{
		PublicIP:           [4]uint8{0, 0, 0, 0},
		LocationOffsetPort: 8923,
	}

	addr := GeoProbeToAddress(probe)
	assert.Equal(t, "0.0.0.0", addr.Host)
}

func TestDiscovery_FiltersParentDevices(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()
	otherDevice := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.GeoProbe{
			{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			},
			{
				PublicIP:           [4]uint8{10, 0, 0, 2},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{otherDevice},
				Code:               "probe2",
			},
			{
				PublicIP:           [4]uint8{10, 0, 0, 3},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice, otherDevice},
				Code:               "probe3",
			},
		},
	}

	ch := make(chan []ProbeAddress, 1)
	cfg := &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		ProbeUpdateCh: ch,
		Interval:      10 * time.Millisecond,
	}

	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = d.Run(ctx)
	}()

	// Wait for the first discovery tick (immediate).
	select {
	case probes := <-ch:
		assert.Len(t, probes, 2)
		hosts := make(map[string]bool)
		for _, p := range probes {
			hosts[p.Host] = true
		}
		assert.True(t, hosts["10.0.0.1"])
		assert.True(t, hosts["10.0.0.3"])
		assert.False(t, hosts["10.0.0.2"])
	case <-ctx.Done():
		t.Fatal("timed out waiting for probe update")
	}
}

func TestDiscovery_MergeWithCLIProbes(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.GeoProbe{
			{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			},
		},
	}

	cliProbe := ProbeAddress{Host: "192.168.1.1", Port: 8923, TWAMPPort: 8925}

	ch := make(chan []ProbeAddress, 1)
	cfg := &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		InitialProbes: []ProbeAddress{cliProbe},
		ProbeUpdateCh: ch,
		Interval:      10 * time.Millisecond,
	}

	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = d.Run(ctx)
	}()

	select {
	case probes := <-ch:
		assert.Len(t, probes, 2)
		hosts := make(map[string]bool)
		for _, p := range probes {
			hosts[p.Host] = true
		}
		assert.True(t, hosts["192.168.1.1"], "CLI probe should be included")
		assert.True(t, hosts["10.0.0.1"], "onchain probe should be included")
	case <-ctx.Done():
		t.Fatal("timed out waiting for probe update")
	}
}

func TestDiscovery_DeduplicatesCLIAndOnchain(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.GeoProbe{
			{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			},
		},
	}

	// CLI probe with same address as onchain probe.
	cliProbe := ProbeAddress{Host: "10.0.0.1", Port: 8923, TWAMPPort: 8925}

	ch := make(chan []ProbeAddress, 1)
	cfg := &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		InitialProbes: []ProbeAddress{cliProbe},
		ProbeUpdateCh: ch,
		Interval:      10 * time.Millisecond,
	}

	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = d.Run(ctx)
	}()

	select {
	case probes := <-ch:
		assert.Len(t, probes, 1, "duplicate should be removed")
		assert.Equal(t, "10.0.0.1", probes[0].Host)
	case <-ctx.Done():
		t.Fatal("timed out waiting for probe update")
	}
}

func TestDiscovery_RPCError(t *testing.T) {
	t.Parallel()

	mock := &mockGeolocationClient{
		err: errors.New("RPC connection failed"),
	}

	ch := make(chan []ProbeAddress, 1)
	cfg := &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: solana.NewWallet().PublicKey(),
		ProbeUpdateCh: ch,
		Interval:      10 * time.Millisecond,
	}

	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = d.Run(ctx)
	}()

	// On RPC error, no update should be sent.
	select {
	case <-ch:
		t.Fatal("should not receive update on RPC error")
	case <-time.After(30 * time.Millisecond):
		// Expected: no update sent.
	}
}

func TestDiscovery_EmptyResults_CLIProbesStillSent(t *testing.T) {
	t.Parallel()

	mock := &mockGeolocationClient{
		probes: []geolocation.GeoProbe{},
	}

	cliProbe := ProbeAddress{Host: "192.168.1.1", Port: 8923, TWAMPPort: 8925}

	ch := make(chan []ProbeAddress, 1)
	cfg := &DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: solana.NewWallet().PublicKey(),
		InitialProbes: []ProbeAddress{cliProbe},
		ProbeUpdateCh: ch,
		Interval:      10 * time.Millisecond,
	}

	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = d.Run(ctx)
	}()

	select {
	case probes := <-ch:
		assert.Len(t, probes, 1)
		assert.Equal(t, "192.168.1.1", probes[0].Host)
	case <-ctx.Done():
		t.Fatal("timed out waiting for probe update")
	}
}

func TestDiscovery_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := newTestDiscoveryConfig()
	d, err := NewDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err = d.Run(ctx)
	assert.NoError(t, err)
}

func TestMergeProbes(t *testing.T) {
	t.Parallel()

	a := []ProbeAddress{
		{Host: "10.0.0.1", Port: 8923, TWAMPPort: 8925},
		{Host: "10.0.0.2", Port: 8923, TWAMPPort: 8925},
	}
	b := []ProbeAddress{
		{Host: "10.0.0.2", Port: 8923, TWAMPPort: 8925}, // duplicate
		{Host: "10.0.0.3", Port: 8923, TWAMPPort: 8925},
	}

	merged := mergeProbes(a, b)
	assert.Len(t, merged, 3)

	hosts := make(map[string]bool)
	for _, p := range merged {
		hosts[p.Host] = true
	}
	assert.True(t, hosts["10.0.0.1"])
	assert.True(t, hosts["10.0.0.2"])
	assert.True(t, hosts["10.0.0.3"])
}

func TestMergeProbes_Empty(t *testing.T) {
	t.Parallel()

	merged := mergeProbes(nil, nil)
	assert.Empty(t, merged)

	merged = mergeProbes([]ProbeAddress{{Host: "10.0.0.1", Port: 8923, TWAMPPort: 8925}}, nil)
	assert.Len(t, merged, 1)
}
