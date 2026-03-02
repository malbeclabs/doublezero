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

type LinuxReflector struct {
	fd             int
	epfd           int
	port           uint16
	timeout        time.Duration
	signer         Signer
	authorizedKeys sync.Map // map[[32]byte]struct{}
	lastVerify     sync.Map // map[[32]byte]time.Time
	shutdown       chan struct{}
	closed         chan struct{}
}

// NewLinuxReflector creates a signed TWAMP reflector. Only the port in addr is used; any IP is ignored.
func NewLinuxReflector(addr string, timeout time.Duration, signer Signer, authorizedKeys [][32]byte) (*LinuxReflector, error) {
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
		fd:       fd,
		epfd:     epfd,
		port:     boundPort,
		timeout:  timeout,
		signer:   signer,
		shutdown: make(chan struct{}),
		closed:   make(chan struct{}),
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
		}
		return true
	})

	for _, key := range keys {
		r.authorizedKeys.Store(key, struct{}{})
	}
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
			if interval := VerifyInterval; interval > 0 {
				if last, ok := r.lastVerify.Load(probe.SenderPubkey); ok {
					if now.Sub(last.(time.Time)) < interval {
						continue
					}
				}
			}
			r.lastVerify.Store(probe.SenderPubkey, now)

			if !probe.Verify() {
				continue
			}

			reply := NewReplyPacket(probe, r.signer)
			var replyBuf [ReplyPacketSize]byte
			_ = reply.Marshal(replyBuf[:])

			_ = unix.Sendto(r.fd, replyBuf[:], 0, from)
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
