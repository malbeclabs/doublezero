//go:build linux

package uping

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Defaults for a short, responsive probing loop.
const (
	defaultSenderCount   = 3
	defaultSenderTimeout = 3 * time.Second
	maxPollSlice         = 200 * time.Millisecond // cap per-Recvfrom block to avoid overshooting deadlines
)

// SenderConfig configures the raw-ICMP sender.
// Both Interface and Source are REQUIRED and must be IPv4-capable.
type SenderConfig struct {
	Logger    *slog.Logger // optional
	Interface string       // required: interface name; used to resolve ifindex for PKTINFO
	Source    net.IP       // required: IPv4 source address used as Spec_dst
}

// Validate enforces required fields and IPv4-ness.
func (cfg *SenderConfig) Validate() error {
	if cfg.Interface == "" {
		return fmt.Errorf("interface is required")
	}
	if cfg.Source == nil || cfg.Source.To4() == nil {
		return fmt.Errorf("source must be a valid IPv4 address")
	}
	return nil
}

// SendConfig describes a single multi-probe operation.
type SendConfig struct {
	Target  net.IP        // required: IPv4 destination
	Count   int           // number of probes; defaulted if zero
	Timeout time.Duration // per-probe absolute timeout; defaulted if zero
}

func (cfg *SendConfig) Validate() error {
	if cfg.Count == 0 {
		cfg.Count = defaultSenderCount
	}
	if cfg.Count <= 0 {
		return fmt.Errorf("count must be greater than 0")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultSenderTimeout
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}
	return nil
}

// SendResults is a bag of per-probe results; Failed() indicates any error occurred.
type SendResults struct{ Results []SendResult }

func (rs *SendResults) Failed() bool {
	for _, r := range rs.Results {
		if r.Error != nil {
			return true
		}
	}
	return false
}

// SendResult records the RTT (on success) or the error (on failure) for a single probe.
type SendResult struct {
	RTT   time.Duration
	Error error
}

// Sender exposes the echo send/wait API.
type Sender interface {
	Send(ctx context.Context, cfg SendConfig) (*SendResults, error)
	Close() error
}

// sender owns the raw socket and addressing state.
// A mutex serializes Send and Close to the single FD.
type sender struct {
	log       *slog.Logger
	cfg       SenderConfig
	sip       net.IP // IPv4 source (validated)
	fd        int    // raw IPv4 ICMP socket
	pid       uint16 // echo identifier seed (pid & 0xffff)
	ifIndex   int    // ifindex derived from Interface
	connected bool
	mu        sync.Mutex
}

// NewSender opens a raw ICMP socket, enables IP_PKTINFO, sets TTL, validates
// IPv4 source, and resolves the interface index. Fails fast on misconfig.
func NewSender(cfg SenderConfig) (Sender, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = unix.Close(fd)
		}
	}()

	sip := cfg.Source.To4() // safe: Validate() ensures IPv4

	// Allow specifying egress ifindex and source address per packet (IP_PKTINFO).
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_PKTINFO, 1); err != nil {
		return nil, fmt.Errorf("enable IP_PKTINFO: %w", err)
	}

	// Sane default TTL; kernel builds IPv4 header for raw ICMP sockets.
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, 64)

	// REQUIRED: resolve interface index; fail if not present.
	ifi, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", cfg.Interface, err)
	}
	ifIndex := ifi.Index

	ok = true
	return &sender{
		log:     cfg.Logger,
		cfg:     cfg,
		sip:     sip,
		fd:      fd,
		pid:     uint16(os.Getpid() & 0xffff),
		ifIndex: ifIndex,
	}, nil
}

// Close closes the underlying raw socket. Concurrency-safe with Send via s.mu.
func (s *sender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return unix.Close(s.fd)
}

// Send transmits Count echo requests and waits up to Timeout for each reply.
// It uses IP_PKTINFO to steer egress and validates echo replies by id/seq/nonce.
func (s *sender) Send(ctx context.Context, cfg SendConfig) (*SendResults, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dip := cfg.Target.To4()
	if dip == nil {
		return nil, fmt.Errorf("invalid target IP: %s", cfg.Target)
	}

	// Serialize access to the single raw socket and protect against Close.
	s.mu.Lock()
	defer s.mu.Unlock()

	results := &SendResults{Results: make([]SendResult, 0, cfg.Count)}
	seq := uint16(1)
	dst := &unix.SockaddrInet4{Addr: [4]byte{dip[0], dip[1], dip[2], dip[3]}}

	// Best-effort: connect to narrow inbound to the target (filters RX path).
	s.maybeConnect(dst)

	// Per-Send() reusable buffers to avoid hot-path allocations.
	buf := make([]byte, 4096)                // RX buffer (IPv4 header + ICMP + payload)
	oob := s.buildPktinfoOOB()               // PKTINFO control message
	payload := make([]byte, 8)               // 8-byte nonce in ICMP echo data
	icmp := ensureICMPBuf(nil, len(payload)) // ICMP packet buffer (header + payload)
	nonce := uint64(time.Now().UnixNano())   // starting nonce; increment per probe

	for i := 0; i < cfg.Count; i++ {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Prepare ICMP Echo with (id, seq, nonce). Reuse buffers; recompute checksum.
		nonce++
		binary.BigEndian.PutUint64(payload, nonce)
		if len(icmp) != 8+len(payload) {
			icmp = ensureICMPBuf(icmp, len(payload))
		}
		fillICMPEcho(icmp, s.pid, seq, payload)

		// When connected, do NOT pass a destination; otherwise, pass dst.
		sendDst := unix.Sockaddr(nil)
		if !s.connected {
			sendDst = dst
		}

		t0 := time.Now()
		if _, err := unix.SendmsgN(s.fd, icmp, oob, sendDst, 0); err != nil {
			// Only retry on errors that imply no datagram could have been queued.
			if transientSendRetryable(err) {
				if s.log != nil {
					s.log.Info("uping/sender: reopen after send err", "i", i+1, "seq", seq, "err", err)
				}
				if e := s.reopen(); e == nil {
					s.maybeConnect(dst)
					oob = s.buildPktinfoOOB()
					sendDst = nil
					if !s.connected {
						sendDst = dst
					}
					_, err = unix.SendmsgN(s.fd, icmp, oob, sendDst, 0)
				}
			}
			if err != nil {
				if s.log != nil {
					s.log.Error("uping/sender: send", "i", i+1, "seq", seq, "err", err)
				}
				results.Results = append(results.Results, SendResult{RTT: -1, Error: err})
				seq++
				continue
			}
		}

		got := false
		deadline := t0.Add(cfg.Timeout)

		// Poll for a reply until the absolute deadline.
		for {
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			if !deadline.After(time.Now()) {
				break
			}
			if !s.applyRecvSlice(deadline) {
				break
			}

			n, _, err := unix.Recvfrom(s.fd, buf, 0)
			if err != nil {
				// Expected transient read conditions: try again within the remaining deadline.
				if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
					continue
				}
				// If the socket became invalid/transient, try reopening and continue waiting.
				if transientSocketErr(err) {
					if s.log != nil {
						s.log.Info("uping/sender: reopen after recv err", "i", i+1, "seq", seq, "err", err)
					}
					if e := s.reopen(); e == nil {
						s.maybeConnect(dst)
						oob = s.buildPktinfoOOB()
						_ = s.applyRecvSlice(deadline)
					}
				}
				if s.log != nil {
					s.log.Error("uping/sender: recv", "i", i+1, "seq", seq, "err", err)
				}
				continue
			}

			// Parse and validate an echo reply (checksum, type/code, id/seq/nonce).
			rtt := time.Since(t0)
			ok, src, itype, icode := validateEchoReply(buf[:n], s.pid, seq, nonce)
			if ok {
				if s.log != nil {
					s.log.Info("uping/sender: reply", "i", i+1, "seq", seq, "src", src.String(), "rtt", rtt, "len", n)
				}
				results.Results = append(results.Results, SendResult{RTT: rtt, Error: nil})
				got = true
				break
			}
			if s.log != nil {
				s.log.Debug("uping/sender: ignored", "i", i+1, "seq", seq, "src", src.String(), "icmp_type", itype, "icmp_code", icode)
			}
		}

		if !got {
			err := fmt.Errorf("timeout waiting for seq=%d", seq)
			if s.log != nil {
				s.log.Warn("uping/sender: timeout", "i", i+1, "seq", seq, "err", err)
			}
			results.Results = append(results.Results, SendResult{RTT: -1, Error: err})
		}
		seq++
	}

	return results, nil
}

// applyRecvSlice sets SO_RCVTIMEO to min(deadline-now, maxPollSlice) on the current socket,
// ensuring Recvfrom won't block past the absolute deadline. Returns false if the deadline
// has already passed.
func (s *sender) applyRecvSlice(deadline time.Time) bool {
	remain := time.Until(deadline)
	if remain <= 0 {
		return false
	}
	if remain > maxPollSlice {
		remain = maxPollSlice
	}
	tv := durationToTimeval(remain)
	_ = unix.SetsockoptTimeval(s.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	return true
}

// ensureICMPBuf returns a slice of size 8+payloadLen backed by dst if possible,
// or a new allocation if dst is too small. Intended to be called rarely (once).
func ensureICMPBuf(dst []byte, payloadLen int) []byte {
	need := 8 + payloadLen
	if cap(dst) < need {
		return make([]byte, need)
	}
	return dst[:need]
}

// fillICMPEcho writes an ICMP Echo Request into dst (type 8) with the given id/seq/payload,
// and computes the checksum over the whole ICMP message. dst must be 8+len(payload) bytes.
func fillICMPEcho(dst []byte, id, seq uint16, payload []byte) {
	dst[0] = 8
	dst[1] = 0
	dst[2], dst[3] = 0, 0
	binary.BigEndian.PutUint16(dst[4:], id)
	binary.BigEndian.PutUint16(dst[6:], seq)
	copy(dst[8:], payload)
	binary.BigEndian.PutUint16(dst[2:], icmpChecksum(dst))
}

// maybeConnect optionally connects the raw socket to the destination to filter inbound
// packets by peer. It skips connecting loopback->non-loopback (to avoid odd routing paths)
// and ignores common routing/address errors so RX remains unconnected if connect fails.
func (s *sender) maybeConnect(dst *unix.SockaddrInet4) {
	if s.sip != nil && s.sip.IsLoopback() && !net.IP(dst.Addr[:]).IsLoopback() {
		return
	}
	if err := unix.Connect(s.fd, dst); err != nil {
		if s.log != nil {
			s.log.Debug("connect skipped", "err", err)
		}
		s.connected = false
		return
	}
	s.connected = true
}

// buildPktinfoOOB constructs a single IP_PKTINFO control message to steer egress
// (ifIndex and Spec_dst). Given validation, this should return a non-nil buffer.
func (s *sender) buildPktinfoOOB() []byte {
	// Guard left in place for safety/future changes.
	if s.ifIndex == 0 && s.sip == nil {
		return nil
	}

	oob := make([]byte, unix.CmsgSpace(unix.SizeofInet4Pktinfo))

	cm := (*unix.Cmsghdr)(unsafe.Pointer(&oob[0]))
	cm.Level = unix.IPPROTO_IP
	cm.Type = unix.IP_PKTINFO
	cm.SetLen(unix.CmsgLen(unix.SizeofInet4Pktinfo))

	// Control data payload for IP_PKTINFO immediately follows the cmsghdr.
	data := oob[unix.CmsgLen(0):unix.CmsgLen(unix.SizeofInet4Pktinfo)]

	var pi unix.Inet4Pktinfo
	pi.Ifindex = int32(s.ifIndex)
	copy(pi.Spec_dst[:], s.sip.To4())

	*(*unix.Inet4Pktinfo)(unsafe.Pointer(&data[0])) = pi
	return oob
}

// validateEchoReply parses an IPv4 packet, verifies it's ICMP, verifies ICMP checksum,
// and returns true only for Echo Reply (type=0, code=0) matching (id, seq, nonce).
// For non-echo ICMP or malformed packets, it returns false with best-effort type/code.
func validateEchoReply(pkt []byte, wantID, wantSeq uint16, wantNonce uint64) (bool, net.IP, int, int) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return false, net.IPv4zero, -1, -1
	}
	ihl := int(pkt[0]&0x0F) * 4
	if ihl < 20 || len(pkt) < ihl+8 {
		return false, net.IPv4zero, -1, -1
	}

	// Not ICMP: surface protocol number as "type".
	if pkt[9] != 1 {
		return false, net.IP(pkt[12:16]), int(pkt[9]), -1
	}

	src := net.IP(pkt[12:16])
	icmp := pkt[ihl:]

	// Surface type/code whenever available, even if we later reject.
	itype, icode := -1, -1
	if len(icmp) >= 2 {
		itype, icode = int(icmp[0]), int(icmp[1])
	}

	// Most ICMP messages are at least 8 bytes. Verify checksum when possible.
	if len(icmp) < 8 {
		return false, src, itype, icode
	}
	if icmpChecksum(icmp) != 0 {
		return false, src, itype, icode
	}

	// Only accept Echo Reply.
	if itype != 0 || icode != 0 {
		return false, src, itype, icode
	}

	// Echo Reply must include id/seq + our 8-byte nonce.
	if len(icmp) < 16 {
		return false, src, itype, icode
	}

	rid := binary.BigEndian.Uint16(icmp[4:6])
	rseq := binary.BigEndian.Uint16(icmp[6:8])
	gotNonce := binary.BigEndian.Uint64(icmp[8:16])
	if rid == wantID && rseq == wantSeq && gotNonce == wantNonce {
		return true, src, itype, icode
	}
	return false, src, itype, icode
}

// icmpChecksum computes the standard Internet checksum over the ICMP message.
func icmpChecksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = (s & 0xffff) + (s >> 16)
	}
	return ^uint16(s)
}

// durationToTimeval converts a Go duration to a timeval for SO_RCVTIMEO.
func durationToTimeval(d time.Duration) unix.Timeval {
	if d <= 0 {
		return unix.Timeval{}
	}
	sec := d / time.Second
	usec := (d % time.Second) / time.Microsecond
	return unix.Timeval{Sec: int64(sec), Usec: int64(usec)}
}

// reopen replaces the socket with a fresh raw ICMP socket and reapplies base options.
// Used after transient errors (device down, address not ready, etc.).
func (s *sender) reopen() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	if err != nil {
		return err
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_PKTINFO, 1); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("enable IP_PKTINFO: %w", err)
	}
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, 64)
	_ = unix.Close(s.fd)
	s.fd = fd
	s.refreshIfIndex()
	return nil
}

// refreshIfIndex re-resolves the interface index on demand (e.g., after a socket reopen).
func (s *sender) refreshIfIndex() {
	ifi, err := net.InterfaceByName(s.cfg.Interface)
	if err == nil {
		s.ifIndex = ifi.Index
	}
}

// transientSocketErr classifies socket errors that are often recoverable with a reopen.
func transientSocketErr(err error) bool {
	return errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENETDOWN) || errors.Is(err, unix.ENODEV) ||
		errors.Is(err, unix.EADDRNOTAVAIL) || errors.Is(err, unix.ENOBUFS) || errors.Is(err, unix.ENETRESET) ||
		errors.Is(err, unix.ENOMEM)
}

// transientSendRetryable classifies send errors that are often recoverable with a reopen
func transientSendRetryable(err error) bool {
	// These imply no datagram could have been queued
	return errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ENETDOWN)
}
