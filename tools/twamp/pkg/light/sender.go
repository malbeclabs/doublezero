package twamplight

import (
	"context"
	"encoding/binary"
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
	mu      sync.Mutex // protects seq
}

func NewSender(ctx context.Context, log *slog.Logger, iface string, localAddr, remoteAddr *net.UDPAddr, timeout time.Duration) (*sender, error) {
	dialer := udp.NewDialer(log)
	conn, err := dialer.Dial(ctx, iface, localAddr, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial UDP: %w", err)
	}
	return &sender{
		log:     log,
		remote:  remoteAddr,
		conn:    conn,
		reader:  udp.NewTimestampedReader(log, conn),
		timeout: timeout,
	}, nil
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

// Probe sends a TWAMP probe packet to the given address and returns the RTT.
//
// If the probe fails, it returns a zero duration and an error.
func (s *sender) Probe(ctx context.Context) (time.Duration, error) {
	buf := make([]byte, 48)

	// Get next sequence number
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	binary.BigEndian.PutUint32(buf[0:4], seq)
	sec, frac := ntpTimestamp(time.Now())
	binary.BigEndian.PutUint32(buf[4:8], sec)
	binary.BigEndian.PutUint32(buf[8:12], frac)

	// Get start time.
	start := s.reader.Now()

	// Configure read and write deadlines.
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting read deadline: %w", err)
		}
		if err := s.conn.SetWriteDeadline(deadline); err != nil {
			return 0, fmt.Errorf("error setting write deadline: %w", err)
		}
	}

	// Send probe packet.
	_, err := s.conn.Write(buf)
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
		n, recvTime, err := s.reader.Read(ctx, buf)
		readDone <- struct {
			n        int
			recvTime time.Time
			err      error
		}{n, recvTime, err}
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
		if result.n != 48 {
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
