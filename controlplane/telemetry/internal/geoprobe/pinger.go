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
	MaxWorkers          = 32
	ProbesPerWorker     = 512
	DefaultStaggerDelay = 100 * time.Millisecond
)

type PingerConfig struct {
	Logger              *slog.Logger
	ProbeTimeout        time.Duration
	Interval            time.Duration
	ManagementNamespace string
	StaggerDelay        time.Duration
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

func (p *Pinger) probeWorker(
	ctx context.Context,
	batch []*senderEntry,
	staggerDelay time.Duration,
	results map[ProbeAddress]uint64,
	resultsMu *sync.Mutex,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for i, entry := range batch {
		select {
		case <-ctx.Done():
			return
		default:
		}

		probeCtx, cancel := context.WithTimeout(ctx, p.cfg.ProbeTimeout)
		rtt, err := entry.sender.Probe(probeCtx)
		cancel()

		if err == nil {
			resultsMu.Lock()
			results[entry.addr] = uint64(rtt.Nanoseconds())
			resultsMu.Unlock()

			p.log.Debug("Probe succeeded", "probe", entry.addr.String(), "rtt", rtt)
		} else {
			p.log.Debug("Probe failed", "probe", entry.addr.String(), "error", err)
		}

		if i < len(batch)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(staggerDelay):
			}
		}
	}
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

	numWorkers := min(MaxWorkers, (len(sendersCopy)+ProbesPerWorker-1)/ProbesPerWorker)
	if numWorkers == 0 {
		numWorkers = 1
	}
	batchSize := (len(sendersCopy) + numWorkers - 1) / numWorkers

	staggerDelay := p.cfg.StaggerDelay
	if staggerDelay == 0 {
		staggerDelay = DefaultStaggerDelay
	}

	for i := 0; i < numWorkers; i++ {
		start := i * batchSize
		end := min(start+batchSize, len(sendersCopy))
		if start >= len(sendersCopy) {
			break
		}

		batch := sendersCopy[start:end]
		wg.Add(1)
		go p.probeWorker(ctx, batch, staggerDelay, results, &resultsMu, &wg)
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
