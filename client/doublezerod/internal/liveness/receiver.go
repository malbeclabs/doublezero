package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Receiver is a long-lived goroutine that continuously reads UDP control packets
// from the shared transport socket and passes valid ones to a handler.
//
// It abstracts read-loop robustness: manages deadlines, throttles noisy logs,
// detects fatal network conditions, and honors context cancellation cleanly.
type Receiver struct {
	log      *slog.Logger // structured logger for debug and warnings
	udp      UDPService   // underlying socket with control message support
	handleRx HandleRxFunc // callback invoked for each valid ControlPacket

	readErrWarnEvery time.Duration // min interval between repeated read warnings
	readErrWarnLast  time.Time     // last time a warning was logged
	readErrWarnMu    sync.Mutex    // guards readErrWarnLast

	metrics *Metrics
}

// HandleRxFunc defines the handler signature for received control packets.
// The callback is invoked for every successfully decoded ControlPacket,
// along with a Peer descriptor identifying interface and IP context.
type HandleRxFunc func(pkt *ControlPacket, peer Peer)

// NewReceiver constructs a new Receiver bound to the given UDPService and handler.
// By default, it throttles repeated read errors to once every 5 seconds.
func NewReceiver(log *slog.Logger, udp UDPService, handleRx HandleRxFunc, metrics *Metrics) *Receiver {
	return &Receiver{
		log:              log,
		udp:              udp,
		handleRx:         handleRx,
		readErrWarnEvery: 5 * time.Second,
		metrics:          metrics,
	}
}

// Run executes the main receive loop until ctx is canceled or the socket fails.
// It continually reads packets, unmarshals them into ControlPackets, and passes
// them to handleRx. Errors are rate-limited and fatal errors terminate the loop.
func (r *Receiver) Run(ctx context.Context) error {
	r.log.Debug("liveness.recv: rx loop started")
	buf := make([]byte, 1500) // typical MTU-sized buffer

	for {
		// Early exit if caller canceled context.
		select {
		case <-ctx.Done():
			r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
			return nil
		default:
		}

		// Periodically set a read deadline to make the loop interruptible.
		if err := r.udp.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			// Respect cancellation immediately if already stopped.
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				r.log.Debug("liveness.recv: socket closed during SetReadDeadline; exiting")
				return fmt.Errorf("socket closed during SetReadDeadline: %w", err)
			}

			// Log throttled warnings for transient errors (e.g., bad FD state).
			now := time.Now()
			r.readErrWarnMu.Lock()
			if r.readErrWarnLast.IsZero() || now.Sub(r.readErrWarnLast) >= r.readErrWarnEvery {
				r.readErrWarnLast = now
				r.readErrWarnMu.Unlock()
				r.log.Warn("liveness.recv: SetReadDeadline error", "error", err)
			} else {
				r.readErrWarnMu.Unlock()
			}

			// Exit for fatal kernel or network-level errors.
			if isFatalNetErr(err) {
				return fmt.Errorf("fatal network error during SetReadDeadline: %w", err)
			}

			// Brief delay prevents a tight loop in persistent error states.
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Perform the actual UDP read with control message extraction.
		n, peerAddr, localIP, ifname, err := r.udp.ReadFrom(buf)
		if err != nil {
			// Stop cleanly on context cancellation.
			select {
			case <-ctx.Done():
				r.log.Debug("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
				return nil
			default:
			}

			// Deadline timeout: simply continue polling.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}

			// Closed socket: terminate immediately.
			if errors.Is(err, net.ErrClosed) {
				r.log.Debug("liveness.recv: socket closed; exiting")
				return fmt.Errorf("socket closed during ReadFrom: %w", err)
			}

			r.metrics.ReadSocketErrors.WithLabelValues(ifname, localIP.String()).Inc()

			// Log other transient read errors, throttled.
			now := time.Now()
			r.readErrWarnMu.Lock()
			if r.readErrWarnLast.IsZero() || now.Sub(r.readErrWarnLast) >= r.readErrWarnEvery {
				r.readErrWarnLast = now
				r.readErrWarnMu.Unlock()
				r.log.Warn("liveness.recv: non-timeout read error", "error", err)
			} else {
				r.readErrWarnMu.Unlock()
			}

			if isFatalNetErr(err) {
				return fmt.Errorf("fatal network error during ReadFrom: %w", err)
			}
			continue
		}
		start := time.Now()

		// Attempt to parse the received packet into a ControlPacket struct.
		ctrl, err := UnmarshalControlPacket(buf[:n])
		if err != nil {
			r.log.Error("liveness.recv: error parsing control packet", "error", err)

			lip := localIP.To4().String()
			switch {
			case errors.Is(err, ErrShortPacket):
				r.metrics.ControlPacketsRxInvalid.WithLabelValues(ifname, lip, "short").Inc()
			case errors.Is(err, ErrInvalidLength):
				r.metrics.ControlPacketsRxInvalid.WithLabelValues(ifname, lip, "bad_len").Inc()
			default:
				if strings.Contains(err.Error(), "unsupported version") {
					r.metrics.ControlPacketsRxInvalid.WithLabelValues(ifname, lip, "bad_version").Inc()
				} else {
					r.metrics.ControlPacketsRxInvalid.WithLabelValues(ifname, lip, "parse_error").Inc()
				}
			}

			continue
		}

		// Skip packets that are not IPv4.
		if localIP.To4() == nil || peerAddr.IP.To4() == nil {
			if localIP.To4() != nil {
				r.metrics.ControlPacketsRxInvalid.WithLabelValues(ifname, localIP.To4().String(), "not_ipv4").Inc()
			}
			continue
		}
		localIP4 := localIP.To4().String()

		r.metrics.ControlPacketsRX.WithLabelValues(ifname, localIP4).Inc()

		// Populate the peer descriptor: identifies which local interface/IP
		// the packet arrived on and the remote endpoint that sent it.
		peer := Peer{
			Interface: ifname,
			LocalIP:   localIP4,
			PeerIP:    peerAddr.IP.To4().String(),
		}

		// Log the control packet at DEBUG level.
		r.log.Debug("liveness.recv: parsed control packet",
			"iface", peer.Interface,
			"localIP", peer.LocalIP,
			"peerIP", peer.PeerIP,
			"state", ctrl.State.String(),
			"localDiscr", ctrl.LocalDiscr,
			"peerDiscr", ctrl.PeerDiscr,
			"detectMult", ctrl.DetectMult,
			"desiredMinTxUs", ctrl.DesiredMinTxUs,
			"requiredMinRxUs", ctrl.RequiredMinRxUs,
		)

		// Delegate to session or higher-level handler for processing.
		r.handleRx(ctrl, peer)
		r.metrics.HandleRxDuration.WithLabelValues(peer.Interface, peer.LocalIP).Observe(time.Since(start).Seconds())
	}
}

// isFatalNetErr determines whether a network-related error is non-recoverable.
// It checks for known fatal errno codes and unwraps platform-specific net errors.
func isFatalNetErr(err error) bool {
	// Closed socket explicitly fatal.
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	// Inspect underlying syscall errno for hardware or interface removal.
	var se syscall.Errno
	if errors.As(err, &se) {
		switch se {
		case syscall.EBADF, syscall.ENETDOWN, syscall.ENODEV, syscall.ENXIO:
			return true
		}
	}

	// On some systems, fatal syscall errors are wrapped in *net.OpError.
	var oe *net.OpError
	if errors.As(err, &oe) && !oe.Timeout() && !oe.Temporary() {
		return true
	}
	return false
}
