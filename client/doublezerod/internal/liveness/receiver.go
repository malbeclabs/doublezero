package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"
)

type Receiver struct {
	log      *slog.Logger
	conn     *UDPConn
	handleRx HandleRxFunc

	// Throttled warning for noisy read errors.
	readErrEvery time.Duration
	lastReadWarn time.Time
	mu           sync.Mutex
}

type HandleRxFunc func(pkt *ControlPacket, peer Peer)

func NewReceiver(log *slog.Logger, conn *UDPConn, handleRx HandleRxFunc) *Receiver {
	return &Receiver{
		log:          log,
		conn:         conn,
		handleRx:     handleRx,
		readErrEvery: 5 * time.Second,
	}
}

func (r *Receiver) Run(ctx context.Context) error {
	r.log.Debug("liveness.recv: rx loop started")
	buf := make([]byte, 1500)

	for {
		// Fast path: bail early if we're asked to stop.
		select {
		case <-ctx.Done():
			r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
			return nil
		default:
		}

		// Handle SetReadDeadline errors (e.g., closed socket).
		if err := r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			// If context is already canceled, exit immediately.
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				// Socket is gone, return as error.
				r.log.Debug("liveness.recv: socket closed during SetReadDeadline; exiting")
				return fmt.Errorf("socket closed during SetReadDeadline: %w", err)
			}
			// Throttle noisy warnings.
			now := time.Now()
			r.mu.Lock()
			if r.lastReadWarn.IsZero() || now.Sub(r.lastReadWarn) >= r.readErrEvery {
				r.lastReadWarn = now
				r.mu.Unlock()
				r.log.Warn("liveness.recv: SetReadDeadline error", "error", err)
			} else {
				r.mu.Unlock()
			}

			// Treat fatal kernel/socket conditions as terminal.
			if isFatalNetErr(err) {
				return fmt.Errorf("fatal network error during SetReadDeadline: %w", err)
			}

			// Brief wait to avoid a hot loop on repeated errors.
			time.Sleep(50 * time.Millisecond)
			continue
		}

		n, remoteAddr, localIP, ifname, err := r.conn.ReadFrom(buf)
		if err != nil {
			// If context is already canceled, exit immediately regardless of error type.
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return nil
			default:
			}

			// Deadline tick.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}

			// Closed socket => terminate without spinning.
			if errors.Is(err, net.ErrClosed) {
				r.log.Debug("liveness.recv: socket closed; exiting")
				return fmt.Errorf("socket closed during ReadFrom: %w", err)
			}

			// Throttle non-timeout read errors to avoid log spam.
			now := time.Now()
			r.mu.Lock()
			if r.lastReadWarn.IsZero() || now.Sub(r.lastReadWarn) >= r.readErrEvery {
				r.lastReadWarn = now
				r.mu.Unlock()
				r.log.Warn("liveness.recv: non-timeout read error", "error", err)
			} else {
				r.mu.Unlock()
			}

			if isFatalNetErr(err) {
				return fmt.Errorf("fatal network error during ReadFrom: %w", err)
			}
			continue
		}

		ctrl, err := UnmarshalControlPacket(buf[:n])
		if err != nil {
			r.log.Error("liveness.recv: error parsing control packet", "error", err)
			continue
		}
		peer := Peer{Interface: ifname, LocalIP: localIP.String(), RemoteIP: remoteAddr.IP.String()}
		r.handleRx(ctrl, peer)
	}
}

func isFatalNetErr(err error) bool {
	// Closed socket.
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	// Syscall-level fatal hints.
	var se syscall.Errno
	if errors.As(err, &se) {
		switch se {
		case syscall.EBADF, syscall.ENETDOWN, syscall.ENODEV, syscall.ENXIO:
			return true
		}
	}

	// Some platforms wrap the above in *net.OpError; treat non-temporary, non-timeout as fatal.
	var oe *net.OpError
	if errors.As(err, &oe) && !oe.Timeout() && !oe.Temporary() {
		return true
	}
	return false
}
