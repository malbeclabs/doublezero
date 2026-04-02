//go:build linux

package geoprobe

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// mockICMPSocket implements icmpSocket for testing without CAP_NET_RAW.
type mockICMPSocket struct {
	mu       sync.Mutex
	sent     []mockSentPacket
	replies  []mockReply
	deadline time.Time
	closed   bool
}

type mockSentPacket struct {
	dst     net.IP
	payload []byte
	txTime  time.Time
}

type mockReply struct {
	data   []byte
	rxTime time.Time
}

func (m *mockICMPSocket) sendEcho(dst net.IP, payload []byte) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	txTime := time.Now()
	m.sent = append(m.sent, mockSentPacket{dst: dst, payload: append([]byte(nil), payload...), txTime: txTime})
	return txTime, nil
}

func (m *mockICMPSocket) recvEcho(buf []byte) (int, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.replies) == 0 {
		return 0, time.Time{}, syscall.ETIMEDOUT
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	n := copy(buf, reply.data)
	return n, reply.rxTime, nil
}

func (m *mockICMPSocket) setReadDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadline = t
	return nil
}

func (m *mockICMPSocket) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// buildEchoReply creates a serialized ICMP echo reply for the given ID and seq.
func buildEchoReply(id, seq int) []byte {
	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEchoReply,
		Code: 0,
		Body: &icmp.Echo{ID: id, Seq: seq, Data: make([]byte, icmpPayloadSize)},
	}
	b, _ := msg.Marshal(nil)
	return b
}

func newMockICMPPinger(mock *mockICMPSocket) *ICMPPinger {
	return &ICMPPinger{
		conn:   mock,
		probes: make(map[string]*icmpProbeEntry),
		cfg: &ICMPPingerConfig{
			Logger:       slog.Default(),
			ProbeTimeout: 1 * time.Second,
			BatchSize:    ICMPDefaultBatchSize,
			StaggerDelay: 0, // no delay in tests
		},
		id:  0xBEEF,
		log: slog.Default(),
	}
}

func localhostProbeAddr() ProbeAddress {
	return ProbeAddress{Host: "127.0.0.1", Port: 1}
}

func TestICMPPinger_AddRemoveProbe(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
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
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
	addr := localhostProbeAddr()

	require.NoError(t, p.AddProbe(addr))
	require.NoError(t, p.AddProbe(addr))

	p.mu.RLock()
	count := len(p.probes)
	p.mu.RUnlock()
	assert.Equal(t, 1, count)
}

func TestICMPPinger_RemoveNonexistent(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	err := p.RemoveProbe(localhostProbeAddr())
	assert.NoError(t, err)
}

func TestICMPPinger_AddProbe_RejectsIPv6(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	err := p.AddProbe(ProbeAddress{Host: "::1", Port: 9000})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPv4")
}

func TestICMPPinger_AddProbe_RejectsInvalidHost(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	err := p.AddProbe(ProbeAddress{Host: "notanip", Port: 9000})
	require.Error(t, err)
}

func TestICMPPinger_MeasureOne(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
	addr := localhostProbeAddr()
	require.NoError(t, p.AddProbe(addr))

	// Pre-load a matching reply. The pinger ID is 0xBEEF, seq will be 1.
	mock.mu.Lock()
	mock.replies = append(mock.replies, mockReply{
		data:   buildEchoReply(0xBEEF, 1),
		rxTime: time.Now().Add(500 * time.Microsecond),
	})
	mock.mu.Unlock()

	ctx := context.Background()
	rtt, ok := p.MeasureOne(ctx, addr)
	require.True(t, ok)
	assert.Greater(t, rtt, uint64(0))

	mock.mu.Lock()
	assert.Len(t, mock.sent, 1)
	mock.mu.Unlock()
}

func TestICMPPinger_MeasureOne_UnknownProbe(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	_, ok := p.MeasureOne(context.Background(), localhostProbeAddr())
	assert.False(t, ok)
}

func TestICMPPinger_MeasureOne_Timeout(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
	addr := localhostProbeAddr()
	require.NoError(t, p.AddProbe(addr))

	// No replies -> timeout
	_, ok := p.MeasureOne(context.Background(), addr)
	assert.False(t, ok)
}

func TestICMPPinger_MeasureOne_IgnoresMismatchedSeq(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
	addr := localhostProbeAddr()
	require.NoError(t, p.AddProbe(addr))

	// Reply with wrong seq, then correct seq
	mock.mu.Lock()
	mock.replies = append(mock.replies,
		mockReply{data: buildEchoReply(0xBEEF, 9999), rxTime: time.Now()},
		mockReply{data: buildEchoReply(0xBEEF, 1), rxTime: time.Now().Add(100 * time.Microsecond)},
	)
	mock.mu.Unlock()

	rtt, ok := p.MeasureOne(context.Background(), addr)
	require.True(t, ok)
	assert.Greater(t, rtt, uint64(0))
}

func TestICMPPinger_MeasureOne_IgnoresMismatchedID(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()
	addr := localhostProbeAddr()
	require.NoError(t, p.AddProbe(addr))

	// Reply with wrong ID -> falls through to timeout
	mock.mu.Lock()
	mock.replies = append(mock.replies,
		mockReply{data: buildEchoReply(0xDEAD, 1), rxTime: time.Now()},
	)
	mock.mu.Unlock()

	_, ok := p.MeasureOne(context.Background(), addr)
	assert.False(t, ok)
}

func TestICMPPinger_MeasureAll(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	addr1 := ProbeAddress{Host: "1.2.3.4", Port: 1}
	addr2 := ProbeAddress{Host: "5.6.7.8", Port: 1}
	require.NoError(t, p.AddProbe(addr1))
	require.NoError(t, p.AddProbe(addr2))

	// Pre-load replies for seq 1 and 2
	mock.mu.Lock()
	now := time.Now()
	mock.replies = append(mock.replies,
		mockReply{data: buildEchoReply(0xBEEF, 1), rxTime: now.Add(200 * time.Microsecond)},
		mockReply{data: buildEchoReply(0xBEEF, 2), rxTime: now.Add(400 * time.Microsecond)},
	)
	mock.mu.Unlock()

	results, err := p.MeasureAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestICMPPinger_MeasureAll_Empty(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	defer p.Close()

	results, err := p.MeasureAll(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestICMPPinger_MeasureAll_ContextCancelled(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)
	p.cfg.StaggerDelay = 10 * time.Millisecond
	defer p.Close()

	for i := 1; i <= 5; i++ {
		require.NoError(t, p.AddProbe(ProbeAddress{Host: net.IPv4(1, 2, 3, byte(i)).String(), Port: 1}))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.MeasureAll(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestICMPPinger_Close(t *testing.T) {
	mock := &mockICMPSocket{}
	p := newMockICMPPinger(mock)

	require.NoError(t, p.AddProbe(localhostProbeAddr()))
	require.NoError(t, p.Close())

	p.mu.RLock()
	assert.Empty(t, p.probes)
	p.mu.RUnlock()

	mock.mu.Lock()
	assert.True(t, mock.closed)
	mock.mu.Unlock()
}

// --- Integration tests (require CAP_NET_RAW) ---

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

func TestICMPPinger_Integration_MeasureOne_Localhost(t *testing.T) {
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

func TestICMPPinger_Integration_MeasureAll_Localhost(t *testing.T) {
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
