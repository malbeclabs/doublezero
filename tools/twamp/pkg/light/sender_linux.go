package twamplight

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type LinuxSender struct {
	fd         int
	epfd       int
	seq        uint32
	remote     *unix.SockaddrInet4
	cancel     context.CancelFunc
	buf        []byte
	oob        []byte
	mu         sync.Mutex
	received   map[Packet]struct{}
	receivedMu sync.Mutex
}

func NewLinuxSender(ctx context.Context, iface string, local *net.UDPAddr, remote *net.UDPAddr) (*LinuxSender, error) {
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
		return nil, fmt.Errorf("remote must be IPv4")
	}
	raddr := &unix.SockaddrInet4{Port: remote.Port}
	copy(raddr.Addr[:], ip4)

	ctx, cancel := context.WithCancel(ctx)
	s := &LinuxSender{
		fd:       fd,
		epfd:     epfd,
		remote:   raddr,
		cancel:   cancel,
		buf:      make([]byte, 1500),
		oob:      make([]byte, 512),
		received: make(map[Packet]struct{}),
	}

	go s.cleanUpReceived(ctx)

	return s, nil
}

func (s *LinuxSender) Probe(ctx context.Context) (time.Duration, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Increment sequence number.
	s.seq++

	// Create a packet and marshal it.
	sentPacket := NewPacket(s.seq)
	err := sentPacket.Marshal(s.buf)
	if err != nil {
		return 0, fmt.Errorf("marshal packet: %w", err)
	}

	// Send the packet.
	sendTime := time.Now()
	if err := unix.Sendto(s.fd, s.buf[:PacketSize], 0, s.remote); err != nil {
		return 0, fmt.Errorf("sendto: %w", err)
	}

	// Use the context deadline if set, otherwise use a default timeout.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultProbeTimeout)
	}

	events := make([]unix.EpollEvent, 1)

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}

		// Wait for packets.
		n, err := unix.EpollWait(s.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return 0, fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			return 0, context.DeadlineExceeded
		}

		// Receive packet.
		n, oobn, _, _, err := unix.Recvmsg(s.fd, s.buf, s.oob, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				continue
			}
			return 0, fmt.Errorf("recvmsg: %w", err)
		}
		fallbackRecvTime := time.Now()

		// Validate packet size.
		if n != PacketSize {
			continue
		}

		// Validate packet format.
		packet, err := UnmarshalPacket(s.buf[:n])
		if err != nil {
			continue
		}

		// If we've already received this packet, ignore it.
		s.receivedMu.Lock()
		_, ok := s.received[*packet]
		s.receivedMu.Unlock()
		if ok {
			continue
		}

		// Add packet to received set.
		s.receivedMu.Lock()
		s.received[*packet] = struct{}{}
		s.receivedMu.Unlock()

		// Parse control message for timestamp.
		cmsgs, err := syscall.ParseSocketControlMessage(s.oob[:oobn])
		if err != nil {
			return 0, fmt.Errorf("parse cmsg: %w", err)
		}

		// Parse timestamp from control message.
		for _, cmsg := range cmsgs {
			if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
				if len(cmsg.Data) < int(unsafe.Sizeof(syscall.Timespec{})) {
					continue
				}
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				kernelRecvTime := time.Unix(int64(ts.Sec), int64(ts.Nsec))
				rtt := decideRTT(sendTime, kernelRecvTime, fallbackRecvTime)

				// Verify that the seq and timestamp match the sent packet.
				if sentPacket.Seq != packet.Seq {
					continue
				}
				if sentPacket.Sec != packet.Sec {
					continue
				}
				if sentPacket.Frac != packet.Frac {
					continue
				}

				return rtt, nil
			}
		}
		return 0, fmt.Errorf("no timestamp in control message")
	}
}

func (s *LinuxSender) Close() error {
	s.cancel()
	unix.Close(s.fd)
	unix.Close(s.epfd)
	return nil
}

func (s *LinuxSender) LocalAddr() *net.UDPAddr {
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

func (s *LinuxSender) cleanUpReceived(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.receivedMu.Lock()
			for p := range s.received {
				ts := time.Unix(int64(p.Sec), int64(p.Frac))
				if time.Since(ts) > 5*time.Minute {
					delete(s.received, p)
				}
			}
			s.receivedMu.Unlock()
		}
	}
}

// decideRTT chooses RTT from userspace send time, kernel recv timestamp, or userspace recv fallback.
// Rules:
//   - kernel RTT < -100µs: use userspace recv (interface misconfigured)
//   - -100µs ≤ kernel RTT < 0: clamp to 0 (clock drift/syscall latency)
//   - else: use kernel RTT
//
// Kernel timestamps via SO_TIMESTAMPNS can appear earlier than userspace CLOCK_REALTIME
// due to clock sampling, syscall latency, or NTP adjustments.
func decideRTT(sendTime, kernelRecvTime, fallbackRecvTime time.Time) time.Duration {
	rtt := kernelRecvTime.Sub(sendTime)
	if rtt < -100*time.Microsecond {
		rtt = fallbackRecvTime.Sub(sendTime)
	}
	rtt = max(rtt, 0)

	return rtt
}
