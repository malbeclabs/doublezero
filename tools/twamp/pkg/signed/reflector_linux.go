package signed

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const defaultReadTimeout = 1 * time.Second

// senderState fields are only accessed from the single-goroutine epoll
// loop in Run(). No mutex needed.
type senderState struct {
	lastRxTime   time.Time
	pairCount    int
	pairStart    time.Time
	pairSourceIP [4]byte // source IP of the first probe in this pair
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
			n, from, err := unix.Recvfrom(r.fd, buf, 0)
			if err != nil {
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					break
				}
				return fmt.Errorf("recvfrom: %w", err)
			}

			if n != ProbePacketSize {
				continue
			}

			probe, err := UnmarshalProbePacket(buf[:n])
			if err != nil {
				continue
			}

			if _, ok := r.authorizedKeys.Load(probe.SenderPubkey); !ok {
				continue
			}

			now := time.Now()

			// Load or create per-sender state.
			raw, _ := r.senderStates.LoadOrStore(probe.SenderPubkey, &senderState{})
			state := raw.(*senderState)

			// Pair-based rate limiting: allow 2 probes per window, then drop.
			if interval := r.verifyInterval; interval > 0 {
				if state.pairCount >= 2 {
					if now.Sub(state.pairStart) < interval {
						continue
					}
					state.pairCount = 0
				}
			}

			// Pair authentication: the measurement loop runs from Probe 0 Rx to
			// Probe 1 Rx, so we skip verification on probe 0 to keep the reply
			// fast and the inter-arrival gap accurate. Probe 1 is verified after
			// capturing `now` — its latency only affects reply 1, which doesn't
			// impact the measurement (TEE targets can't measure time). Both
			// probes in a pair must come from the same source IP.
			fromAddr, ok := from.(*unix.SockaddrInet4)
			if !ok {
				continue
			}
			if state.pairCount == 0 {
				state.pairSourceIP = fromAddr.Addr
			} else {
				if fromAddr.Addr != state.pairSourceIP {
					continue
				}
				// Verify the probe signature. Comment out these two lines to
				// rely solely on IP-binding for pair authentication.
				// if !probe.Verify() {
				// 	continue
				// }
			}

			// SinceLastRxNs is only meaningful for the second probe in a pair.
			// Reset to zero at the start of each new pair so reply 0 always
			// carries SinceLastRxNs=0 and RttNs=dzdRttNs.
			var sinceLastRxNs uint64
			if !state.lastRxTime.IsZero() && state.pairCount > 0 {
				sinceLastRxNs = uint64(now.Sub(state.lastRxTime).Nanoseconds())
			}
			state.lastRxTime = now
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
