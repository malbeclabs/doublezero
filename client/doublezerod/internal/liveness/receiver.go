package liveness

import (
	"context"
	"errors"
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

	// throttled warning for noisy read errors
	readErrEvery time.Duration
	lastReadWarn time.Time
	mu           sync.Mutex

	// fatal socket error reporting
	fatalCh chan<- error
}

type HandleRxFunc func(pkt *ControlPacket, peer Peer)

func NewReceiver(log *slog.Logger, conn *UDPConn, handleRx HandleRxFunc, fatalCh chan<- error) *Receiver {
	return &Receiver{
		log:          log,
		conn:         conn,
		handleRx:     handleRx,
		readErrEvery: 5 * time.Second,
		fatalCh:      fatalCh,
	}
}

func (r *Receiver) Run(ctx context.Context) {
	r.log.Debug("liveness.recv: rx loop started")

	buf := make([]byte, 1500)
	for {
		_ = r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remoteAddr, localIP, ifname, err := r.conn.ReadFrom(buf)
		if err != nil {
			// timeout: check for stop
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
					return
				default:
					continue
				}
			}
			// context cancelled
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return
			default:
				// throttle non-timeout read errors
				now := time.Now()
				r.mu.Lock()
				if r.lastReadWarn.IsZero() || now.Sub(r.lastReadWarn) >= r.readErrEvery {
					r.lastReadWarn = now
					r.mu.Unlock()
					r.log.Warn("liveness.recv: non-timeout read error", "error", err)
				} else {
					r.mu.Unlock()
				}
				// if fatal, notify supervisor and exit to avoid hot loop
				if isFatalNetErr(err) {
					select {
					case r.fatalCh <- err:
					default:
					}
					// brief pause to avoid immediate tight spin in caller
					time.Sleep(100 * time.Millisecond)
					return
				}
				continue
			}
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
	// closed socket
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// syscall-level fatal hints
	var se syscall.Errno
	if errors.As(err, &se) {
		switch se {
		case syscall.EBADF, syscall.ENETDOWN, syscall.ENODEV, syscall.ENXIO:
			return true
		}
	}
	// some platforms wrap the above in *net.OpError; treat non-temporary, non-timeout as fatal
	var oe *net.OpError
	if errors.As(err, &oe) && !oe.Timeout() && !oe.Temporary() {
		return true
	}
	return false
}
