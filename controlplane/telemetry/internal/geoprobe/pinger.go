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
	DefaultWarmupDelay  = 2 * time.Millisecond
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
	addr         ProbeAddress
	sender       twamplight.Sender
	warmupSender twamplight.Sender
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

	resolvedAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.TWAMPPort)}

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

	warmupSourceAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	warmupSender, err := twamplight.NewSender(ctx, p.log, iface, warmupSourceAddr, resolvedAddr)
	if err != nil {
		sender.Close()
		return fmt.Errorf("failed to create warmup TWAMP sender for %s: %w", addr.String(), err)
	}

	p.senders[key] = &senderEntry{
		addr:         addr,
		sender:       sender,
		warmupSender: warmupSender,
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
	if err := entry.warmupSender.Close(); err != nil {
		p.log.Warn("Failed to close warmup sender", "probe", key, "error", err)
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

		// Send a warmup probe first to wake the reflector's thread, then
		// send the measurement probe after a short delay. Both run on
		// separate sockets so neither blocks the other. We take the min RTT.
		type probeResult struct {
			rtt time.Duration
			err error
		}
		ch := make(chan probeResult, 2)
		go func() {
			rtt, err := entry.warmupSender.Probe(probeCtx)
			ch <- probeResult{rtt, err}
		}()
		go func() {
			time.Sleep(DefaultWarmupDelay)
			rtt, err := entry.sender.Probe(probeCtx)
			ch <- probeResult{rtt, err}
		}()

		var bestRTT time.Duration
		ok := false
		for range 2 {
			r := <-ch
			if r.err == nil && (!ok || r.rtt < bestRTT) {
				bestRTT = r.rtt
				ok = true
			}
		}
		cancel()

		if ok {
			resultsMu.Lock()
			results[entry.addr] = uint64(bestRTT.Nanoseconds())
			resultsMu.Unlock()

			p.log.Debug("Probe succeeded", "probe", entry.addr.String(), "rtt", bestRTT)
		} else {
			p.log.Debug("Probe failed", "probe", entry.addr.String())
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

// MeasureOne measures a single probe and returns the best RTT in nanoseconds.
func (p *Pinger) MeasureOne(ctx context.Context, addr ProbeAddress) (uint64, bool) {
	p.sendersMu.Lock()
	entry, exists := p.senders[addr.String()]
	p.sendersMu.Unlock()
	if !exists {
		p.log.Warn("MeasureOne called for unknown probe", "probe", addr.String())
		return 0, false
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.cfg.ProbeTimeout)
	defer cancel()

	type probeResult struct {
		rtt time.Duration
		err error
	}
	ch := make(chan probeResult, 2)
	go func() {
		rtt, err := entry.warmupSender.Probe(probeCtx)
		ch <- probeResult{rtt, err}
	}()
	go func() {
		time.Sleep(DefaultWarmupDelay)
		rtt, err := entry.sender.Probe(probeCtx)
		ch <- probeResult{rtt, err}
	}()

	var bestRTT time.Duration
	ok := false
	for range 2 {
		r := <-ch
		if r.err == nil && (!ok || r.rtt < bestRTT) {
			bestRTT = r.rtt
			ok = true
		}
	}

	if ok {
		p.log.Debug("MeasureOne succeeded", "probe", addr.String(), "rtt", bestRTT)
	} else {
		p.log.Debug("MeasureOne failed", "probe", addr.String())
	}

	if !ok {
		return 0, false
	}
	return uint64(bestRTT.Nanoseconds()), true
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
		if err := entry.warmupSender.Close(); err != nil {
			p.log.Warn("Failed to close warmup sender", "probe", key, "error", err)
			lastErr = err
		}
	}

	p.senders = make(map[string]*senderEntry)
	p.log.Info("Closed all probes")

	return lastErr
}
