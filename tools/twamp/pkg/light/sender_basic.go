package twamplight

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

type BasicSender struct {
	log        *slog.Logger
	remote     *net.UDPAddr
	conn       *net.UDPConn
	once       sync.Once
	cancel     context.CancelFunc
	buf        []byte
	seq        uint32     // sequence counter
	mu         sync.Mutex // protects seq
	nowFunc    func() time.Time
	received   map[Packet]struct{}
	receivedMu sync.Mutex
}

func NewBasicSender(ctx context.Context, log *slog.Logger, iface string, localAddr, remoteAddr *net.UDPAddr) (*BasicSender, error) {
	if iface != "" {
		_, err := net.InterfaceByName(iface)
		if err != nil {
			return nil, fmt.Errorf("failed to dial: %w", err)
		}
	}
	dialer := net.Dialer{
		LocalAddr: localAddr,
	}
	conn, err := dialer.DialContext(ctx, "udp", remoteAddr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &BasicSender{
		log:      log,
		remote:   remoteAddr,
		conn:     conn.(*net.UDPConn),
		cancel:   cancel,
		nowFunc:  time.Now,
		buf:      make([]byte, PacketSize),
		received: make(map[Packet]struct{}),
	}

	go s.cleanUpReceived(ctx)

	return s, nil
}

func (s *BasicSender) Close() error {
	var err error
	s.once.Do(func() {
		s.cancel()
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

// Probe sends a TWAMP probe packet to the given address and returns the RTT.
//
// If the probe fails, it returns a zero duration and an error.
func (s *BasicSender) Probe(ctx context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get next sequence number
	s.seq++

	// Create a packet and marshal it.
	sentPacket := NewPacket(s.seq)
	err := sentPacket.Marshal(s.buf)
	if err != nil {
		return 0, fmt.Errorf("marshal packet: %w", err)
	}

	// Use the context deadline if set, otherwise use a default timeout.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultProbeTimeout)
	}

	// Configure read and write deadlines.
	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return 0, fmt.Errorf("error setting read deadline: %w", err)
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return 0, fmt.Errorf("error setting write deadline: %w", err)
	}

	// Send probe packet.
	sendTime := s.nowFunc()
	_, err = s.conn.Write(s.buf)
	if err != nil {
		return 0, fmt.Errorf("failed to write to UDP: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		// Read response packet.
		n, err := s.conn.Read(s.buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return 0, context.DeadlineExceeded
			}
			s.log.Debug("failed to read from UDP", "error", err)
			return 0, fmt.Errorf("failed to read from UDP: %w", err)
		}

		// Get receive time.
		recvTime := s.nowFunc()

		// Validate received packet.
		if n != PacketSize {
			return 0, ErrInvalidPacket
		}

		// Validate packet.
		packet, err := UnmarshalPacket(s.buf[:n])
		if err != nil {
			return 0, fmt.Errorf("unmarshal packet: %w", err)
		}

		// If we've already received this packet, ignore it.
		s.receivedMu.Lock()
		_, ok := s.received[*packet]
		s.receivedMu.Unlock()
		if ok {
			s.log.Debug("Ignoring duplicate packet", "packet", packet)
			continue
		}

		// Add packet to received set.
		s.receivedMu.Lock()
		s.received[*packet] = struct{}{}
		s.receivedMu.Unlock()

		// Verify that the seq and timestamp match the sent packet.
		if sentPacket.Seq != packet.Seq {
			s.log.Debug("sequence number mismatch", "sent_seq", sentPacket.Seq, "received_seq", packet.Seq)
			continue
		}
		if sentPacket.Sec != packet.Sec {
			s.log.Debug("timestamp seconds mismatch", "sent_sec", sentPacket.Sec, "received_sec", packet.Sec)
			continue
		}
		if sentPacket.Frac != packet.Frac {
			s.log.Debug("timestamp fractional mismatch", "sent_frac", sentPacket.Frac, "received_frac", packet.Frac)
			continue
		}

		// Calculate RTT.
		rtt := recvTime.Sub(sendTime)

		// The send timestamp is captured in user space using CLOCK_REALTIME, while the receive
		// timestamp comes from the kernel via SO_TIMESTAMPNS. Due to clock sampling differences,
		// syscall latency, or NTP adjustments, the kernel timestamp can occasionally appear earlier
		// than the user-space send time. This results in a spurious negative RTT, which we
		// conservatively clamp to 0.
		if rtt < 0 {
			s.log.Warn("Negative RTT detected, clamping to 0", "rtt", rtt)
			rtt = 0
		}

		return rtt, nil
	}
}

// LocalAddr returns the local address of the sender connection.
func (s *BasicSender) LocalAddr() *net.UDPAddr {
	addr := s.conn.LocalAddr().(*net.UDPAddr)
	ip := addr.IP.To4()
	if ip == nil {
		ip = net.IPv4zero
	}
	return &net.UDPAddr{
		IP:   ip,
		Port: addr.Port,
		Zone: addr.Zone,
	}
}

func (s *BasicSender) cleanUpReceived(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.receivedMu.Lock()
			for p := range s.received {
				ts := time.Unix(int64(p.Sec), int64(p.Frac))
				if time.Since(ts) > 5*time.Minute {
					delete(s.received, p)
				}
			}
			s.receivedMu.Unlock()
		}
	}
}
