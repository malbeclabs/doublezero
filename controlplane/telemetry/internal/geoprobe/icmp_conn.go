package geoprobe

import (
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type icmpConn struct {
	fd          int
	epfd        int
	oob         []byte
	events      []unix.EpollEvent
	deadline    time.Time
	hasKernelTS bool
}

func newICMPConn(log *slog.Logger) (*icmpConn, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW|unix.SOCK_NONBLOCK, unix.IPPROTO_ICMP)
	if err != nil {
		return nil, fmt.Errorf("raw ICMP socket: %w", err)
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

	hasKernelTS := true
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1); err != nil {
		log.Warn("SO_TIMESTAMPNS unavailable, using userspace timestamps", "error", err)
		hasKernelTS = false
	}

	return &icmpConn{
		fd:          fd,
		epfd:        epfd,
		oob:         make([]byte, 512),
		events:      make([]unix.EpollEvent, 1),
		hasKernelTS: hasKernelTS,
	}, nil
}

func (c *icmpConn) sendEcho(dst net.IP, payload []byte) (time.Time, error) {
	ip4 := dst.To4()
	if ip4 == nil {
		return time.Time{}, fmt.Errorf("not an IPv4 address: %v", dst)
	}
	sa := &unix.SockaddrInet4{}
	copy(sa.Addr[:], ip4)

	txTime := time.Now()
	if err := unix.Sendto(c.fd, payload, 0, sa); err != nil {
		return time.Time{}, fmt.Errorf("sendto: %w", err)
	}
	return txTime, nil
}

func (c *icmpConn) recvEcho(buf []byte) (int, time.Time, error) {
	remaining := int(time.Until(c.deadline).Milliseconds())
	if remaining <= 0 {
		return 0, time.Time{}, syscall.ETIMEDOUT
	}

	n, err := unix.EpollWait(c.epfd, c.events, remaining)
	if err != nil && err != syscall.EINTR {
		return 0, time.Time{}, fmt.Errorf("epoll_wait: %w", err)
	}
	if n == 0 {
		return 0, time.Time{}, syscall.ETIMEDOUT
	}

	var oob []byte
	if c.hasKernelTS {
		oob = c.oob
	}

	msgN, oobn, _, _, err := unix.Recvmsg(c.fd, buf, oob, 0)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0, time.Time{}, syscall.EAGAIN
		}
		return 0, time.Time{}, err
	}
	fallbackTime := time.Now()

	// Raw ICMP sockets include the IPv4 header; strip it so callers
	// see only the ICMP payload (matching icmp.PacketConn.ReadFrom behavior).
	hdrLen := stripIPv4Header(buf[:msgN])
	icmpLen := msgN - hdrLen
	copy(buf, buf[hdrLen:msgN])

	if !c.hasKernelTS {
		return icmpLen, fallbackTime, nil
	}

	rxTime := parseKernelTimestamp(c.oob[:oobn], fallbackTime)
	return icmpLen, rxTime, nil
}

func parseKernelTimestamp(oob []byte, fallback time.Time) time.Time {
	cmsgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return fallback
	}
	for _, cmsg := range cmsgs {
		if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
			if len(cmsg.Data) >= int(unsafe.Sizeof(syscall.Timespec{})) {
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				kernel := time.Unix(int64(ts.Sec), int64(ts.Nsec))
				return decideRxTimestamp(kernel, fallback)
			}
		}
	}
	return fallback
}

// decideRxTimestamp picks the best receive timestamp. If the kernel timestamp
// is >10ms behind userspace, the kernel clock is likely misconfigured and we
// fall back. Scheduler preemption can add 200-500µs, so small deltas are normal.
func decideRxTimestamp(kernel, fallback time.Time) time.Time {
	if fallback.Sub(kernel) > 10*time.Millisecond {
		return fallback
	}
	return kernel
}

// stripIPv4Header returns the length of the IPv4 header in buf.
func stripIPv4Header(buf []byte) int {
	if len(buf) < 1 {
		return 0
	}
	ihl := int(buf[0]&0x0f) * 4
	if ihl < 20 || ihl > len(buf) {
		return 0
	}
	return ihl
}

func (c *icmpConn) setReadDeadline(t time.Time) error {
	c.deadline = t
	return nil
}

func (c *icmpConn) close() error {
	unix.Close(c.epfd)
	return unix.Close(c.fd)
}
