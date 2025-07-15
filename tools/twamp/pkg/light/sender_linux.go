package twamplight

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type LinuxSender struct {
	cfg SenderConfig

	fd         int
	epfd       int
	seq        uint32
	remote     *unix.SockaddrInet4
	buf        []byte
	oob        []byte
	mu         sync.Mutex
	received   map[Packet]struct{}
	receivedMu sync.Mutex
	clockBias  *ClockBias
}

func NewLinuxSender(ctx context.Context, cfg SenderConfig) (*LinuxSender, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	if cfg.LocalInterface != "" {
		if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, cfg.LocalInterface); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("SO_BINDTODEVICE(%q): %w", cfg.LocalInterface, err)
		}
	}

	if cfg.LocalAddr != nil {
		ip4 := cfg.LocalAddr.IP.To4()
		if ip4 == nil {
			return nil, fmt.Errorf("local address must be IPv4")
		}
		sa := &unix.SockaddrInet4{Port: cfg.LocalAddr.Port}
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

	ip4 := cfg.RemoteAddr.IP.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("remote must be IPv4")
	}
	raddr := &unix.SockaddrInet4{Port: cfg.RemoteAddr.Port}
	copy(raddr.Addr[:], ip4)

	clockBias := NewClockBias(ctx, 100)

	s := &LinuxSender{
		cfg:       cfg,
		fd:        fd,
		epfd:      epfd,
		remote:    raddr,
		buf:       make([]byte, 1500),
		oob:       make([]byte, 512),
		received:  make(map[Packet]struct{}),
		clockBias: clockBias,
	}

	go s.cleanUpReceived(ctx)

	return s, nil
}

func (s *LinuxSender) Probe(ctx context.Context) (time.Duration, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if s.cfg.SchedulerPriority != nil {
		if err := SetRealtimePriority(*s.cfg.SchedulerPriority); err != nil {
			return 0, fmt.Errorf("set realtime priority: %w", err)
		}
	}

	if s.cfg.PinToCPU != nil {
		if err := PinCurrentThreadToCPU(*s.cfg.PinToCPU); err != nil {
			return 0, fmt.Errorf("pin current thread to cpu: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Increment sequence number.
	s.seq++

	// Create a packet and marshal it.
	packet := NewPacket(s.seq)
	err := packet.Marshal(s.buf)
	if err != nil {
		return 0, fmt.Errorf("marshal packet: %w", err)
	}

	// Send the packet.
	sendTime := time.Now().Add(s.clockBias.Get().WallclockToKernelSendLag) // estimate kernel send time
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

		// Validate packet size.
		if n != PacketSize {
			continue
		}

		// Validate packet format.
		packet, err = UnmarshalPacket(s.buf[:n])
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
				rtt := time.Unix(int64(ts.Sec), int64(ts.Nsec)).Sub(sendTime)

				// The send timestamp is captured in user space using CLOCK_REALTIME, while the receive
				// timestamp comes from the kernel via SO_TIMESTAMPNS. Due to clock sampling differences,
				// syscall latency, or NTP adjustments, the kernel timestamp can occasionally appear earlier
				// than the user-space send time. This results in a spurious negative RTT, which we
				// conservatively clamp to 0.
				rtt = max(rtt, 0)

				return rtt, nil
			}
		}
		return 0, fmt.Errorf("no timestamp in control message")
	}
}

func (s *LinuxSender) Close() error {
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

type ClockBias struct {
	WallclockToKernelSendLag time.Duration // kernel = wallclock + lag

	samples int
	mu      sync.RWMutex
}

func NewClockBias(ctx context.Context, samples int) *ClockBias {
	cb := &ClockBias{
		samples: samples,
	}
	_ = cb.Measure() // TODO(snormore): Handle error.
	go cb.measureLoop(ctx)
	return cb
}

func (c *ClockBias) Get() ClockBias {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ClockBias{
		WallclockToKernelSendLag: c.WallclockToKernelSendLag,
	}
}

func (c *ClockBias) measureLoop(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		err := c.Measure()
		if err != nil {
			// TODO(snormore): Log error instead of failing the loop.
			return fmt.Errorf("measure clock bias: %w", err)
		}
	}
}

func (c *ClockBias) Measure() error {
	laddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	rawConn.Control(func(fd uintptr) {
		controlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, 1)
	})
	if controlErr != nil {
		return controlErr
	}

	raddr := conn.LocalAddr().(*net.UDPAddr)
	var sendLags []time.Duration
	buf := []byte("ping")
	oob := make([]byte, 512)
	recvBuf := make([]byte, 512)

	for i := 0; i < c.samples; i++ {
		sendTime := time.Now()
		if _, err := conn.WriteToUDP(buf, raddr); err != nil {
			return err
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, oobn, _, _, err := conn.ReadMsgUDP(recvBuf, oob)
		if err != nil || n == 0 {
			continue
		}

		msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			if msg.Header.Level == syscall.SOL_SOCKET && msg.Header.Type == syscall.SO_TIMESTAMPNS {
				if len(msg.Data) < int(unsafe.Sizeof(syscall.Timespec{})) {
					continue
				}
				ts := *(*syscall.Timespec)(unsafe.Pointer(&msg.Data[0]))
				kernelTimestamp := time.Unix(int64(ts.Sec), int64(ts.Nsec))

				sendLags = append(sendLags, kernelTimestamp.Sub(sendTime)) // kernel = wallclock + lag
				break
			}
		}
	}

	if len(sendLags) == 0 {
		return fmt.Errorf("no samples collected")
	}

	slices.Sort(sendLags)

	c.mu.Lock()
	c.WallclockToKernelSendLag = sendLags[len(sendLags)/2]
	c.mu.Unlock()

	// fmt.Printf("WallclockToKernelSendLag: %v\n", c.WallclockToKernelSendLag)

	return nil
}
