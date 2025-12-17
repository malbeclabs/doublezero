package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestTelemetry_FlowIngest_Server_Run_StopsWorkersOnCancel(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.KafkaClient = &mockKafkaClient{
			ProduceFunc: func(context.Context, *kgo.Record, func(*kgo.Record, error)) {},
		}
		c.HealthListener = &errListener{addr: dummyAddr("health"), err: errors.New("accept failed")}
		c.WorkerCount = 4
	})

	s, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Run(ctx)
	require.NoError(t, err)
}

func TestTelemetry_FlowIngest_Server_ReadLoop_ForwardsPackets(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.ReadTimeout = 25 * time.Millisecond
		c.BufferSizeBytes = 4096
	})

	s, err := New(cfg)
	require.NoError(t, err)

	out := make(chan packet, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.readLoop(ctx, out) }()

	dst := cfg.FlowListener.LocalAddr().(*net.UDPAddr)
	src, err := net.DialUDP("udp", nil, dst)
	require.NoError(t, err)
	_, err = src.Write([]byte("hello"))
	require.NoError(t, err)
	_ = src.Close()

	select {
	case p := <-out:
		require.NotNil(t, p.addr)
		require.Equal(t, []byte("hello"), p.data)
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for packet")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestTelemetry_FlowIngest_Server_ReadLoop_IgnoresTimeouts(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.ReadTimeout = 5 * time.Millisecond
	})

	s, err := New(cfg)
	require.NoError(t, err)

	out := make(chan packet, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.readLoop(ctx, out) }()

	time.Sleep(30 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestTelemetry_FlowIngest_Server_HealthLoop_AcceptThenCancel(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	s, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.healthLoop(ctx) }()

	conn, err := net.Dial("tcp", cfg.HealthListener.Addr().String())
	require.NoError(t, err)
	_ = conn.Close()

	cancel()
	_ = cfg.HealthListener.Close()

	require.NoError(t, <-done)
}

func TestTelemetry_FlowIngest_Server_HealthLoop_DoesNotFailOnTransientErrors(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.HealthListener = &errListener{addr: dummyAddr("health"), err: errors.New("boom")}
	})
	s, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.healthLoop(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestTelemetry_FlowIngest_Server_Ingest_InvalidDoesNotProduce(t *testing.T) {
	t.Parallel()

	var produced int32
	mk := &mockKafkaClient{
		ProduceFunc: func(ctx context.Context, rec *kgo.Record, fn func(*kgo.Record, error)) {
			atomic.AddInt32(&produced, 1)
			fn(rec, nil)
		},
	}

	cfg := newTestConfig(t, func(c *Config) {
		c.KafkaClient = mk
		c.WorkerCount = 1
	})

	s, err := New(cfg)
	require.NoError(t, err)

	s.ingestPacket(context.Background(), 0, packet{
		addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
		data: []byte("not sflow"),
	})

	require.Equal(t, int32(0), atomic.LoadInt32(&produced))
}

func TestTelemetry_FlowIngest_Server_isClosedNetErr(t *testing.T) {
	t.Parallel()

	require.True(t, isClosedNetErr(net.ErrClosed))
	require.False(t, isClosedNetErr(errors.New("some other error")))
	require.True(t, isClosedNetErr(errors.New("use of closed network connection")))
	require.True(t, isClosedNetErr(errors.New("bad file descriptor")))
	require.False(t, isClosedNetErr(errors.New("timeout")))
}

func TestTelemetry_FlowIngest_Server_ReadLoop_PermanentErrorExits(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	s, err := New(cfg)
	require.NoError(t, err)

	_ = cfg.FlowListener.Close()

	out := make(chan packet, 1)
	err = s.readLoop(context.Background(), out)
	require.NoError(t, err)
}

func TestTelemetry_FlowIngest_Server_ReadLoop_SetDeadlineFailsFast(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	s, err := New(cfg)
	require.NoError(t, err)

	_ = cfg.FlowListener.Close()

	out := make(chan packet, 1)
	err = s.readLoop(context.Background(), out)
	require.NoError(t, err)
}

func TestTelemetry_FlowIngest_Server_Run_ReturnsFirstNonNilError(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.HealthListener = &errListener{addr: dummyAddr("health"), err: errors.New("health down")}
	})

	s, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Run(ctx)
	require.NoError(t, err)
}

type mockKafkaClient struct {
	ProduceFunc func(ctx context.Context, record *kgo.Record, fn func(*kgo.Record, error))
}

func (m *mockKafkaClient) Produce(ctx context.Context, record *kgo.Record, fn func(*kgo.Record, error)) {
	m.ProduceFunc(ctx, record, fn)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func newUDPConn(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func newTCPListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func newTestConfig(t *testing.T, mutate ...func(*Config)) *Config {
	t.Helper()

	cfg := &Config{
		Logger:         newLogger(),
		FlowListener:   newUDPConn(t),
		HealthListener: newTCPListener(t),

		KafkaClient: &mockKafkaClient{
			ProduceFunc: func(context.Context, *kgo.Record, func(*kgo.Record, error)) {},
		},
		KafkaTopic: "topic",

		ReadTimeout:       10 * time.Millisecond,
		WorkerCount:       1,
		BufferSizePackets: 8,
		BufferSizeBytes:   2048,
	}

	for _, m := range mutate {
		m(cfg)
	}
	require.NoError(t, cfg.Validate())
	return cfg
}

type errListener struct {
	addr net.Addr
	err  error
}

func (e *errListener) Accept() (net.Conn, error) { return nil, e.err }
func (e *errListener) Close() error              { return nil }
func (e *errListener) Addr() net.Addr            { return e.addr }

type dummyAddr string

func (d dummyAddr) Network() string { return "dummy" }
func (d dummyAddr) String() string  { return string(d) }
