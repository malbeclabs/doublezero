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

	// Bind-error retry budget; kept small because socket creation is now on the
	// probe hot path and the transient EINVAL it guards clears quickly.
	senderRetries  = 3
	senderRetryMin = 20 * time.Millisecond

	// maxPairBindBackoff is the backoff a successful createSenderPair incurs in
	// the worst case: both sockets retry senderRetries-1 times before succeeding
	// (senderRetryMin*(2^0+...+2^(senderRetries-2)) per socket, two sockets).
	maxPairBindBackoff = 2 * senderRetryMin * ((1 << (senderRetries - 1)) - 1)

	// slowSenderPairThreshold sits just below maxPairBindBackoff so the warning
	// fires only on near-worst-case bind contention; the goal is to learn whether
	// we hit that ceiling often, not to flag every retry.
	slowSenderPairThreshold = maxPairBindBackoff - senderRetryMin
)

type senderFactory func(ctx context.Context, log *slog.Logger, iface string, local, remote *net.UDPAddr) (twamplight.Sender, error)

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
	targets   map[string]ProbeAddress
	targetsMu sync.Mutex
	newSender senderFactory
}

func NewPinger(cfg *PingerConfig) *Pinger {
	return &Pinger{
		log:       cfg.Logger,
		cfg:       cfg,
		targets:   make(map[string]ProbeAddress),
		newSender: newSenderWithRetry,
	}
}

func newSenderWithRetry(ctx context.Context, log *slog.Logger, iface string, local, remote *net.UDPAddr) (twamplight.Sender, error) {
	return retryOnBindError(ctx, log, func() (twamplight.Sender, error) {
		return twamplight.NewSender(ctx, log, iface, local, remote)
	})
}

func (p *Pinger) AddProbe(_ context.Context, addr ProbeAddress) error {
	p.targetsMu.Lock()
	defer p.targetsMu.Unlock()

	key := addr.String()
	if _, exists := p.targets[key]; exists {
		p.log.Debug("Probe already exists", "probe", key)
		return nil
	}

	if err := addr.Validate(); err != nil {
		return fmt.Errorf("invalid probe address %s: %w", key, err)
	}

	p.targets[key] = addr
	p.log.Info("Added probe", "probe", key)
	return nil
}

func (p *Pinger) RemoveProbe(addr ProbeAddress) error {
	p.targetsMu.Lock()
	defer p.targetsMu.Unlock()

	key := addr.String()
	if _, exists := p.targets[key]; !exists {
		p.log.Warn("Probe not found for removal", "probe", key)
		return nil
	}

	delete(p.targets, key)
	p.log.Info("Removed probe", "probe", key)
	return nil
}

func (p *Pinger) createSenderPair(ctx context.Context, addr ProbeAddress) (sender, warmup twamplight.Sender, err error) {
	resolvedAddr := &net.UDPAddr{IP: net.ParseIP(addr.Host), Port: int(addr.TWAMPPort)}
	iface := p.cfg.ManagementNamespace

	start := time.Now()

	sender, err = p.newSender(ctx, p.log, iface, &net.UDPAddr{IP: net.IPv4zero, Port: 0}, resolvedAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("create sender for %s: %w", addr.String(), err)
	}

	warmup, err = p.newSender(ctx, p.log, iface, &net.UDPAddr{IP: net.IPv4zero, Port: 0}, resolvedAddr)
	if err != nil {
		sender.Close()
		return nil, nil, fmt.Errorf("create warmup sender for %s: %w", addr.String(), err)
	}

	if elapsed := time.Since(start); elapsed > slowSenderPairThreshold {
		p.log.Warn("Slow sender pair creation",
			"probe", addr.String(),
			"elapsed", elapsed,
			"threshold", slowSenderPairThreshold)
	}

	return sender, warmup, nil
}

func (p *Pinger) probeTarget(ctx context.Context, addr ProbeAddress) (time.Duration, bool) {
	sender, warmup, err := p.createSenderPair(ctx, addr)
	if err != nil {
		p.log.Warn("Failed to create senders", "probe", addr.String(), "error", err)
		return 0, false
	}
	defer sender.Close()
	defer warmup.Close()

	probeCtx, cancel := context.WithTimeout(ctx, p.cfg.ProbeTimeout)
	defer cancel()

	// Send a warmup probe first to wake the reflector's thread, then send the
	// measurement probe after a short delay. Both run on separate sockets so
	// neither blocks the other; we take the min RTT. Note the naming: warmup
	// fires immediately, while sender is the delayed measurement probe.
	type probeResult struct {
		rtt time.Duration
		err error
	}
	ch := make(chan probeResult, 2)
	go func() {
		rtt, err := warmup.Probe(probeCtx)
		ch <- probeResult{rtt, err}
	}()
	go func() {
		time.Sleep(DefaultWarmupDelay)
		rtt, err := sender.Probe(probeCtx)
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

	return bestRTT, ok
}

func (p *Pinger) probeWorker(
	ctx context.Context,
	batch []ProbeAddress,
	staggerDelay time.Duration,
	results map[ProbeAddress]uint64,
	resultsMu *sync.Mutex,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for i, addr := range batch {
		select {
		case <-ctx.Done():
			return
		default:
		}

		rtt, ok := p.probeTarget(ctx, addr)

		if ok {
			resultsMu.Lock()
			results[addr] = uint64(rtt.Nanoseconds())
			resultsMu.Unlock()
			p.log.Debug("Probe succeeded", "probe", addr.String(), "rtt", rtt)
		} else {
			p.log.Debug("Probe failed", "probe", addr.String())
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
	p.targetsMu.Lock()
	_, exists := p.targets[addr.String()]
	p.targetsMu.Unlock()
	if !exists {
		p.log.Warn("MeasureOne called for unknown probe", "probe", addr.String())
		return 0, false
	}

	rtt, ok := p.probeTarget(ctx, addr)
	if ok {
		p.log.Debug("MeasureOne succeeded", "probe", addr.String(), "rtt", rtt)
		return uint64(rtt.Nanoseconds()), true
	}
	p.log.Debug("MeasureOne failed", "probe", addr.String())
	return 0, false
}

func (p *Pinger) MeasureAll(ctx context.Context) (map[ProbeAddress]uint64, error) {
	p.targetsMu.Lock()
	targetsCopy := make([]ProbeAddress, 0, len(p.targets))
	for _, addr := range p.targets {
		targetsCopy = append(targetsCopy, addr)
	}
	p.targetsMu.Unlock()

	if len(targetsCopy) == 0 {
		return make(map[ProbeAddress]uint64), nil
	}

	totalProbes := len(targetsCopy)
	results := make(map[ProbeAddress]uint64)
	resultsMu := sync.Mutex{}
	var wg sync.WaitGroup

	numWorkers := min(MaxWorkers, (len(targetsCopy)+ProbesPerWorker-1)/ProbesPerWorker)
	if numWorkers == 0 {
		numWorkers = 1
	}
	batchSize := (len(targetsCopy) + numWorkers - 1) / numWorkers

	staggerDelay := p.cfg.StaggerDelay
	if staggerDelay == 0 {
		staggerDelay = DefaultStaggerDelay
	}

	for i := 0; i < numWorkers; i++ {
		start := i * batchSize
		end := min(start+batchSize, len(targetsCopy))
		if start >= len(targetsCopy) {
			break
		}

		batch := targetsCopy[start:end]
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
	p.targetsMu.Lock()
	defer p.targetsMu.Unlock()

	p.targets = make(map[string]ProbeAddress)
	p.log.Info("Closed all probes")
	return nil
}
