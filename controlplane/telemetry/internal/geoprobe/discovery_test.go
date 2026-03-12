package geoprobe

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	geolocation "github.com/malbeclabs/doublezero/sdk/geolocation/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGeolocationClient struct {
	probes     []geolocation.KeyedGeoProbe
	err        error
	keys       []solana.PublicKey
	keysErr    error
	probeCalls atomic.Int32
	keysCalls  atomic.Int32
}

func (m *mockGeolocationClient) GetGeoProbes(_ context.Context) ([]geolocation.KeyedGeoProbe, error) {
	m.probeCalls.Add(1)
	return m.probes, m.err
}

func (m *mockGeolocationClient) GetGeoProbeKeys(_ context.Context) ([]solana.PublicKey, error) {
	m.keysCalls.Add(1)
	return m.keys, m.keysErr
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
		probes: []geolocation.KeyedGeoProbe{
			{GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
			{GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 2},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{otherDevice},
				Code:               "probe2",
			}},
			{GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 3},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice, otherDevice},
				Code:               "probe3",
			}},
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
		probes: []geolocation.KeyedGeoProbe{
			{GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
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
		probes: []geolocation.KeyedGeoProbe{
			{GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
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
		probes: []geolocation.KeyedGeoProbe{},
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

// --- Caching tests ---

func TestDiscovery_CachingSkipsFullFetch(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()
	probeKey := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.KeyedGeoProbe{
			{Pubkey: probeKey, GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
		},
		keys: []solana.PublicKey{probeKey},
	}

	ch := make(chan []ProbeAddress, 1)
	d, err := NewDiscovery(&DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		ProbeUpdateCh: ch,
		Interval:      time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First tick (tickCount=0): 0 % fullRefreshEvery == 0, so full fetch.
	d.discover(ctx)
	assert.Equal(t, int32(1), mock.probeCalls.Load())
	// Key cache is populated from the full fetch result, no separate GetGeoProbeKeys call.
	assert.Equal(t, int32(0), mock.keysCalls.Load())
	<-ch // drain

	// Second tick: key set unchanged → skip full fetch.
	d.discover(ctx)
	assert.Equal(t, int32(1), mock.probeCalls.Load(), "should not call GetGeoProbes again")
	assert.Equal(t, int32(1), mock.keysCalls.Load(), "should call GetGeoProbeKeys for comparison")

	// No update sent on channel since we skipped.
	select {
	case <-ch:
		t.Fatal("should not send update when key set unchanged")
	default:
	}
}

func TestDiscovery_CachingDetectsKeyChange(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()
	probeKey1 := solana.NewWallet().PublicKey()
	probeKey2 := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.KeyedGeoProbe{
			{Pubkey: probeKey1, GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
		},
		keys: []solana.PublicKey{probeKey1},
	}

	ch := make(chan []ProbeAddress, 1)
	d, err := NewDiscovery(&DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		ProbeUpdateCh: ch,
		Interval:      time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First tick: full fetch.
	d.discover(ctx)
	assert.Equal(t, int32(1), mock.probeCalls.Load())
	<-ch // drain

	// Change the key set to simulate a new probe appearing.
	mock.keys = []solana.PublicKey{probeKey1, probeKey2}

	// Second tick: key set changed → triggers full fetch.
	d.discover(ctx)
	assert.Equal(t, int32(2), mock.probeCalls.Load(), "should call GetGeoProbes on key change")
	<-ch // drain
}

func TestDiscovery_ForcedFullRefresh(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()
	probeKey := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.KeyedGeoProbe{
			{Pubkey: probeKey, GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
		},
		keys: []solana.PublicKey{probeKey},
	}

	ch := make(chan []ProbeAddress, 1)
	d, err := NewDiscovery(&DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		ProbeUpdateCh: ch,
		Interval:      time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Tick 0: full fetch (0 % fullRefreshEvery == 0).
	d.discover(ctx)
	<-ch
	assert.Equal(t, int32(1), mock.probeCalls.Load())

	// Ticks 1 through fullRefreshEvery-1: should skip (unchanged keys).
	for i := 1; i < fullRefreshEvery; i++ {
		d.discover(ctx)
	}
	assert.Equal(t, int32(1), mock.probeCalls.Load(), "should not call GetGeoProbes on cached ticks")

	// Tick fullRefreshEvery: forced full refresh (tickCount % fullRefreshEvery == 0 again).
	d.discover(ctx)
	<-ch
	assert.Equal(t, int32(2), mock.probeCalls.Load(), "should force full fetch on Nth tick")
}

func TestDiscovery_KeyFetchErrorFallsBackToFullFetch(t *testing.T) {
	t.Parallel()

	localDevice := solana.NewWallet().PublicKey()
	probeKey := solana.NewWallet().PublicKey()

	mock := &mockGeolocationClient{
		probes: []geolocation.KeyedGeoProbe{
			{Pubkey: probeKey, GeoProbe: geolocation.GeoProbe{
				PublicIP:           [4]uint8{10, 0, 0, 1},
				LocationOffsetPort: 8923,
				ParentDevices:      []solana.PublicKey{localDevice},
				Code:               "probe1",
			}},
		},
		keys: []solana.PublicKey{probeKey},
	}

	ch := make(chan []ProbeAddress, 1)
	d, err := NewDiscovery(&DiscoveryConfig{
		Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Client:        mock,
		LocalDevicePK: localDevice,
		ProbeUpdateCh: ch,
		Interval:      time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First tick: full fetch.
	d.discover(ctx)
	<-ch
	assert.Equal(t, int32(1), mock.probeCalls.Load())

	// Make key fetch fail on the next tick.
	mock.keysErr = errors.New("RPC timeout")

	// Second tick: key fetch fails → falls back to full fetch.
	d.discover(ctx)
	assert.Equal(t, int32(2), mock.probeCalls.Load(), "should fall back to full fetch on key error")
	<-ch
}

func TestPubkeySetsEqual(t *testing.T) {
	t.Parallel()

	k1 := solana.NewWallet().PublicKey()
	k2 := solana.NewWallet().PublicKey()
	k3 := solana.NewWallet().PublicKey()

	tests := []struct {
		name string
		a, b map[solana.PublicKey]struct{}
		want bool
	}{
		{"both empty", map[solana.PublicKey]struct{}{}, map[solana.PublicKey]struct{}{}, true},
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, map[solana.PublicKey]struct{}{}, true},
		{"same single key", pubkeySet([]solana.PublicKey{k1}), pubkeySet([]solana.PublicKey{k1}), true},
		{"same multiple keys", pubkeySet([]solana.PublicKey{k1, k2}), pubkeySet([]solana.PublicKey{k2, k1}), true},
		{"different count", pubkeySet([]solana.PublicKey{k1}), pubkeySet([]solana.PublicKey{k1, k2}), false},
		{"same count different keys", pubkeySet([]solana.PublicKey{k1, k2}), pubkeySet([]solana.PublicKey{k1, k3}), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pubkeySetsEqual(tt.a, tt.b))
		})
	}
}
