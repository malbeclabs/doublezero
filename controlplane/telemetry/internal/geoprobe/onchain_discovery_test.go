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
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock types ---

type mockGeoProbeAccountClient struct {
	probe *geolocation.GeoProbe
	err   error
	calls int
}

func (m *mockGeoProbeAccountClient) GetGeoProbeByPubkey(_ context.Context, _ solana.PublicKey) (*geolocation.GeoProbe, error) {
	m.calls++
	return m.probe, m.err
}

type mockDeviceResolver struct {
	devices map[solana.PublicKey]*serviceability.Device
	err     error
	calls   int
}

func (m *mockDeviceResolver) GetDevice(_ context.Context, pubkey solana.PublicKey) (*serviceability.Device, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	dev, ok := m.devices[pubkey]
	if !ok {
		return nil, errors.New("device not found")
	}
	return dev, nil
}

// --- helpers ---

func newTestParentDiscoveryConfig() *ParentDiscoveryConfig {
	return &ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         &mockGeoProbeAccountClient{},
		Resolver:       &mockDeviceResolver{},
		GeoProbePubkey: solana.NewWallet().PublicKey(),
		Interval:       10 * time.Millisecond,
	}
}

// --- NewParentDiscovery tests ---

func TestNewParentDiscovery(t *testing.T) {
	t.Parallel()
	cfg := newTestParentDiscoveryConfig()
	pd, err := NewParentDiscovery(cfg)
	require.NoError(t, err)
	require.NotNil(t, pd)
}

func TestNewParentDiscovery_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*ParentDiscoveryConfig)
		wantErr string
	}{
		{"nil logger", func(c *ParentDiscoveryConfig) { c.Logger = nil }, "logger is required"},
		{"nil client", func(c *ParentDiscoveryConfig) { c.Client = nil }, "geoprobe account client is required"},
		{"nil resolver", func(c *ParentDiscoveryConfig) { c.Resolver = nil }, "device resolver is required"},
		{"zero pubkey", func(c *ParentDiscoveryConfig) { c.GeoProbePubkey = solana.PublicKey{} }, "geoprobe pubkey is required"},
		{"zero interval", func(c *ParentDiscoveryConfig) { c.Interval = 0 }, "interval must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestParentDiscoveryConfig()
			tt.modify(cfg)
			pd, err := NewParentDiscovery(cfg)
			assert.Error(t, err)
			assert.Nil(t, pd)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// --- discover() tests ---

func TestParentDiscovery_HappyPath(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	parentDevice1PK := solana.NewWallet().PublicKey()
	parentDevice2PK := solana.NewWallet().PublicKey()
	var metricsKey1, metricsKey2 [32]byte
	copy(metricsKey1[:], solana.NewWallet().PublicKey()[:])
	copy(metricsKey2[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			AccountType:   geolocation.AccountTypeGeoProbe,
			ParentDevices: []solana.PublicKey{parentDevice1PK, parentDevice2PK},
		},
	}

	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			parentDevice1PK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: metricsKey1,
			},
			parentDevice2PK: {
				PublicIp:               [4]uint8{10, 0, 0, 2},
				MetricsPublisherPubKey: metricsKey2,
			},
		},
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	select {
	case update := <-ch:
		assert.Len(t, update.Authorities, 2)
		assert.Equal(t, metricsKey1, update.Authorities[parentDevice1PK])
		assert.Equal(t, metricsKey2, update.Authorities[parentDevice2PK])
		assert.Len(t, update.AllowedKeys, 2)
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_MergeWithCLIParents(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	onchainParentPK := solana.NewWallet().PublicKey()
	cliParentPK := solana.NewWallet().PublicKey()
	var onchainMetricsKey, cliMetricsKey [32]byte
	copy(onchainMetricsKey[:], solana.NewWallet().PublicKey()[:])
	copy(cliMetricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{onchainParentPK},
		},
	}

	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			onchainParentPK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: onchainMetricsKey,
			},
		},
	}

	cliParents := map[[32]byte][32]byte{
		cliParentPK: cliMetricsKey,
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		CLIParents:     cliParents,
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	select {
	case update := <-ch:
		assert.Len(t, update.Authorities, 2, "should have onchain + CLI parent")
		assert.Equal(t, onchainMetricsKey, update.Authorities[onchainParentPK])
		assert.Equal(t, cliMetricsKey, update.Authorities[cliParentPK])
		assert.Len(t, update.AllowedKeys, 2)
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_GeoProbeNotFound(t *testing.T) {
	t.Parallel()

	client := &mockGeoProbeAccountClient{
		err: geolocation.ErrAccountNotFound,
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       &mockDeviceResolver{},
		GeoProbePubkey: solana.NewWallet().PublicKey(),
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	// Should receive CLI-only update (empty since no CLI parents).
	select {
	case update := <-ch:
		assert.Empty(t, update.Authorities)
		assert.Empty(t, update.AllowedKeys)
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_GeoProbeNotFound_WithCLIParents(t *testing.T) {
	t.Parallel()

	cliParentPK := solana.NewWallet().PublicKey()
	var cliMetricsKey [32]byte
	copy(cliMetricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		err: geolocation.ErrAccountNotFound,
	}

	cliParents := map[[32]byte][32]byte{
		cliParentPK: cliMetricsKey,
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       &mockDeviceResolver{},
		GeoProbePubkey: solana.NewWallet().PublicKey(),
		CLIParents:     cliParents,
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	select {
	case update := <-ch:
		assert.Len(t, update.Authorities, 1)
		assert.Equal(t, cliMetricsKey, update.Authorities[cliParentPK])
		assert.Len(t, update.AllowedKeys, 1)
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_DeviceResolutionFailure(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	goodParentPK := solana.NewWallet().PublicKey()
	badParentPK := solana.NewWallet().PublicKey()
	var goodMetricsKey [32]byte
	copy(goodMetricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{goodParentPK, badParentPK},
		},
	}

	// Only the good parent has a device, bad one returns error.
	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			goodParentPK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: goodMetricsKey,
			},
		},
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	select {
	case update := <-ch:
		// Only good parent should be present.
		assert.Len(t, update.Authorities, 1)
		assert.Equal(t, goodMetricsKey, update.Authorities[goodParentPK])
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_Caching(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	parentPK := solana.NewWallet().PublicKey()
	var metricsKey [32]byte
	copy(metricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{parentPK},
		},
	}

	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			parentPK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: metricsKey,
			},
		},
	}

	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		Interval:       time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First tick (tickCount=0): forced full refresh.
	update, err := pd.discover(ctx)
	require.NoError(t, err)
	require.NotNil(t, update)
	assert.Len(t, update.Authorities, 1)
	assert.Equal(t, 1, resolver.calls)

	// Second tick (tickCount=1): parent set unchanged → returns nil (no update).
	update, err = pd.discover(ctx)
	require.NoError(t, err)
	assert.Nil(t, update, "should skip when parent set unchanged")
	assert.Equal(t, 1, resolver.calls, "should not resolve devices again")
}

func TestParentDiscovery_ForcedFullRefresh(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	parentPK := solana.NewWallet().PublicKey()
	var metricsKey [32]byte
	copy(metricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{parentPK},
		},
	}

	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			parentPK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: metricsKey,
			},
		},
	}

	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		Interval:       time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Tick 0: full refresh.
	update, _ := pd.discover(ctx)
	require.NotNil(t, update)

	// Ticks 1 through parentDiscoveryFullRefreshEvery-1: skip.
	for i := 1; i < parentDiscoveryFullRefreshEvery; i++ {
		update, _ = pd.discover(ctx)
		assert.Nil(t, update, "tick %d should skip", i)
	}

	// Tick parentDiscoveryFullRefreshEvery: forced full refresh.
	update, _ = pd.discover(ctx)
	require.NotNil(t, update, "forced refresh tick should produce update")
	assert.Len(t, update.Authorities, 1)
}

func TestParentDiscovery_RPCError(t *testing.T) {
	t.Parallel()

	client := &mockGeoProbeAccountClient{
		err: errors.New("RPC connection failed"),
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       &mockDeviceResolver{},
		GeoProbePubkey: solana.NewWallet().PublicKey(),
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	// On RPC error (not ErrAccountNotFound), no update should be sent.
	select {
	case <-ch:
		t.Fatal("should not receive update on RPC error")
	case <-time.After(30 * time.Millisecond):
		// Expected: no update sent.
	}
}

func TestParentDiscovery_EmptyParentDevices(t *testing.T) {
	t.Parallel()

	cliParentPK := solana.NewWallet().PublicKey()
	var cliMetricsKey [32]byte
	copy(cliMetricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{}, // empty onchain
		},
	}

	cliParents := map[[32]byte][32]byte{
		cliParentPK: cliMetricsKey,
	}

	ch := make(chan ParentUpdate, 1)
	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       &mockDeviceResolver{},
		GeoProbePubkey: solana.NewWallet().PublicKey(),
		CLIParents:     cliParents,
		Interval:       10 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pd.Run(ctx, ch)

	select {
	case update := <-ch:
		// Only CLI parent should be present.
		assert.Len(t, update.Authorities, 1)
		assert.Equal(t, cliMetricsKey, update.Authorities[cliParentPK])
	case <-ctx.Done():
		t.Fatal("timed out waiting for parent update")
	}
}

func TestParentDiscovery_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := newTestParentDiscoveryConfig()
	pd, err := NewParentDiscovery(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	ch := make(chan ParentUpdate, 1)

	done := make(chan struct{})
	go func() {
		pd.Run(ctx, ch)
		close(done)
	}()

	select {
	case <-done:
		// Run exited cleanly on context cancellation.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run did not exit on context cancellation")
	}
}

func TestParentDiscovery_CLIDedupWithOnchain(t *testing.T) {
	t.Parallel()

	geoProbePK := solana.NewWallet().PublicKey()
	sharedParentPK := solana.NewWallet().PublicKey()
	var onchainMetricsKey, cliMetricsKey [32]byte
	copy(onchainMetricsKey[:], solana.NewWallet().PublicKey()[:])
	copy(cliMetricsKey[:], solana.NewWallet().PublicKey()[:])

	client := &mockGeoProbeAccountClient{
		probe: &geolocation.GeoProbe{
			ParentDevices: []solana.PublicKey{sharedParentPK},
		},
	}

	resolver := &mockDeviceResolver{
		devices: map[solana.PublicKey]*serviceability.Device{
			sharedParentPK: {
				PublicIp:               [4]uint8{10, 0, 0, 1},
				MetricsPublisherPubKey: onchainMetricsKey,
			},
		},
	}

	// CLI parent has the same pubkey but different metrics key.
	cliParents := map[[32]byte][32]byte{
		sharedParentPK: cliMetricsKey,
	}

	pd, err := NewParentDiscovery(&ParentDiscoveryConfig{
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:         client,
		Resolver:       resolver,
		GeoProbePubkey: geoProbePK,
		CLIParents:     cliParents,
		Interval:       time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()
	update, err := pd.discover(ctx)
	require.NoError(t, err)
	require.NotNil(t, update)

	// Onchain should win since it's resolved first, CLI only fills in missing keys.
	assert.Len(t, update.Authorities, 1)
	assert.Equal(t, onchainMetricsKey, update.Authorities[sharedParentPK],
		"onchain metrics key should take precedence over CLI")
}

// --- pubkeySlicesEqual tests ---

func TestPubkeySlicesEqual(t *testing.T) {
	t.Parallel()

	k1 := solana.NewWallet().PublicKey()
	k2 := solana.NewWallet().PublicKey()

	tests := []struct {
		name string
		a, b []solana.PublicKey
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []solana.PublicKey{}, []solana.PublicKey{}, true},
		{"nil vs empty", nil, []solana.PublicKey{}, true},
		{"same single", []solana.PublicKey{k1}, []solana.PublicKey{k1}, true},
		{"same order", []solana.PublicKey{k1, k2}, []solana.PublicKey{k1, k2}, true},
		{"different order", []solana.PublicKey{k1, k2}, []solana.PublicKey{k2, k1}, false},
		{"different length", []solana.PublicKey{k1}, []solana.PublicKey{k1, k2}, false},
		{"different keys", []solana.PublicKey{k1}, []solana.PublicKey{k2}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pubkeySlicesEqual(tt.a, tt.b))
		})
	}
}
