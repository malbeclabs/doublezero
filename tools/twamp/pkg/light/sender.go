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
	// ErrTimeout is returned when a probe times out.
	ErrTimeout = errors.New("timeout")
	// ErrInvalidPacket is returned when a received packet is malformed.
	ErrInvalidPacket = errors.New("invalid packet format")
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
	conn    *net.UDPConn
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
		conn:    conn,
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

	start := time.Now()
	_, err := s.conn.Write(buf)
	if err != nil {
		return 0, fmt.Errorf("failed to write to UDP: %w", err)
	}

	err = s.conn.SetReadDeadline(time.Now().Add(s.timeout))
	if err != nil {
		if isClosedErr(err) {
			s.log.Debug("TWAMP sender socket closed")
			return 0, nil
		}
		return 0, fmt.Errorf("failed to set read deadline: %w", err)
	}

	readDone := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		n, err := s.conn.Read(buf)
		readDone <- struct {
			n   int
			err error
		}{n, err}
	}()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case result := <-readDone:
		if result.err != nil {
			if ne, ok := result.err.(net.Error); ok && ne.Timeout() {
				return 0, ErrTimeout
			}
			s.log.Debug("failed to read from UDP", "error", result.err)
			return 0, fmt.Errorf("failed to read from UDP: %w", result.err)
		}

		// Validate received packet
		if result.n != 48 {
			return 0, ErrInvalidPacket
		}

		return time.Since(start), nil
	}
}

// LocalAddr returns the local address of the sender connection.
func (s *sender) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}
