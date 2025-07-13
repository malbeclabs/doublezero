package twamplight

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type LinuxReflector struct {
	fd       int
	epfd     int
	port     uint16
	timeout  time.Duration
	shutdown chan struct{}
	closed   chan struct{}
}

func NewLinuxReflector(addr string, timeout time.Duration) (*LinuxReflector, error) {
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

	return &LinuxReflector{
		fd:       fd,
		epfd:     epfd,
		port:     uint16(udpAddr.Port),
		timeout:  timeout,
		shutdown: make(chan struct{}),
		closed:   make(chan struct{}),
	}, nil
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

		// Configure read deadline.
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(defaultReadTimeout)
		}
		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return context.DeadlineExceeded
		}

		// Wait for packets.
		n, err := unix.EpollWait(r.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			continue
		}

		for {
			// Receive packet.
			n, from, err := unix.Recvfrom(r.fd, buf, 0)
			if err != nil {
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					break
				}
				return fmt.Errorf("recvfrom: %w", err)
			}

			// Validate packet size.
			if n != PacketSize {
				continue
			}

			// Validate packet format.
			_, err = UnmarshalPacket(buf[:n])
			if err != nil {
				continue
			}

			// Send response.
			_ = unix.Sendto(r.fd, buf[:n], 0, from)
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

func (r *LinuxReflector) LocalAddr() *net.UDPAddr {
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

func zoneFromIndex(zoneId uint32) string {
	ifi, err := net.InterfaceByIndex(int(zoneId))
	if err != nil {
		return ""
	}
	return ifi.Name
}
