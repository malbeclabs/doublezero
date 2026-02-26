package twamplight

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type LinuxSignedReflector struct {
	fd             int
	epfd           int
	port           uint16
	timeout        time.Duration
	privateKey     ed25519.PrivateKey
	authorizedKeys sync.Map // map[[32]byte]struct{}
	lastVerify     sync.Map // map[[32]byte]time.Time
	shutdown       chan struct{}
	closed         chan struct{}
}

func NewLinuxSignedReflector(addr string, timeout time.Duration, privateKey ed25519.PrivateKey, authorizedKeys [][32]byte) (*LinuxSignedReflector, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve addr: %w", err)
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	sockaddr := &unix.SockaddrInet4{Port: udpAddr.Port}
	copy(sockaddr.Addr[:], udpAddr.IP.To4())
	if err := unix.Bind(fd, sockaddr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}

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

	r := &LinuxSignedReflector{
		fd:         fd,
		epfd:       epfd,
		port:       uint16(udpAddr.Port),
		timeout:    timeout,
		privateKey: privateKey,
		shutdown:   make(chan struct{}),
		closed:     make(chan struct{}),
	}

	for _, key := range authorizedKeys {
		r.authorizedKeys.Store(key, struct{}{})
	}

	return r, nil
}

func (r *LinuxSignedReflector) SetAuthorizedKeys(keys [][32]byte) {
	// Build new set.
	newKeys := make(map[[32]byte]struct{}, len(keys))
	for _, key := range keys {
		newKeys[key] = struct{}{}
	}

	// Remove keys no longer authorized.
	r.authorizedKeys.Range(func(key, _ any) bool {
		k := key.([32]byte)
		if _, ok := newKeys[k]; !ok {
			r.authorizedKeys.Delete(k)
		}
		return true
	})

	// Add new keys.
	for _, key := range keys {
		r.authorizedKeys.Store(key, struct{}{})
	}
}

func (r *LinuxSignedReflector) Run(ctx context.Context) error {
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

			if n != SignedProbePacketSize {
				continue
			}

			probe, err := UnmarshalSignedProbePacket(buf[:n])
			if err != nil {
				continue
			}

			// Check pubkey against allowlist.
			if _, ok := r.authorizedKeys.Load(probe.SenderPubkey); !ok {
				continue
			}

			// Rate-limit signature verifications per pubkey to bound CPU
			// cost from attackers replaying a known authorized pubkey.
			now := time.Now()
			if interval := SignedReflectorVerifyInterval; interval > 0 {
				if last, ok := r.lastVerify.Load(probe.SenderPubkey); ok {
					if now.Sub(last.(time.Time)) < interval {
						continue
					}
				}
			}
			r.lastVerify.Store(probe.SenderPubkey, now)

			// Verify probe signature.
			if !VerifyProbe(probe) {
				continue
			}

			// Construct signed reply.
			reply := NewSignedReplyPacket(probe, r.privateKey)
			var replyBuf [SignedReplyPacketSize]byte
			_ = reply.Marshal(replyBuf[:])

			_ = unix.Sendto(r.fd, replyBuf[:], 0, from)
		}
	}
}

func (r *LinuxSignedReflector) Close() error {
	select {
	case <-r.closed:
		return nil
	default:
		close(r.shutdown)
		<-r.closed
		return nil
	}
}

func (r *LinuxSignedReflector) LocalAddr() *net.UDPAddr {
	sa, err := unix.Getsockname(r.fd)
	if err != nil {
		return nil
	}

	switch addr := sa.(type) {
	case *unix.SockaddrInet4:
		ip := net.IP(addr.Addr[:])
		return &net.UDPAddr{IP: ip, Port: addr.Port}
	case *unix.SockaddrInet6:
		ip := net.IP(addr.Addr[:])
		return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: zoneFromIndex(addr.ZoneId)}
	default:
		return nil
	}
}
