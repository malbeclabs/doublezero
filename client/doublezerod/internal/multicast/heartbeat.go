package multicast

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

const (
	// HeartbeatPort is the well-known UDP port for multicast heartbeat packets.
	HeartbeatPort = 5765

	// DefaultHeartbeatTTL is the default IP TTL for heartbeat packets,
	// matching the GRE tunnel TTL configured on devices.
	DefaultHeartbeatTTL = 32

	// DefaultHeartbeatInterval is the default interval between heartbeat packets.
	DefaultHeartbeatInterval = 10 * time.Second
)

// HeartbeatPayload is the fixed payload sent in each heartbeat packet.
// 0x44, 0x5A = "DZ", followed by a 2-byte version (0x00, 0x01).
var HeartbeatPayload = []byte{0x44, 0x5A, 0x00, 0x01}

// PacketConner abstracts the UDP multicast connection for testing.
type PacketConner interface {
	WriteTo(b []byte, cm *ipv4.ControlMessage, dst net.Addr) (int, error)
	SetMulticastTTL(ttl int) error
	SetMulticastInterface(intf *net.Interface) error
	Close() error
}

// HeartbeatSender sends periodic UDP heartbeat packets to multicast groups
// to keep PIM (S, G) state and MSDP SA caches alive in the network.
type HeartbeatSender struct {
	mu   sync.Mutex
	dsts []*net.UDPAddr
	// conn and stop are set while a run is live, and nil otherwise.
	conn PacketConner
	stop chan struct{}
	wg   sync.WaitGroup
}

func NewHeartbeatSender() *HeartbeatSender {
	return &HeartbeatSender{}
}

// Start begins sending heartbeat packets to each multicast group at the given interval.
// srcIP is the source address to bind (the publisher's allocated DZ IP).
func (h *HeartbeatSender) Start(iface string, srcIP net.IP, groups []net.IP, ttl int, interval time.Duration) error {
	intf, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %v", iface, err)
	}

	conn, err := net.ListenPacket("udp4", net.JoinHostPort(srcIP.String(), "0"))
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", srcIP, err)
	}

	p := ipv4.NewPacketConn(conn)
	return h.startWithConn(p, intf, groups, ttl, interval)
}

// UpdateGroups applies a new set of multicast groups in-place without
// restarting the goroutine or connection. It sends an immediate heartbeat
// to the new destinations so callers don't have to wait for the next tick.
func (h *HeartbeatSender) UpdateGroups(groups []net.IP) error {
	dsts := make([]*net.UDPAddr, len(groups))
	for i, group := range groups {
		dsts[i] = &net.UDPAddr{IP: group, Port: HeartbeatPort}
	}

	// Send under the lock, so Close cannot close the connection while this sends.
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dsts = dsts
	if h.conn != nil {
		sendHeartbeats(h.conn, dsts)
	}
	return nil
}

// startWithConn is the internal start method that accepts a pre-built connection for testing.
func (h *HeartbeatSender) startWithConn(p PacketConner, intf *net.Interface, groups []net.IP, ttl int, interval time.Duration) error {
	if err := p.SetMulticastTTL(ttl); err != nil {
		p.Close()
		return fmt.Errorf("failed to set multicast TTL: %v", err)
	}
	if err := p.SetMulticastInterface(intf); err != nil {
		p.Close()
		return fmt.Errorf("failed to set multicast interface: %v", err)
	}

	dsts := make([]*net.UDPAddr, len(groups))
	for i, group := range groups {
		dsts[i] = &net.UDPAddr{IP: group, Port: HeartbeatPort}
	}

	// A Setup that failed after Start can call Start again with no Close, so stop that run first.
	h.Close()

	stop := make(chan struct{})
	h.mu.Lock()
	h.dsts = dsts
	h.conn = p
	h.stop = stop
	h.wg.Add(1)
	h.mu.Unlock()

	go func() {
		// Defers run last first. Close the connection before Done, so Close returns after it.
		defer h.wg.Done()
		defer p.Close()

		// Send immediately before starting ticker so we don't delay by the interval.
		h.mu.Lock()
		currentDsts := h.dsts
		h.mu.Unlock()
		sendHeartbeats(p, currentDsts)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.mu.Lock()
				currentDsts := h.dsts
				h.mu.Unlock()
				sendHeartbeats(p, currentDsts)
			case <-stop:
				return
			}
		}
	}()
	return nil
}

func sendHeartbeats(p PacketConner, dsts []*net.UDPAddr) {
	for _, dst := range dsts {
		if _, err := p.WriteTo(HeartbeatPayload, nil, dst); err != nil {
			slog.Error("failed to send heartbeat", "dst", dst, "error", err)
		}
	}
}

// Close stops the run and returns after its connection is closed. A second or concurrent call does nothing.
func (h *HeartbeatSender) Close() error {
	h.mu.Lock()
	stop := h.stop
	h.stop = nil
	h.conn = nil
	h.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	h.wg.Wait()
	return nil
}
