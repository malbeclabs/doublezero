package pim

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// RegisterSender periodically sends PIM Register messages (a beacon) for the
// publisher's groups to the RP, so the RP originates the source into MSDP.
// It ignores Register-Stop: there is no inbound path.
type RegisterSender struct {
	done       chan struct{}
	wg         *sync.WaitGroup
	mu         sync.Mutex
	conn       RawConner
	iface      string
	srcOverlay net.IP
	innerSrc   net.IP
	rp         net.IP
	dport      int
	payload    []byte
	groups     []net.IP
}

func NewRegisterSender() *RegisterSender {
	return &RegisterSender{done: make(chan struct{})}
}

func (s *RegisterSender) Start(iface string, srcOverlay, innerSrc net.IP, groups []net.IP, rp net.IP, dport int, payload []byte, interval time.Duration) error {
	c, err := net.ListenPacket("ip4:103", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("register: failed to listen: %v", err)
	}
	r, err := ipv4.NewRawConn(c)
	if err != nil {
		c.Close()
		return fmt.Errorf("register: failed to create raw conn: %v", err)
	}
	if err := r.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		r.Close()
		return fmt.Errorf("register: failed to enable control message: %v", err)
	}
	intf, err := net.InterfaceByName(iface)
	if err != nil {
		r.Close()
		return fmt.Errorf("register: failed to get interface: %v", err)
	}
	s.srcOverlay = srcOverlay
	s.innerSrc = innerSrc
	s.rp = rp
	s.dport = dport
	s.payload = payload
	return s.startWithConn(r, intf, groups, interval)
}

func (s *RegisterSender) startWithConn(conn RawConner, intf *net.Interface, groups []net.IP, interval time.Duration) error {
	s.conn = conn
	s.iface = intf.Name
	s.groups = groups
	s.wg = &sync.WaitGroup{}
	s.wg.Add(1)
	go func() {
		defer conn.Close()
		defer s.wg.Done()

		// Stagger the first send within the interval so publishers across the
		// fleet do not synchronize into CoPP-stressing bursts.
		jitter := time.Duration(rand.Int63n(int64(interval)))
		select {
		case <-time.After(jitter):
		case <-s.done:
			return
		}

		s.sendAll(intf)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sendAll(intf)
			case <-s.done:
				return
			}
		}
	}()
	return nil
}

func (s *RegisterSender) sendAll(intf *net.Interface) {
	s.mu.Lock()
	groups := s.groups
	s.mu.Unlock()
	for _, g := range groups {
		if err := s.sendRegister(intf, g); err != nil {
			slog.Error("failed to send pim register", "group", g, "error", err)
		}
	}
}

func (s *RegisterSender) sendRegister(intf *net.Interface, group net.IP) error {
	buf, err := constructRegisterMessage(s.innerSrc, group, s.dport, s.payload)
	if err != nil {
		return err
	}
	b := buf.Bytes()
	// PIM Register checksum is computed over the first 8 bytes only
	// (PIM header + flags), excluding the encapsulated datagram (RFC 7761 4.9.1).
	binary.BigEndian.PutUint16(b[2:4], Checksum(b[:8]))

	iph := &ipv4.Header{
		Version:  4,
		Len:      ipv4.HeaderLen,
		TTL:      32,
		Protocol: 103,
		Src:      s.srcOverlay,
		Dst:      s.rp,
		TotalLen: ipv4.HeaderLen + len(b),
	}
	// Pin egress to the GRE tunnel interface so no route for the RP is needed.
	cm := &ipv4.ControlMessage{IfIndex: intf.Index}
	return s.conn.WriteTo(iph, b, cm)
}

// UpdateGroups applies a new set of publisher groups in-place; the next beacon
// tick sends Registers for the updated set. It takes only the mutex, so it
// never blocks the caller. A channel handoff would stall the reconciler (which
// calls this) until the sender goroutine is past its startup jitter, up to a
// full interval.
func (s *RegisterSender) UpdateGroups(groups []net.IP) error {
	s.mu.Lock()
	s.groups = groups
	s.mu.Unlock()
	return nil
}

// Close stops the sender goroutine. It signals done rather than closing it, so
// this daemon-lifetime singleton can be restarted by a later Start after a
// teardown or reconnect. It resets wg so a repeated Close is a no-op.
func (s *RegisterSender) Close() error {
	if s.wg != nil {
		s.done <- struct{}{}
		s.wg.Wait()
		s.wg = nil
	}
	return nil
}
