package twamplight

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
)

var (
	// ErrInvalidPacket is returned when a received packet is malformed.
	ErrInvalidPacket = errors.New("invalid packet format")

	// ErrTimeout is returned when a probe times out.
	ErrTimeout = errors.New("timeout")
)

// Sender sends TWAMP probe packets to a given address and returns the round-trip time (RTT).
//
// It is safe for concurrent use, as each Probe call uses its own ephemeral UDP socket
// and does not modify internal state.
//
// If a probe times out or fails, it returns a zero duration and a non-nil error.
type Sender interface {
	Probe(ctx context.Context) (time.Duration, error)
	Close() error
}

type sender struct {
	log     *slog.Logger
	remote  *net.UDPAddr
	conn    *net.UDPConn
	reader  udp.TimestampedReader
	timeout time.Duration
	once    sync.Once
	seq     uint32     // sequence counter
	seqMu   sync.Mutex // protects seq

	received   map[Packet]struct{}
	receivedMu sync.Mutex
}

func NewSender(ctx context.Context, log *slog.Logger, iface string, localAddr, remoteAddr *net.UDPAddr, timeout time.Duration) (*sender, error) {
	dialer := udp.NewDialer(log)
	conn, err := dialer.Dial(ctx, iface, localAddr, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial UDP: %w", err)
	}

	s := &sender{
		log:      log,
		remote:   remoteAddr,
		conn:     conn,
		reader:   udp.NewTimestampedReader(log, conn),
		timeout:  timeout,
		received: make(map[Packet]struct{}),
	}

	go s.cleanUpReceived(ctx)

	return s, nil
}

func (s *sender) Close() error {
	var err error
	s.once.Do(func() {
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

func (s *sender) cleanUpReceived(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for p := range s.received {
				ts := time.Unix(int64(p.Sec), int64(p.Frac))
				if time.Since(ts) > 5*time.Minute {
					s.receivedMu.Lock()
					delete(s.received, p)
					s.receivedMu.Unlock()
				}
			}
		}
	}
}

// Probe sends a TWAMP probe packet to the given address and returns the RTT.
//
// If the probe fails, it returns a zero duration and an error.
func (s *sender) Probe(ctx context.Context) (time.Duration, error) {
	// Get next sequence number
	s.seqMu.Lock()
	s.seq++
	seq := s.seq
	s.seqMu.Unlock()

	// Create packet.
	packet := NewPacket(seq)
	buf, err := packet.MarshalBinary()
	if err != nil {
		return 0, fmt.Errorf("failed to marshal packet: %w", err)
	}

	// Get start time.
	start := s.reader.Now()

	// Configure read and write deadlines.
	if s.timeout > 0 {
		deadline := time.Now().Add(s.timeout)
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting read deadline: %w", err)
		}
		if err := s.conn.SetWriteDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting write deadline: %w", err)
		}
	} else if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting read deadline: %w", err)
		}
		if err := s.conn.SetWriteDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting write deadline: %w", err)
		}
	}

	// Send probe packet.
	_, err = s.conn.Write(buf)
	if err != nil {
		return 0, fmt.Errorf("failed to write to UDP: %w", err)
	}

	// Read response packet.
	readDone := make(chan struct {
		n        int
		recvTime time.Time
		err      error
	}, 1)
	go func() {
		for {
			n, recvTime, err := s.reader.Read(ctx, buf)
			if err != nil {
				readDone <- struct {
					n        int
					recvTime time.Time
					err      error
				}{n, recvTime, err}
				return
			}

			// Unmarshal packet.
			packet, err := UnmarshalBinary(buf)
			if err != nil {
				readDone <- struct {
					n        int
					recvTime time.Time
					err      error
				}{n, recvTime, err}
				return
			}

			// If we've already received this packet, ignore it.
			s.receivedMu.Lock()
			if _, ok := s.received[*packet]; ok {
				s.receivedMu.Unlock()
				s.log.Debug("Ignoring duplicate packet", "packet", packet)
				continue
			}

			// Add packet to received map.
			s.received[*packet] = struct{}{}
			s.receivedMu.Unlock()

			// Return packet.
			readDone <- struct {
				n        int
				recvTime time.Time
				err      error
			}{n, recvTime, err}
			return
		}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Wait for response packet.
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timeoutCtx.Done():
		return 0, ErrTimeout
	case result := <-readDone:
		if result.err != nil {
			if ne, ok := result.err.(net.Error); ok && ne.Timeout() {
				return 0, context.DeadlineExceeded
			}
			s.log.Debug("failed to read from UDP", "error", result.err)
			return 0, fmt.Errorf("failed to read from UDP: %w", result.err)
		}

		// Validate received packet.
		if result.n != packetSize {
			return 0, ErrInvalidPacket
		}

		// Calculate RTT.
		rtt := result.recvTime.Sub(start)

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
func (s *sender) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}
