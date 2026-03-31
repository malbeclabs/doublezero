package geoprobe

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestICMPPinger(t *testing.T) *ICMPPinger {
	t.Helper()
	p, err := NewICMPPinger(&ICMPPingerConfig{
		Logger: slog.Default(),
	})
	if err != nil {
		t.Skipf("skipping: need CAP_NET_RAW: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func localhostProbeAddr() ProbeAddress {
	return ProbeAddress{Host: "127.0.0.1", Port: 1}
}

func TestNewICMPPinger(t *testing.T) {
	p := newTestICMPPinger(t)
	assert.NotNil(t, p.conn)
	assert.NotZero(t, p.id)
}

func TestICMPPinger_AddRemoveProbe(t *testing.T) {
	p := newTestICMPPinger(t)
	addr := localhostProbeAddr()

	require.NoError(t, p.AddProbe(addr))

	p.mu.RLock()
	_, exists := p.probes[addr.Host]
	p.mu.RUnlock()
	assert.True(t, exists)

	require.NoError(t, p.RemoveProbe(addr))

	p.mu.RLock()
	_, exists = p.probes[addr.Host]
	p.mu.RUnlock()
	assert.False(t, exists)
}

func TestICMPPinger_AddDuplicate(t *testing.T) {
	p := newTestICMPPinger(t)
	addr := localhostProbeAddr()

	require.NoError(t, p.AddProbe(addr))
	require.NoError(t, p.AddProbe(addr))

	p.mu.RLock()
	count := len(p.probes)
	p.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestICMPPinger_RemoveNonexistent(t *testing.T) {
	p := newTestICMPPinger(t)
	addr := localhostProbeAddr()

	err := p.RemoveProbe(addr)
	assert.NoError(t, err)
}

func TestICMPPinger_MeasureOne_Localhost(t *testing.T) {
	p := newTestICMPPinger(t)
	addr := localhostProbeAddr()

	require.NoError(t, p.AddProbe(addr))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rtt, ok := p.MeasureOne(ctx, addr)
	require.True(t, ok, "expected successful ping to localhost")
	assert.Greater(t, rtt, uint64(0))
	assert.Less(t, rtt, uint64(10*time.Millisecond))
}

func TestICMPPinger_MeasureAll_Localhost(t *testing.T) {
	p := newTestICMPPinger(t)
	addr := localhostProbeAddr()

	require.NoError(t, p.AddProbe(addr))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := p.MeasureAll(ctx)
	require.NoError(t, err)
	require.Contains(t, results, addr)

	rtt := results[addr]
	assert.Greater(t, rtt, uint64(0))
	assert.Less(t, rtt, uint64(10*time.Millisecond))
}
