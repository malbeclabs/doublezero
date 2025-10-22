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

	"syscall"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// Defaults for a short, responsive probing loop.
const (
	defaultSenderCount   = 3
	defaultSenderTimeout = 3 * time.Second
	maxPollSlice         = 200 * time.Millisecond // cap per-Recv block to avoid overshooting deadlines
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

// sender owns the socket and addressing state.
// A mutex serializes Send and Close to the single underlying conn.
type sender struct {
	log     *slog.Logger
	cfg     SenderConfig
	sip     net.IP           // IPv4 source (validated)
	ifIndex int              // ifindex derived from Interface
	pid     uint16           // echo identifier seed (pid & 0xffff)
	ipc     *net.IPConn      // ip4:icmp bound to src
	ip4c    *ipv4.PacketConn // ipv4 wrapper
	mu      sync.Mutex
}

// NewSender opens an ICMP socket bound to Source, pins to device, sets TTL,
// validates IPv4 source, and resolves the interface index. Fails fast on misconfig.
func NewSender(cfg SenderConfig) (Sender, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	sip := cfg.Source.To4() // safe: Validate() ensures IPv4

	// REQUIRED: resolve interface index; fail if not present.
	ifi, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", cfg.Interface, err)
	}

	// Bind to the source IP so the kernel builds IPv4 and selects routes accordingly.
	ipc, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: sip})
	if err != nil {
		return nil, err
	}

	// Wrap so we can use control messages and TTL helpers.
	ip4c := ipv4.NewPacketConn(ipc)
	_ = ip4c.SetTTL(64)
	_ = ip4c.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true)

	// Pin the socket to the given interface for both RX and TX routing.
	if err := bindToDevice(ipc, ifi.Name); err != nil {
		_ = ip4c.Close()
		_ = ipc.Close()
		return nil, fmt.Errorf("bind-to-device %q: %w", ifi.Name, err)
	}

	return &sender{
		log:     cfg.Logger,
		cfg:     cfg,
		sip:     sip,
		ifIndex: ifi.Index,
		pid:     uint16(os.Getpid() & 0xffff),
		ipc:     ipc,
		ip4c:    ip4c,
	}, nil
}

// Close closes the underlying socket. Concurrency-safe with Send via s.mu.
func (s *sender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ip4c != nil {
		_ = s.ip4c.Close()
	}
	if s.ipc != nil {
		return s.ipc.Close()
	}
	return nil
}

// Send transmits Count echo requests and waits up to Timeout for each reply.
// It steers egress by iface and source using an ipv4.ControlMessage and validates
// echo replies by id/seq/nonce.
func (s *sender) Send(ctx context.Context, cfg SendConfig) (*SendResults, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dip := cfg.Target.To4()
	if dip == nil {
		return nil, fmt.Errorf("invalid target IP: %s", cfg.Target)
	}

	// Serialize access to the single socket and protect against Close.
	s.mu.Lock()
	defer s.mu.Unlock()

	results := &SendResults{Results: make([]SendResult, 0, cfg.Count)}
	seq := uint16(1)
	dst := &net.IPAddr{IP: dip}

	// Per-Send() reusable buffers to avoid hot-path allocations.
	buf := make([]byte, 8192)              // RX buffer (ICMP payload when using PacketConn)
	payload := make([]byte, 8)             // 8-byte nonce in ICMP echo data
	nonce := uint64(time.Now().UnixNano()) // starting nonce; increment per probe

	for i := 0; i < cfg.Count; i++ {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Prepare ICMP Echo with (id, seq, nonce).
		nonce++
		binary.BigEndian.PutUint64(payload, nonce)
		wb, err := (&icmp.Message{
			Type: ipv4.ICMPTypeEcho, Code: 0,
			Body: &icmp.Echo{ID: int(s.pid), Seq: int(seq), Data: payload},
		}).Marshal(nil)
		if err != nil {
			results.Results = append(results.Results, SendResult{RTT: -1, Error: err})
			seq++
			continue
		}

		// Per-packet steering: IfIndex emulate IP_PKTINFO (Spec_dst + ifindex).
		cm := &ipv4.ControlMessage{IfIndex: s.ifIndex}

		t0 := time.Now()
		if _, err := s.ip4c.WriteTo(wb, cm, dst); err != nil {
			// Try a reopen on common transient send failures.
			if transientSendRetryable(err) {
				if s.log != nil {
					s.log.Info("uping/sender: reopen after send err", "i", i+1, "seq", seq, "err", err)
				}
				if e := s.reopen(); e == nil {
					cm = &ipv4.ControlMessage{IfIndex: s.ifIndex}
					_, err = s.ip4c.WriteTo(wb, cm, dst)
				}
			}
			// One-shot retry on EPERM after a tiny backoff
			// This can happen sometimes especially on loopback interfaces.
			if err != nil && errors.Is(err, syscall.EPERM) {
				time.Sleep(5 * time.Millisecond)
				_, err = s.ip4c.WriteTo(wb, nil, dst)
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
			remain := time.Until(deadline)
			if remain <= 0 {
				break
			}
			if remain > maxPollSlice {
				remain = maxPollSlice
			}
			_ = s.ipc.SetReadDeadline(time.Now().Add(remain))

			n, rcm, raddr, err := s.ip4c.ReadFrom(buf)
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if err != nil {
				// If the socket became invalid/transient, try reopening and continue waiting.
				if transientSocketErr(err) {
					if s.log != nil {
						s.log.Info("uping/sender: reopen after recv err", "i", i+1, "seq", seq, "err", err)
					}
					if e := s.reopen(); e == nil {
						_ = s.ipc.SetReadDeadline(time.Now().Add(time.Until(deadline)))
						continue
					}
				}
				if s.log != nil {
					s.log.Error("uping/sender: recv", "i", i+1, "seq", seq, "err", err)
				}
				continue
			}

			// Optionally filter by ingress ifindex when available.
			if rcm != nil && rcm.IfIndex != 0 && rcm.IfIndex != s.ifIndex {
				continue
			}

			// Parse and validate an echo reply. buf[:n] is ICMP payload with PacketConn,
			// or full IPv4 if the stack delivers that; validateEchoReply handles both.
			rtt := time.Since(t0)
			ok, src, itype, icode := validateEchoReply(buf[:n], s.pid, seq, nonce)
			if ok {
				if s.log != nil {
					ip := src
					if ip == nil || ip.Equal(net.IPv4zero) {
						if ipaddr, _ := raddr.(*net.IPAddr); ipaddr != nil {
							ip = ipaddr.IP
						}
					}
					s.log.Info("uping/sender: reply", "i", i+1, "seq", seq, "src", ip.String(), "rtt", rtt, "len", n)
				}
				results.Results = append(results.Results, SendResult{RTT: rtt, Error: nil})
				got = true
				break
			}
			if s.log != nil {
				ip := src
				if ip == nil || ip.Equal(net.IPv4zero) {
					if ipaddr, _ := raddr.(*net.IPAddr); ipaddr != nil {
						ip = ipaddr.IP
					}
				}
				s.log.Debug("uping/sender: ignored", "i", i+1, "seq", seq, "src", ip.String(), "icmp_type", itype, "icmp_code", icode)
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

// bindToDevice applies SO_BINDTODEVICE to c’s socket so traffic stays on ifname.
func bindToDevice(c any, ifname string) error {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return fmt.Errorf("no raw fd")
	}
	var setErr error
	raw, err := sc.SyscallConn()
	if err != nil {
		return err
	}
	if err := raw.Control(func(fd uintptr) {
		if e := unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname); e != nil {
			setErr = e
		}
	}); err != nil {
		return err
	}
	return setErr
}

// validateEchoReply parses a packet or ICMP message, verifies checksum,
// and returns true only for Echo Reply (type=0, code=0) matching (id, seq, nonce).
// Accepts either a full IPv4 packet or a bare ICMP payload.
func validateEchoReply(pkt []byte, wantID, wantSeq uint16, wantNonce uint64) (bool, net.IP, int, int) {
	// Full IPv4?
	if len(pkt) >= 20 && pkt[0]>>4 == 4 {
		ihl := int(pkt[0]&0x0F) * 4
		if ihl < 20 || len(pkt) < ihl+8 {
			return false, net.IPv4zero, -1, -1
		}
		if pkt[9] != 1 { // not ICMP
			return false, net.IP(pkt[12:16]), int(pkt[9]), -1
		}
		src := net.IP(pkt[12:16])
		return validateICMPEcho(pkt[ihl:], wantID, wantSeq, wantNonce, src)
	}
	// Otherwise treat as bare ICMP payload from PacketConn.
	return validateICMPEcho(pkt, wantID, wantSeq, wantNonce, net.IPv4zero)
}

// validateICMPEcho verifies checksum, parses with icmp.ParseMessage, and matches id/seq/nonce.
// src is surfaced unchanged (IPv4zero for bare ICMP).
func validateICMPEcho(icmpb []byte, wantID, wantSeq uint16, wantNonce uint64, src net.IP) (bool, net.IP, int, int) {
	if len(icmpb) < 8 {
		return false, src, -1, -1
	}
	// Raw for logging/return
	itype := int(icmpb[0])
	icode := int(icmpb[1])

	// Verify Internet checksum over ICMP message.
	if icmpChecksum(icmpb) != 0 {
		return false, src, itype, icode
	}

	m, err := icmp.ParseMessage(1, icmpb)
	if err != nil {
		return false, src, itype, icode
	}

	// Only accept Echo Reply (type=0, code=0). Use m.Type for the predicate.
	if m.Type != ipv4.ICMPTypeEchoReply {
		return false, src, itype, icode
	}
	echo, ok := m.Body.(*icmp.Echo)
	if !ok || echo == nil {
		return false, src, itype, icode
	}
	if len(echo.Data) < 8 {
		return false, src, itype, icode
	}
	gotNonce := binary.BigEndian.Uint64(echo.Data[:8])
	if uint16(echo.ID) == wantID && uint16(echo.Seq) == wantSeq && gotNonce == wantNonce {
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

// reopen replaces the socket with a fresh ip4:icmp socket and reapplies base options.
// Used after transient errors (device down, address not ready, etc.).
func (s *sender) reopen() error {
	if s.ip4c != nil {
		_ = s.ip4c.Close()
	}
	if s.ipc != nil {
		_ = s.ipc.Close()
	}
	ipc, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: s.sip})
	if err != nil {
		return err
	}
	ip4c := ipv4.NewPacketConn(ipc)
	_ = ip4c.SetTTL(64)
	_ = ip4c.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true)

	// Re-pin to device.
	if err := bindToDevice(ipc, s.cfg.Interface); err != nil {
		_ = ip4c.Close()
		_ = ipc.Close()
		return fmt.Errorf("bind-to-device %q: %w", s.cfg.Interface, err)
	}

	// Re-resolve ifindex defensively.
	s.refreshIfIndex()

	s.ipc, s.ip4c = ipc, ip4c
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
	// net errors often wrap unix errors; keep the common set.
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENETDOWN) || errors.Is(err, unix.ENODEV) ||
		errors.Is(err, unix.EADDRNOTAVAIL) || errors.Is(err, unix.ENOBUFS) || errors.Is(err, unix.ENETRESET) ||
		errors.Is(err, unix.ENOMEM)
}

// transientSendRetryable classifies send errors that are often recoverable with a reopen
func transientSendRetryable(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ENETDOWN)
}
