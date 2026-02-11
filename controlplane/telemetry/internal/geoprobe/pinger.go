package geoprobe

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	MaxConcurrentProbes = 100
)

type PingerConfig struct {
	Logger              *slog.Logger
	ProbeTimeout        time.Duration
	Interval            time.Duration
	ManagementNamespace string
}

type Pinger struct {
	log       *slog.Logger
	cfg       *PingerConfig
	senders   map[string]*senderEntry
	sendersMu sync.Mutex
}

type senderEntry struct {
	addr   ProbeAddress
	sender twamplight.Sender
}

func NewPinger(cfg *PingerConfig) *Pinger {
	return &Pinger{
		log:     cfg.Logger,
		cfg:     cfg,
		senders: make(map[string]*senderEntry),
	}
}

func (p *Pinger) AddProbe(ctx context.Context, addr ProbeAddress) error {
	p.sendersMu.Lock()
	defer p.sendersMu.Unlock()

	key := addr.String()

	if _, exists := p.senders[key]; exists {
		p.log.Debug("Probe already exists", "probe", key)
		return nil
	}

	if err := addr.Validate(); err != nil {
		return fmt.Errorf("invalid probe address %s: %w", key, err)
	}

	resolvedAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.Port)}

	sourceAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: 0,
	}

	iface := ""
	if p.cfg.ManagementNamespace != "" {
		iface = p.cfg.ManagementNamespace
	}

	sender, err := twamplight.NewSender(ctx, p.log, iface, sourceAddr, resolvedAddr)
	if err != nil {
		return fmt.Errorf("failed to create TWAMP sender for %s: %w", addr.String(), err)
	}

	p.senders[key] = &senderEntry{
		addr:   addr,
		sender: sender,
	}

	p.log.Info("Added probe", "probe", key, "resolved", resolvedAddr.String())
	return nil
}

func (p *Pinger) RemoveProbe(addr ProbeAddress) error {
	p.sendersMu.Lock()
	defer p.sendersMu.Unlock()

	key := addr.String()

	entry, exists := p.senders[key]
	if !exists {
		p.log.Warn("Probe not found for removal", "probe", key)
		return nil
	}

	if err := entry.sender.Close(); err != nil {
		p.log.Warn("Failed to close sender", "probe", key, "error", err)
	}

	delete(p.senders, key)
	p.log.Info("Removed probe", "probe", key)
	return nil
}

func (p *Pinger) MeasureAll(ctx context.Context) (map[ProbeAddress]uint64, error) {
	p.sendersMu.Lock()
	sendersCopy := make([]*senderEntry, 0, len(p.senders))
	for _, entry := range p.senders {
		sendersCopy = append(sendersCopy, entry)
	}
	p.sendersMu.Unlock()

	if len(sendersCopy) == 0 {
		return make(map[ProbeAddress]uint64), nil
	}

	totalProbes := len(sendersCopy)
	results := make(map[ProbeAddress]uint64)
	resultsMu := sync.Mutex{}
	var wg sync.WaitGroup

	sem := make(chan struct{}, MaxConcurrentProbes)

	for _, entry := range sendersCopy {
		wg.Add(1)
		go func(e *senderEntry) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			probeCtx, cancel := context.WithTimeout(ctx, p.cfg.ProbeTimeout)
			defer cancel()

			rtt, err := e.sender.Probe(probeCtx)
			if err != nil {
				p.log.Debug("Probe failed", "probe", e.addr.String(), "error", err)
				return
			}

			resultsMu.Lock()
			results[e.addr] = uint64(rtt.Nanoseconds())
			resultsMu.Unlock()

			p.log.Debug("Probe succeeded", "probe", e.addr.String(), "rtt", rtt)
		}(entry)
	}

	wg.Wait()

	successCount := len(results)
	failureCount := totalProbes - successCount

	p.log.Debug("Probe measurement completed",
		"total", totalProbes,
		"success", successCount,
		"failed", failureCount)

	return results, nil
}

func (p *Pinger) Close() error {
	p.sendersMu.Lock()
	defer p.sendersMu.Unlock()

	var lastErr error
	for key, entry := range p.senders {
		if err := entry.sender.Close(); err != nil {
			p.log.Warn("Failed to close sender", "probe", key, "error", err)
			lastErr = err
		}
	}

	p.senders = make(map[string]*senderEntry)
	p.log.Info("Closed all probes")

	return lastErr
}
