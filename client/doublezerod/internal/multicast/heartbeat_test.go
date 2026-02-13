package multicast

import (
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
)

type mockPacketConn struct {
	mu      sync.Mutex
	writes  []mockWrite
	ttl     int
	ifIndex int
	closed  bool
	writeCh chan mockWrite
}

type mockWrite struct {
	payload []byte
	dst     net.Addr
}

func newMockPacketConn() *mockPacketConn {
	return &mockPacketConn{
		writeCh: make(chan mockWrite, 100),
	}
}

func (m *mockPacketConn) WriteTo(b []byte, _ *ipv4.ControlMessage, dst net.Addr) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	w := mockWrite{payload: cp, dst: dst}
	m.mu.Lock()
	m.writes = append(m.writes, w)
	m.mu.Unlock()
	m.writeCh <- w
	return len(b), nil
}

func (m *mockPacketConn) SetMulticastTTL(ttl int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttl = ttl
	return nil
}

func (m *mockPacketConn) SetMulticastInterface(intf *net.Interface) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ifIndex = intf.Index
	return nil
}

func (m *mockPacketConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockPacketConn) getWrites() []mockWrite {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockWrite, len(m.writes))
	copy(cp, m.writes)
	return cp
}

func (m *mockPacketConn) getTTL() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ttl
}

func TestHeartbeatSender_SendsImmediately(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()

	groups := []net.IP{net.IPv4(239, 0, 0, 1)}
	intf := &net.Interface{Index: 1, Name: "lo0"}

	err := sender.startWithConn(conn, intf, groups, 32, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Wait for the immediate send.
	select {
	case w := <-conn.writeCh:
		udpAddr, ok := w.dst.(*net.UDPAddr)
		if !ok {
			t.Fatalf("expected *net.UDPAddr, got %T", w.dst)
		}
		if !udpAddr.IP.Equal(net.IPv4(239, 0, 0, 1)) {
			t.Errorf("expected dst 239.0.0.1, got %s", udpAddr.IP)
		}
		if udpAddr.Port != HeartbeatPort {
			t.Errorf("expected port %d, got %d", HeartbeatPort, udpAddr.Port)
		}
		if len(w.payload) != len(heartbeatPayload) {
			t.Errorf("expected payload len %d, got %d", len(heartbeatPayload), len(w.payload))
		}
		for i, b := range w.payload {
			if b != heartbeatPayload[i] {
				t.Errorf("payload[%d] = 0x%02x, want 0x%02x", i, b, heartbeatPayload[i])
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for immediate heartbeat")
	}

	sender.Close()
}

func TestHeartbeatSender_SetsTTL(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()

	intf := &net.Interface{Index: 1, Name: "lo0"}
	err := sender.startWithConn(conn, intf, []net.IP{net.IPv4(239, 0, 0, 1)}, 42, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if got := conn.getTTL(); got != 42 {
		t.Errorf("TTL = %d, want 42", got)
	}

	sender.Close()
}

func TestHeartbeatSender_MultipleGroups(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()

	groups := []net.IP{net.IPv4(239, 0, 0, 1), net.IPv4(239, 0, 0, 2)}
	intf := &net.Interface{Index: 1, Name: "lo0"}

	err := sender.startWithConn(conn, intf, groups, 32, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Wait for both immediate sends.
	for i := range 2 {
		select {
		case <-conn.writeCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for heartbeat %d", i)
		}
	}

	writes := conn.getWrites()
	if len(writes) < 2 {
		t.Fatalf("expected at least 2 writes, got %d", len(writes))
	}

	seen := map[string]bool{}
	for _, w := range writes[:2] {
		udpAddr := w.dst.(*net.UDPAddr)
		seen[udpAddr.IP.String()] = true
	}
	if !seen["239.0.0.1"] {
		t.Error("missing heartbeat to 239.0.0.1")
	}
	if !seen["239.0.0.2"] {
		t.Error("missing heartbeat to 239.0.0.2")
	}

	sender.Close()
}

func TestHeartbeatSender_SendsAtInterval(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()

	groups := []net.IP{net.IPv4(239, 0, 0, 1)}
	intf := &net.Interface{Index: 1, Name: "lo0"}

	// Use a short interval for testing.
	err := sender.startWithConn(conn, intf, groups, 32, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Wait for at least 3 sends (1 immediate + 2 ticker).
	for i := range 3 {
		select {
		case <-conn.writeCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for heartbeat %d", i)
		}
	}

	writes := conn.getWrites()
	if len(writes) < 3 {
		t.Errorf("expected at least 3 writes, got %d", len(writes))
	}

	sender.Close()
}

func TestHeartbeatSender_CloseBeforeStart(t *testing.T) {
	sender := NewHeartbeatSender()

	// Close on a never-started sender must not panic or block.
	if err := sender.Close(); err != nil {
		t.Fatalf("Close() on never-started sender returned error: %v", err)
	}
}

func TestHeartbeatSender_RestartAfterClose(t *testing.T) {
	sender := NewHeartbeatSender()
	groups := []net.IP{net.IPv4(239, 0, 0, 1)}

	// First start/close cycle.
	conn1 := newMockPacketConn()
	intf1 := &net.Interface{Index: 1, Name: "lo0"}
	err := sender.startWithConn(conn1, intf1, groups, 32, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	<-conn1.writeCh // drain immediate send
	sender.Close()

	// Second start/close cycle on the same sender instance.
	conn2 := newMockPacketConn()
	intf2 := &net.Interface{Index: 2, Name: "lo0"}
	err = sender.startWithConn(conn2, intf2, groups, 32, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("second start failed: %v", err)
	}

	// Verify heartbeats flow on the new connection.
	select {
	case w := <-conn2.writeCh:
		udpAddr := w.dst.(*net.UDPAddr)
		if !udpAddr.IP.Equal(net.IPv4(239, 0, 0, 1)) {
			t.Errorf("expected dst 239.0.0.1, got %s", udpAddr.IP)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for heartbeat after restart")
	}

	sender.Close()
}

func TestHeartbeatSender_DoubleClose(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()
	intf := &net.Interface{Index: 1, Name: "lo0"}

	err := sender.startWithConn(conn, intf, []net.IP{net.IPv4(239, 0, 0, 1)}, 32, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	<-conn.writeCh // drain immediate send

	sender.Close()
	// Second close must not deadlock or panic.
	sender.Close()
}

func TestHeartbeatSender_CloseStopsSending(t *testing.T) {
	conn := newMockPacketConn()
	sender := NewHeartbeatSender()

	groups := []net.IP{net.IPv4(239, 0, 0, 1)}
	intf := &net.Interface{Index: 1, Name: "lo0"}

	err := sender.startWithConn(conn, intf, groups, 32, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Drain the immediate send.
	<-conn.writeCh

	sender.Close()

	// Record count after close.
	countAfterClose := len(conn.getWrites())

	// Wait a bit and verify no more sends.
	time.Sleep(200 * time.Millisecond)
	countLater := len(conn.getWrites())

	if countLater != countAfterClose {
		t.Errorf("writes continued after Close: %d -> %d", countAfterClose, countLater)
	}

	conn.mu.Lock()
	closed := conn.closed
	conn.mu.Unlock()
	if !closed {
		t.Error("connection was not closed")
	}
}
