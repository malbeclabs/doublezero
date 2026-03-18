package signed

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const defaultProbeTimeout = 1 * time.Second

// tosDSCPCS5 is the IP TOS byte value for DSCP CS5 (Traffic Class 5).
// DSCP CS5 = 40 (0b101000), shifted left 2 bits into the TOS byte = 0xA0.
const tosDSCPCS5 = 0xA0

// busyPollWindow keeps the thread warm via EpollWait(timeout=0) so that
// scheduler wakeup latency doesn't dominate short-RTT measurements.
const busyPollWindow = 15 * time.Millisecond

type LinuxSender struct {
	fd           int
	epfd         int
	seq          uint32
	remote       *unix.SockaddrInet4
	signer       Signer
	remotePubkey [32]byte
	cancel       context.CancelFunc
	buf          []byte
	oob          []byte
	mu           sync.Mutex
}

// NewLinuxSender creates a signed TWAMP sender. Only local.Port is used; any IP is ignored.
func NewLinuxSender(ctx context.Context, iface string, local *net.UDPAddr, remote *net.UDPAddr, signer Signer, remotePubkey [32]byte) (*LinuxSender, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, unix.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	// Mark probes as TC5 (DSCP CS5 = 40, TOS byte = 0xA0).
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TOS, tosDSCPCS5); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("IP_TOS: %w", err)
	}

	if iface != "" {
		if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("SO_BINDTODEVICE(%q): %w", iface, err)
		}
	}

	if local != nil {
		sa := &unix.SockaddrInet4{Port: local.Port}
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
	s := &LinuxSender{
		fd:           fd,
		epfd:         epfd,
		remote:       raddr,
		signer:       signer,
		remotePubkey: remotePubkey,
		cancel:       cancel,
		buf:          make([]byte, 1500),
		oob:          make([]byte, 512),
	}

	return s, nil
}

func (s *LinuxSender) Probe(ctx context.Context) (time.Duration, *ReplyPacket, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	probe := NewProbePacket(s.seq, s.signer)
	var buf [ProbePacketSize]byte
	if err := probe.Marshal(buf[:]); err != nil {
		return 0, nil, fmt.Errorf("marshal probe: %w", err)
	}

	return s.sendAndRecv(ctx, buf[:], probe, true, 0)
}

// ProbePair sends two probes in quick succession. Both probe packets are
// pre-created and pre-signed before any network I/O. When reply 0 arrives,
// probe 1 is fired immediately after a 4-byte seq check — before any
// parsing — to minimize the target turnaround that inflates the reflector's
// Tx-to-Rx measurement.
func (s *LinuxSender) ProbePair(ctx context.Context) (ProbePairResult, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Pre-sign both probes before any network I/O.
	s.seq++
	probe0 := NewProbePacket(s.seq, s.signer)
	var buf0 [ProbePacketSize]byte
	if err := probe0.Marshal(buf0[:]); err != nil {
		return ProbePairResult{}, fmt.Errorf("marshal probe 0: %w", err)
	}

	s.seq++
	probe1 := NewProbePacket(s.seq, s.signer)
	var buf1 [ProbePacketSize]byte
	if err := probe1.Marshal(buf1[:]); err != nil {
		return ProbePairResult{}, fmt.Errorf("marshal probe 1: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultProbeTimeout)
	}

	// --- Send probe 0 and wait for reply 0 ---
	send0Time := time.Now()
	if err := unix.Sendto(s.fd, buf0[:], 0, s.remote); err != nil {
		return ProbePairResult{}, fmt.Errorf("sendto probe 0: %w", err)
	}

	events := make([]unix.EpollEvent, 1)
	var (
		reply0Len    int
		reply0CtlLen int
		reply0Ctl    [512]byte // stack copy so Recvmsg for reply 1 doesn't overwrite
		probe1Sent   bool
		send1Time    time.Time
	)

	// recvAndFireProbe1 does Recvmsg, checks size+seq, and immediately fires
	// probe 1 if the packet looks like reply 0. Returns true if a packet was
	// consumed (caller should stop polling), false on EAGAIN/mismatch.
	recvAndFireProbe1 := func() (bool, error) {
		n, oobn, _, _, err := unix.Recvmsg(s.fd, s.buf, s.oob, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return false, nil
			}
			return false, fmt.Errorf("recvmsg: %w", err)
		}
		if n < MinReplyPacketSize || n > MaxReplyPacketSize {
			return false, nil
		}
		if binary.BigEndian.Uint32(s.buf[0:4]) != probe0.Seq {
			return false, nil
		}

		// Seq matches — fire probe 1 immediately before any parsing.
		reply0Len = n
		reply0CtlLen = oobn
		copy(reply0Ctl[:oobn], s.oob[:oobn])
		send1Time = time.Now()
		if err := unix.Sendto(s.fd, buf1[:], 0, s.remote); err != nil {
			return false, fmt.Errorf("sendto probe 1: %w", err)
		}
		probe1Sent = true
		return true, nil
	}

	// Busy-poll phase for reply 0.
	busyPollDeadline := send0Time.Add(busyPollWindow)
	for time.Now().Before(busyPollDeadline) {
		n, err := unix.EpollWait(s.epfd, events, 0)
		if err != nil && err != syscall.EINTR {
			return ProbePairResult{}, fmt.Errorf("epoll_wait: %w", err)
		}
		if n > 0 {
			done, err := recvAndFireProbe1()
			if err != nil {
				return ProbePairResult{}, fmt.Errorf("probe 0: %w", err)
			}
			if done {
				break
			}
		}
	}

	// Blocking phase for reply 0 (if busy-poll didn't catch it).
	for !probe1Sent {
		select {
		case <-ctx.Done():
			return ProbePairResult{}, ctx.Err()
		default:
		}
		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return ProbePairResult{}, context.DeadlineExceeded
		}
		n, err := unix.EpollWait(s.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return ProbePairResult{}, fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			return ProbePairResult{}, context.DeadlineExceeded
		}
		done, err := recvAndFireProbe1()
		if err != nil {
			return ProbePairResult{}, fmt.Errorf("probe 0: %w", err)
		}
		_ = done
	}

	// --- Parse reply 0 (deferred until after probe 1 was sent) ---
	fallback0Time := time.Now()
	reply0, err := UnmarshalReplyPacket(s.buf[:reply0Len])
	if err != nil {
		return ProbePairResult{}, fmt.Errorf("unmarshal reply 0: %w", err)
	}
	if reply0.Probe.Sec != probe0.Sec || reply0.Probe.Frac != probe0.Frac {
		return ProbePairResult{}, fmt.Errorf("reply 0: timestamp mismatch")
	}

	cmsgs0, err := syscall.ParseSocketControlMessage(reply0Ctl[:reply0CtlLen])
	if err != nil {
		return ProbePairResult{}, fmt.Errorf("reply 0: parse cmsg: %w", err)
	}
	var rtt0 time.Duration
	for _, cmsg := range cmsgs0 {
		if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
			if len(cmsg.Data) >= int(unsafe.Sizeof(syscall.Timespec{})) {
				ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
				kernelRecvTime := time.Unix(int64(ts.Sec), int64(ts.Nsec))
				rtt0 = decideRTT(send0Time, kernelRecvTime, fallback0Time)
				break
			}
		}
	}

	// --- Wait for reply 1 (already sent, just receive) ---
	for {
		select {
		case <-ctx.Done():
			return ProbePairResult{}, ctx.Err()
		default:
		}
		remaining := int(time.Until(deadline).Milliseconds())
		if remaining <= 0 {
			return ProbePairResult{}, context.DeadlineExceeded
		}
		n, err := unix.EpollWait(s.epfd, events, remaining)
		if err != nil && err != syscall.EINTR {
			return ProbePairResult{}, fmt.Errorf("epoll_wait: %w", err)
		}
		if n == 0 {
			return ProbePairResult{}, context.DeadlineExceeded
		}
		if rtt1, reply1, err, ok := s.tryRecv(send1Time, probe1, true); ok {
			if err != nil {
				return ProbePairResult{}, fmt.Errorf("probe 1: %w", err)
			}
			return ProbePairResult{
				RTT0:   rtt0,
				RTT1:   rtt1,
				Reply0: reply0,
				Reply1: reply1,
			}, nil
		}
	}
}

// sendAndRecv sends a probe and waits for the matching reply.
// Caller must hold s.mu and have called runtime.LockOSThread().
func (s *LinuxSender) sendAndRecv(ctx context.Context, probeBuf []byte, probe *ProbePacket, verify bool, busyPollDuration time.Duration) (time.Duration, *ReplyPacket, error) {
	sendTime := time.Now()
	if err := unix.Sendto(s.fd, probeBuf, 0, s.remote); err != nil {
		return 0, nil, fmt.Errorf("sendto: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultProbeTimeout)
	}

	events := make([]unix.EpollEvent, 1)

	// Busy-poll phase: EpollWait(timeout=0) to avoid scheduler wakeup latency.
	if busyPollDuration > 0 {
		busyPollDeadline := sendTime.Add(busyPollDuration)
		for time.Now().Before(busyPollDeadline) {
			n, err := unix.EpollWait(s.epfd, events, 0)
			if err != nil && err != syscall.EINTR {
				return 0, nil, fmt.Errorf("epoll_wait: %w", err)
			}
			if n > 0 {
				if rtt, reply, err, ok := s.tryRecv(sendTime, probe, verify); ok {
					return rtt, reply, err
				}
			}
		}
	}

	// Blocking phase: standard EpollWait with full timeout.
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

		if rtt, reply, err, ok := s.tryRecv(sendTime, probe, verify); ok {
			return rtt, reply, err
		}
	}
}

// tryRecv reads one reply. Returns ok=true on a definitive result (match or
// fatal error), ok=false if the caller should keep polling.
func (s *LinuxSender) tryRecv(sendTime time.Time, probe *ProbePacket, verify bool) (time.Duration, *ReplyPacket, error, bool) {
	n, oobn, _, _, err := unix.Recvmsg(s.fd, s.buf, s.oob, 0)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0, nil, nil, false
		}
		return 0, nil, fmt.Errorf("recvmsg: %w", err), true
	}
	fallbackRecvTime := time.Now()

	if n < MinReplyPacketSize || n > MaxReplyPacketSize {
		return 0, nil, nil, false
	}

	reply, err := UnmarshalReplyPacket(s.buf[:n])
	if err != nil {
		return 0, nil, nil, false
	}

	if reply.Probe.Seq != probe.Seq || reply.Probe.Sec != probe.Sec || reply.Probe.Frac != probe.Frac {
		return 0, nil, nil, false
	}
	if verify {
		if !reply.Probe.Verify() {
			return 0, nil, nil, false
		}
		if reply.AuthorityPubkey != s.remotePubkey {
			return 0, nil, nil, false
		}
		if !reply.Verify() {
			return 0, nil, nil, false
		}
	}

	cmsgs, err := syscall.ParseSocketControlMessage(s.oob[:oobn])
	if err != nil {
		return 0, nil, fmt.Errorf("parse cmsg: %w", err), true
	}
	for _, cmsg := range cmsgs {
		if cmsg.Header.Level == syscall.SOL_SOCKET && cmsg.Header.Type == syscall.SO_TIMESTAMPNS {
			if len(cmsg.Data) < int(unsafe.Sizeof(syscall.Timespec{})) {
				continue
			}
			ts := *(*syscall.Timespec)(unsafe.Pointer(&cmsg.Data[0]))
			kernelRecvTime := time.Unix(int64(ts.Sec), int64(ts.Nsec))
			rtt := decideRTT(sendTime, kernelRecvTime, fallbackRecvTime)
			return rtt, reply, nil, true
		}
	}
	return 0, nil, fmt.Errorf("no timestamp in control message"), true
}

func (s *LinuxSender) Close() error {
	s.cancel()
	unix.Close(s.fd)
	unix.Close(s.epfd)
	return nil
}

func decideRTT(sendTime, kernelRecvTime, fallbackRecvTime time.Time) time.Duration {
	rtt := kernelRecvTime.Sub(sendTime)
	if rtt < -100*time.Microsecond {
		rtt = fallbackRecvTime.Sub(sendTime)
	}
	return max(rtt, 0)
}
