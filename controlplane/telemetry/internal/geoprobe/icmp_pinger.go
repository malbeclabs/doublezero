//go:build linux

package geoprobe

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	ICMPDefaultBatchSize    = 512
	ICMPDefaultStaggerDelay = 200 * time.Microsecond
	ICMPDefaultProbeTimeout = 1 * time.Second
	ICMPMeasureOneTimeout   = 100 * time.Millisecond

	icmpProtocol    = 1  // IANA protocol number for ICMP
	icmpPayloadSize = 56 // standard ping payload size
)

type ICMPPingerConfig struct {
	Logger       *slog.Logger
	ProbeTimeout time.Duration // final drain timeout (default 1s)
	BatchSize    int           // targets per batch (default 512)
	StaggerDelay time.Duration // delay between sends within a batch
}

type ICMPPinger struct {
	conn   icmpSocket
	probes map[string]*icmpProbeEntry
	cfg    *ICMPPingerConfig
	seq    atomic.Uint32
	id     int
	mu     sync.RWMutex
	log    *slog.Logger
}

type icmpProbeEntry struct {
	addr ProbeAddress
	ip   net.IP
}

type pendingProbe struct {
	addr   ProbeAddress
	txTime time.Time
}

func NewICMPPinger(cfg *ICMPPingerConfig) (*ICMPPinger, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ProbeTimeout == 0 {
		cfg.ProbeTimeout = ICMPDefaultProbeTimeout
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = ICMPDefaultBatchSize
	}
	if cfg.StaggerDelay == 0 {
		cfg.StaggerDelay = ICMPDefaultStaggerDelay
	}

	conn, err := newICMPConn(cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ICMPPinger{
		conn:   conn,
		probes: make(map[string]*icmpProbeEntry),
		cfg:    cfg,
		id:     os.Getpid() & 0xffff,
		log:    cfg.Logger,
	}, nil
}

func (p *ICMPPinger) AddProbe(addr ProbeAddress) error {
	if err := addr.ValidateICMP(); err != nil {
		return err
	}

	ip := net.ParseIP(addr.Host)
	if ip == nil {
		return net.InvalidAddrError(addr.Host)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := addr.Host
	if _, exists := p.probes[key]; exists {
		p.log.Debug("ICMP probe already exists", "host", key)
		return nil
	}

	p.probes[key] = &icmpProbeEntry{addr: addr, ip: ip}
	p.log.Info("Added ICMP probe", "host", key)
	return nil
}

func (p *ICMPPinger) RemoveProbe(addr ProbeAddress) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := addr.Host
	if _, exists := p.probes[key]; !exists {
		p.log.Warn("ICMP probe not found for removal", "host", key)
		return nil
	}

	delete(p.probes, key)
	p.log.Info("Removed ICMP probe", "host", key)
	return nil
}

func (p *ICMPPinger) buildEchoRequest(seq uint16) ([]byte, error) {
	payload := make([]byte, icmpPayloadSize)
	binary.BigEndian.PutUint16(payload[0:2], seq)

	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.id,
			Seq:  int(seq),
			Data: payload,
		},
	}
	return msg.Marshal(nil)
}

func (p *ICMPPinger) sendEcho(entry *icmpProbeEntry, seq uint16) (time.Time, error) {
	b, err := p.buildEchoRequest(seq)
	if err != nil {
		return time.Time{}, err
	}
	return p.conn.sendEcho(entry.ip, b)
}

// readReplies reads ICMP echo replies until the deadline, matching against the pending map.
// Matched entries are removed from pending and their RTTs are written to results.
func (p *ICMPPinger) readReplies(pending map[uint16]*pendingProbe, results map[ProbeAddress]uint64) {
	buf := make([]byte, 1500)
	for len(pending) > 0 {
		n, rxTime, err := p.conn.recvEcho(buf)
		if err != nil {
			if err == syscall.EAGAIN {
				// Spurious epoll wakeup; re-enter recvEcho with remaining deadline.
				continue
			}
			return
		}

		msg, err := icmp.ParseMessage(icmpProtocol, buf[:n])
		if err != nil {
			continue
		}

		echo, ok := msg.Body.(*icmp.Echo)
		if !ok || msg.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if echo.ID != p.id {
			continue
		}

		seq := uint16(echo.Seq)
		pp, exists := pending[seq]
		if !exists {
			continue
		}

		rtt := uint64(rxTime.Sub(pp.txTime).Nanoseconds())
		results[pp.addr] = rtt
		delete(pending, seq)
	}
}

func (p *ICMPPinger) MeasureOne(ctx context.Context, addr ProbeAddress) (uint64, bool) {
	p.mu.RLock()
	entry, exists := p.probes[addr.Host]
	p.mu.RUnlock()

	if !exists {
		p.log.Warn("MeasureOne called for unknown ICMP probe", "host", addr.Host)
		return 0, false
	}

	seq := uint16(p.seq.Add(1))
	txTime, err := p.sendEcho(entry, seq)
	if err != nil {
		p.log.Debug("MeasureOne send failed", "host", addr.Host, "error", err)
		return 0, false
	}

	timeout := ICMPMeasureOneTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if ctxTimeout := time.Until(deadline); ctxTimeout < timeout {
			timeout = ctxTimeout
		}
	}

	if err := p.conn.setReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, false
	}

	buf := make([]byte, 1500)
	for {
		n, rxTime, err := p.conn.recvEcho(buf)
		if err != nil {
			if err == syscall.EAGAIN {
				// Spurious epoll wakeup; re-enter recvEcho with remaining deadline.
				continue
			}
			return 0, false
		}

		msg, err := icmp.ParseMessage(icmpProtocol, buf[:n])
		if err != nil {
			continue
		}

		echo, ok := msg.Body.(*icmp.Echo)
		if !ok || msg.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if echo.ID != p.id || uint16(echo.Seq) != seq {
			continue
		}

		rtt := uint64(rxTime.Sub(txTime).Nanoseconds())
		p.log.Debug("MeasureOne succeeded", "host", addr.Host, "rtt_ns", rtt)
		return rtt, true
	}
}

func (p *ICMPPinger) MeasureAll(ctx context.Context) (map[ProbeAddress]uint64, error) {
	p.mu.RLock()
	entries := make([]*icmpProbeEntry, 0, len(p.probes))
	for _, e := range p.probes {
		entries = append(entries, e)
	}
	p.mu.RUnlock()

	results := make(map[ProbeAddress]uint64, len(entries))
	if len(entries) == 0 {
		return results, nil
	}

	pending := make(map[uint16]*pendingProbe)
	batchSize := p.cfg.BatchSize

	for i := 0; i < len(entries); i += batchSize {
		end := i + batchSize
		isLast := end >= len(entries)
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[i:end]

		for j, entry := range batch {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			default:
			}

			seq := uint16(p.seq.Add(1))
			txTime, err := p.sendEcho(entry, seq)
			if err != nil {
				p.log.Debug("MeasureAll send failed", "host", entry.addr.Host, "error", err)
				continue
			}
			pending[seq] = &pendingProbe{addr: entry.addr, txTime: txTime}

			if j < len(batch)-1 {
				time.Sleep(p.cfg.StaggerDelay)
			}
		}

		if isLast {
			_ = p.conn.setReadDeadline(time.Now().Add(p.cfg.ProbeTimeout))
		} else {
			_ = p.conn.setReadDeadline(time.Now().Add(5 * time.Millisecond))
		}
		p.readReplies(pending, results)
	}

	total := len(entries)
	p.log.Debug("ICMP measurement completed",
		"total", total,
		"success", len(results),
		"failed", total-len(results))

	return results, nil
}

func (p *ICMPPinger) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.probes = make(map[string]*icmpProbeEntry)
	if p.conn != nil {
		return p.conn.close()
	}
	return nil
}
