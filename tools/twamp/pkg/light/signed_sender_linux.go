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
	"unsafe"

	"golang.org/x/sys/unix"
)

type LinuxSignedSender struct {
	fd           int
	epfd         int
	seq          uint32
	remote       *unix.SockaddrInet4
	privateKey   ed25519.PrivateKey
	remotePubkey [32]byte
	cancel       context.CancelFunc
	buf          []byte
	oob          []byte
	mu           sync.Mutex
}

func NewLinuxSignedSender(ctx context.Context, iface string, local *net.UDPAddr, remote *net.UDPAddr, privateKey ed25519.PrivateKey, remotePubkey [32]byte) (*LinuxSignedSender, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	if iface != "" {
		if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("SO_BINDTODEVICE(%q): %w", iface, err)
		}
	}

	if local != nil {
		ip4 := local.IP.To4()
		if ip4 == nil {
			unix.Close(fd)
			return nil, fmt.Errorf("local address must be IPv4")
		}
		sa := &unix.SockaddrInet4{Port: local.Port}
		copy(sa.Addr[:], ip4)
		if err := unix.Bind(fd, sa); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("bind: %w", err)
		}
	}

	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("epoll_create1: %w", err)
	}

	event := &unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(fd)}
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

	ip4 := remote.IP.To4()
	if ip4 == nil {
		unix.Close(fd)
		unix.Close(epfd)
		return nil, fmt.Errorf("remote must be IPv4")
	}
	raddr := &unix.SockaddrInet4{Port: remote.Port}
	copy(raddr.Addr[:], ip4)

	_, cancel := context.WithCancel(ctx)
	s := &LinuxSignedSender{
		fd:           fd,
		epfd:         epfd,
		remote:       raddr,
		privateKey:   privateKey,
		remotePubkey: remotePubkey,
		cancel:       cancel,
		buf:          make([]byte, 1500),
		oob:          make([]byte, 512),
	}

	return s, nil
}

func (s *LinuxSignedSender) Probe(ctx context.Context) (time.Duration, *SignedReplyPacket, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++

	probe := NewSignedProbePacket(s.seq, s.privateKey)
	if err := probe.Marshal(s.buf); err != nil {
		return 0, nil, fmt.Errorf("marshal packet: %w", err)
	}

	sendTime := time.Now()
	if err := unix.Sendto(s.fd, s.buf[:SignedProbePacketSize], 0, s.remote); err != nil {
		return 0, nil, fmt.Errorf("sendto: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultProbeTimeout)
	}

	events := make([]unix.EpollEvent, 1)

	for {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		default:
		}

		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return 0, nil, context.DeadlineExceeded
		}

		n, err := unix.EpollWait(s.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return 0, nil, fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			return 0, nil, context.DeadlineExceeded
		}

		n, oobn, _, _, err := unix.Recvmsg(s.fd, s.buf, s.oob, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				continue
			}
			return 0, nil, fmt.Errorf("recvmsg: %w", err)
		}
		fallbackRecvTime := time.Now()

		if n != SignedReplyPacketSize {
			continue
		}

		reply, err := UnmarshalSignedReplyPacket(s.buf[:n])
		if err != nil {
			continue
		}

		if reply.Probe.Seq != probe.Seq || reply.Probe.Sec != probe.Sec || reply.Probe.Frac != probe.Frac {
			continue
		}
		if !VerifyProbe(&reply.Probe) {
			continue
		}
		if reply.ReflectorPubkey != s.remotePubkey {
			continue
		}
		if !VerifyReply(reply) {
			continue
		}

		cmsgs, err := syscall.ParseSocketControlMessage(s.oob[:oobn])
		if err != nil {
			return 0, nil, fmt.Errorf("parse cmsg: %w", err)
		}

		for _, cmsg := range cmsgs {
			if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
				if len(cmsg.Data) < int(unsafe.Sizeof(syscall.Timespec{})) {
					continue
				}
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				kernelRecvTime := time.Unix(int64(ts.Sec), int64(ts.Nsec))
				rtt := decideRTT(sendTime, kernelRecvTime, fallbackRecvTime)
				return rtt, reply, nil
			}
		}
		return 0, nil, fmt.Errorf("no timestamp in control message")
	}
}

func (s *LinuxSignedSender) Close() error {
	s.cancel()
	unix.Close(s.fd)
	unix.Close(s.epfd)
	return nil
}

func (s *LinuxSignedSender) LocalAddr() *net.UDPAddr {
	sa, err := unix.Getsockname(s.fd)
	if err != nil {
		return nil
	}
	switch addr := sa.(type) {
	case *unix.SockaddrInet4:
		return &net.UDPAddr{IP: net.IP(addr.Addr[:]), Port: addr.Port}
	default:
		return nil
	}
}
