package signed

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	defaultReadTimeout = 1 * time.Second
	stalePairTimeout   = 5 * time.Second
)

// senderState fields are only accessed from the single-goroutine epoll
// loop in Run(). No mutex needed.
type senderState struct {
	lastTxTime   time.Time // captured just before Sendto of reply N-1
	pairCount    int
	pairStart    time.Time
	pairSourceIP [4]byte
}

type LinuxReflector struct {
	fd             int
	epfd           int
	port           uint16
	timeout        time.Duration
	verifyInterval time.Duration
	signer         Signer
	geoprobePubkey [32]byte
	authorizedKeys sync.Map // map[[32]byte]struct{}
	senderStates   sync.Map // map[[32]byte]*senderState
	offsetsMu      sync.RWMutex
	offsets        [][]byte
	shutdown       chan struct{}
	closed         chan struct{}
	logger         *slog.Logger
}

// NewLinuxReflector creates a signed TWAMP reflector. Only the port in addr is used; any IP is ignored.
func NewLinuxReflector(addr string, timeout time.Duration, signer Signer, geoprobePubkey [32]byte, authorizedKeys [][32]byte, verifyInterval time.Duration) (*LinuxReflector, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve addr: %w", err)
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	sockaddr := &unix.SockaddrInet4{Port: udpAddr.Port}
	if err := unix.Bind(fd, sockaddr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}

	sa, err := unix.Getsockname(fd)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("getsockname: %w", err)
	}
	boundPort := uint16(sa.(*unix.SockaddrInet4).Port)

	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("epoll_create1: %w", err)
	}

	event := &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(fd),
	}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, fd, event); err != nil {
		unix.Close(fd)
		unix.Close(epfd)
		return nil, fmt.Errorf("epoll_ctl: %w", err)
	}

	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1); err != nil {
		unix.Close(fd)
		unix.Close(epfd)
		return nil, fmt.Errorf("SO_TIMESTAMPNS: %w", err)
	}

	r := &LinuxReflector{
		fd:             fd,
		epfd:           epfd,
		port:           boundPort,
		timeout:        timeout,
		verifyInterval: verifyInterval,
		signer:         signer,
		geoprobePubkey: geoprobePubkey,
		shutdown:       make(chan struct{}),
		closed:         make(chan struct{}),
	}

	for _, key := range authorizedKeys {
		r.authorizedKeys.Store(key, struct{}{})
	}

	return r, nil
}

func (r *LinuxReflector) SetAuthorizedKeys(keys [][32]byte) {
	newKeys := make(map[[32]byte]struct{}, len(keys))
	for _, key := range keys {
		newKeys[key] = struct{}{}
	}

	r.authorizedKeys.Range(func(key, _ any) bool {
		k := key.([32]byte)
		if _, ok := newKeys[k]; !ok {
			r.authorizedKeys.Delete(k)
			r.senderStates.Delete(k)
		}
		return true
	})

	for _, key := range keys {
		r.authorizedKeys.Store(key, struct{}{})
	}
}

func (r *LinuxReflector) SetOffsets(offsets [][]byte) {
	cp := make([][]byte, len(offsets))
	for i, blob := range offsets {
		cp[i] = make([]byte, len(blob))
		copy(cp[i], blob)
	}
	r.offsetsMu.Lock()
	r.offsets = cp
	r.offsetsMu.Unlock()
}

func (r *LinuxReflector) SetLogger(logger *slog.Logger) {
	r.logger = logger
}

func (r *LinuxReflector) Port() uint16 {
	return r.port
}

func (r *LinuxReflector) Run(ctx context.Context) error {
	runtime.LockOSThread()
	defer close(r.closed)
	defer unix.Close(r.fd)
	defer unix.Close(r.epfd)

	events := make([]unix.EpollEvent, 1)
	buf := make([]byte, 1500)
	oob := make([]byte, 512)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.shutdown:
			return nil
		default:
		}

		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(defaultReadTimeout)
		}
		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return context.DeadlineExceeded
		}

		n, err := unix.EpollWait(r.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			continue
		}

		for {
			n, oobn, _, from, err := unix.Recvmsg(r.fd, buf, oob, 0)
			if err != nil {
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					break
				}
				return fmt.Errorf("recvmsg: %w", err)
			}

			if n != ProbePacketSize {
				continue
			}

			probe, err := UnmarshalProbePacket(buf[:n])
			if err != nil {
				continue
			}

			if _, ok := r.authorizedKeys.Load(probe.SenderPubkey); !ok {
				if r.logger != nil {
					r.logger.Debug("dropping probe from unauthorized pubkey", "sender_pubkey", fmt.Sprintf("%x", probe.SenderPubkey))
				}
				continue
			}

			now := kernelTimestamp(oob[:oobn])

			// Load or create per-sender state.
			raw, _ := r.senderStates.LoadOrStore(probe.SenderPubkey, &senderState{})
			state := raw.(*senderState)

			// Pair-based rate limiting: allow 2 probes per window, then drop.
			if interval := r.verifyInterval; interval > 0 {
				if state.pairCount >= 2 {
					if now.Sub(state.pairStart) < interval {
						if r.logger != nil {
							r.logger.Debug("dropping rate-limited probe", "sender_pubkey", fmt.Sprintf("%x", probe.SenderPubkey))
						}
						continue
					}
					state.pairCount = 0
				}
			}

			// If probe 1 never arrived, reset so the next probe starts a fresh pair.
			if state.pairCount == 1 && !state.lastTxTime.IsZero() && now.Sub(state.lastTxTime) > stalePairTimeout {
				state.pairCount = 0
			}

			// Pair integrity: both probes must come from the same source IP.
			// The pubkey allowlist (checked above) provides authentication;
			// per-probe signature verification is left to the target.
			fromAddr, ok := from.(*unix.SockaddrInet4)
			if !ok {
				continue
			}
			if state.pairCount == 0 {
				state.pairSourceIP = fromAddr.Addr
			} else if fromAddr.Addr != state.pairSourceIP {
				if r.logger != nil {
					r.logger.Debug("dropping probe with mismatched source IP",
						"sender_pubkey", fmt.Sprintf("%x", probe.SenderPubkey),
						"expected", net.IP(state.pairSourceIP[:]).String(),
						"actual", net.IP(fromAddr.Addr[:]).String())
				}
				continue
			}

			// SinceLastRxNs: Tx-to-Rx interval (reply 0 Tx → probe 1 Rx).
			// Zero for the first probe in a pair.
			var sinceLastRxNs uint64
			if !state.lastTxTime.IsZero() && state.pairCount > 0 {
				sinceLastRxNs = uint64(now.Sub(state.lastTxTime).Nanoseconds())
			}
			if state.pairCount == 0 {
				state.pairStart = now
			}
			state.pairCount++

			r.offsetsMu.RLock()
			currentOffsets := r.offsets
			r.offsetsMu.RUnlock()

			// Derive location data from the first reference offset.
			var slot uint64
			var lat, lng float64
			var dzdRttNs uint64
			if len(currentOffsets) > 0 {
				if info, ok := ParseOffsetInfo(currentOffsets[0]); ok {
					slot = info.MeasurementSlot
					lat = info.Lat
					lng = info.Lng
					dzdRttNs = info.RttNs
				}
			}

			reply, err := NewReplyPacket(probe, r.signer, r.geoprobePubkey, currentOffsets, slot, lat, lng, sinceLastRxNs, dzdRttNs+sinceLastRxNs)
			if err != nil {
				continue
			}
			var replyBuf [MaxReplyPacketSize]byte
			replyLen, _ := reply.Marshal(replyBuf[:])

			state.lastTxTime = time.Now()
			_ = unix.Sendto(r.fd, replyBuf[:replyLen], 0, from)
		}
	}
}

func (r *LinuxReflector) Close() error {
	select {
	case <-r.closed:
		return nil
	default:
		close(r.shutdown)
		<-r.closed
		return nil
	}
}

// kernelTimestamp extracts the SO_TIMESTAMPNS kernel receive timestamp from
// the ancillary data returned by Recvmsg. Falls back to time.Now() if the
// timestamp is missing or unparseable.
func kernelTimestamp(oob []byte) time.Time {
	cmsgs, err := syscall.ParseSocketControlMessage(oob)
	if err == nil {
		for _, cmsg := range cmsgs {
			if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
				if len(cmsg.Data) >= int(unsafe.Sizeof(syscall.Timespec{})) {
					ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
					return time.Unix(int64(ts.Sec), int64(ts.Nsec))
				}
			}
		}
	}
	return time.Now()
}
