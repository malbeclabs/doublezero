package multicast

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListener_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Logger:      slog.Default(),
		MulticastIP: "239.0.0.1",
		Port:        5000,
		BufferSize:  65535,
		ReadTimeout: 250 * time.Millisecond,
	}

	l, err := NewListener(cfg)
	require.NoError(t, err)
	assert.NotNil(t, l)
	assert.Equal(t, net.ParseIP("239.0.0.1").To4(), l.multicastIP.To4())
	assert.Equal(t, 5000, l.port)
}

func TestNewListener_NilConfig(t *testing.T) {
	t.Parallel()

	l, err := NewListener(nil)
	require.NoError(t, err)
	assert.NotNil(t, l)
}

func TestNewListener_InvalidMulticastIP(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		MulticastIP: "invalid",
		Port:        5000,
	}

	l, err := NewListener(cfg)
	require.Error(t, err)
	assert.Nil(t, l)
	assert.Contains(t, err.Error(), "invalid multicast IP")
}

func TestNewListener_NonMulticastIP(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		MulticastIP: "192.168.1.1",
		Port:        5000,
	}

	l, err := NewListener(cfg)
	require.Error(t, err)
	assert.Nil(t, l)
	assert.Contains(t, err.Error(), "not a multicast address")
}

func TestListener_Subscribe(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	l, err := NewListener(cfg)
	require.NoError(t, err)

	ch := make(chan Packet, 10)
	unsubscribe := l.Subscribe(ch)

	assert.Equal(t, 1, l.SubscriberCount())

	unsubscribe()
	assert.Equal(t, 0, l.SubscriberCount())
}

func TestListener_Broadcast(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	l, err := NewListener(cfg)
	require.NoError(t, err)

	ch1 := make(chan Packet, 10)
	ch2 := make(chan Packet, 10)

	l.Subscribe(ch1)
	l.Subscribe(ch2)

	pkt := Packet{
		Data:       []byte("test data"),
		ReceivedAt: time.Now(),
	}

	l.broadcast(pkt)

	// Both channels should receive the packet
	select {
	case received := <-ch1:
		assert.Equal(t, pkt.Data, received.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch1 did not receive packet")
	}

	select {
	case received := <-ch2:
		assert.Equal(t, pkt.Data, received.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch2 did not receive packet")
	}
}

func TestListener_BroadcastDropsForSlowSubscribers(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	l, err := NewListener(cfg)
	require.NoError(t, err)

	// Unbuffered channel simulates slow subscriber
	slowCh := make(chan Packet)
	fastCh := make(chan Packet, 10)

	l.Subscribe(slowCh)
	l.Subscribe(fastCh)

	pkt := Packet{
		Data:       []byte("test data"),
		ReceivedAt: time.Now(),
	}

	// This should not block even though slowCh is full
	done := make(chan struct{})
	go func() {
		l.broadcast(pkt)
		close(done)
	}()

	select {
	case <-done:
		// Broadcast completed without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("broadcast blocked on slow subscriber")
	}

	// Fast channel should still receive the packet
	select {
	case received := <-fastCh:
		assert.Equal(t, pkt.Data, received.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("fastCh did not receive packet")
	}
}

func TestListener_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	l, err := NewListener(cfg)
	require.NoError(t, err)

	const numSubscribers = 10
	channels := make([]chan Packet, numSubscribers)
	unsubscribes := make([]func(), numSubscribers)

	for i := 0; i < numSubscribers; i++ {
		channels[i] = make(chan Packet, 10)
		unsubscribes[i] = l.Subscribe(channels[i])
	}

	assert.Equal(t, numSubscribers, l.SubscriberCount())

	pkt := Packet{
		Data:       []byte("broadcast test"),
		ReceivedAt: time.Now(),
	}

	l.broadcast(pkt)

	// All channels should receive the packet
	for i, ch := range channels {
		select {
		case received := <-ch:
			assert.Equal(t, pkt.Data, received.Data)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("channel %d did not receive packet", i)
		}
	}

	// Unsubscribe all
	for _, unsub := range unsubscribes {
		unsub()
	}

	assert.Equal(t, 0, l.SubscriberCount())
}

func TestListener_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	l, err := NewListener(cfg)
	require.NoError(t, err)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ch := make(chan Packet, 1)
			unsub := l.Subscribe(ch)
			time.Sleep(time.Millisecond)
			unsub()
		}()
	}

	wg.Wait()
	assert.Equal(t, 0, l.SubscriberCount())
}

func TestIsTimeout(t *testing.T) {
	t.Parallel()

	// Create a timeout error by using a connection with a very short timeout
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer conn.Close()

	// Set a very short read deadline
	err = conn.SetReadDeadline(time.Now().Add(time.Nanosecond))
	require.NoError(t, err)

	buf := make([]byte, 1)
	_, _, err = conn.ReadFromUDP(buf)
	require.Error(t, err)
	assert.True(t, isTimeout(err))
}

// UDPSimulator helps simulate UDP traffic for testing without actual multicast.
type UDPSimulator struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

// NewUDPSimulator creates a UDP simulator for testing.
func NewUDPSimulator(t *testing.T) (*UDPSimulator, *net.UDPConn) {
	t.Helper()

	// Create a receiver connection on any available port
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)

	// Create a sender connection
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)

	sim := &UDPSimulator{
		conn: sender,
		addr: receiver.LocalAddr().(*net.UDPAddr),
	}

	return sim, receiver
}

// Send sends data to the receiver.
func (s *UDPSimulator) Send(data []byte) error {
	_, err := s.conn.WriteToUDP(data, s.addr)
	return err
}

// Close closes the simulator.
func (s *UDPSimulator) Close() {
	s.conn.Close()
}

func TestUDPSimulator(t *testing.T) {
	t.Parallel()

	sim, receiver := NewUDPSimulator(t)
	defer sim.Close()
	defer receiver.Close()

	testData := []byte("hello world")
	err := sim.Send(testData)
	require.NoError(t, err)

	buf := make([]byte, 1024)
	err = receiver.SetReadDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)

	n, _, err := receiver.ReadFromUDP(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

// TestableListener wraps Listener to allow injecting a UDP connection for testing.
type TestableListener struct {
	*Listener
	conn *net.UDPConn
}

// NewTestableListener creates a listener that uses a provided UDP connection instead of multicast.
func NewTestableListener(t *testing.T) (*TestableListener, *UDPSimulator) {
	t.Helper()

	sim, receiver := NewUDPSimulator(t)

	l := &Listener{
		log:         slog.Default(),
		bufferSize:  65535,
		readTimeout: 50 * time.Millisecond,
		subscribers: make(map[chan<- Packet]struct{}),
	}

	return &TestableListener{
		Listener: l,
		conn:     receiver,
	}, sim
}

// Run runs the testable listener using the injected connection.
func (tl *TestableListener) Run(ctx context.Context) error {
	buf := make([]byte, tl.bufferSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := tl.conn.SetReadDeadline(time.Now().Add(tl.readTimeout)); err != nil {
			continue
		}

		n, _, err := tl.conn.ReadFromUDP(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}

		receivedAt := time.Now()
		data := make([]byte, n)
		copy(data, buf[:n])

		pkt := Packet{
			Data:       data,
			ReceivedAt: receivedAt,
		}

		tl.broadcast(pkt)
	}
}

func TestTestableListener_ReceivesAndBroadcasts(t *testing.T) {
	t.Parallel()

	listener, sim := NewTestableListener(t)
	defer sim.Close()
	defer listener.conn.Close()

	ch := make(chan Packet, 10)
	listener.Subscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the listener
	go func() {
		_ = listener.Run(ctx)
	}()

	// Give the listener time to start
	time.Sleep(10 * time.Millisecond)

	// Send test data
	testData := []byte("test packet data")
	err := sim.Send(testData)
	require.NoError(t, err)

	// Wait for the packet
	select {
	case pkt := <-ch:
		assert.Equal(t, testData, pkt.Data)
		assert.WithinDuration(t, time.Now(), pkt.ReceivedAt, time.Second)
	case <-time.After(time.Second):
		t.Fatal("did not receive packet")
	}
}

func TestTestableListener_MultiplePackets(t *testing.T) {
	t.Parallel()

	listener, sim := NewTestableListener(t)
	defer sim.Close()
	defer listener.conn.Close()

	ch := make(chan Packet, 100)
	listener.Subscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = listener.Run(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	// Send multiple packets
	const numPackets = 50
	for i := 0; i < numPackets; i++ {
		data := []byte(time.Now().String())
		err := sim.Send(data)
		require.NoError(t, err)
	}

	// Receive packets
	received := 0
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case <-ch:
			received++
			if received >= numPackets {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	assert.GreaterOrEqual(t, received, numPackets/2, "should receive at least half of the packets")
}
