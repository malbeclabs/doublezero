package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

// Collector orchestrates telemetry collection by coordinating the TWAMP reflector,
// peer discovery, periodic probing (via Pinger), and sample submission (via Submitter).
// It owns shared resources such as the sender pool and sample buffer.
type Collector struct {
	log *slog.Logger
	cfg Config

	peers     PeerDiscovery
	reflector twamplight.Reflector
	pinger    *Pinger
	submitter *Submitter

	senders   map[string]*senderEntry
	sendersMu sync.Mutex

	buffer *AccountsBuffer
}

func New(log *slog.Logger, cfg Config) (*Collector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	buffer := NewAccountsBuffer()

	c := &Collector{
		log:       log,
		cfg:       cfg,
		peers:     cfg.PeerDiscovery,
		reflector: cfg.TWAMPReflector,
		senders:   make(map[string]*senderEntry),
		buffer:    buffer,
	}

	var err error
	c.submitter, err = NewSubmitter(log, &SubmitterConfig{
		Interval:           cfg.SubmissionInterval,
		Buffer:             buffer,
		MetricsPublisherPK: cfg.MetricsPublisherPK,
		ProbeInterval:      cfg.ProbeInterval,
		ProgramClient:      cfg.TelemetryProgramClient,
		GetCurrentEpoch:    cfg.GetCurrentEpochFunc,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create submitter: %w", err)
	}

	c.pinger = NewPinger(log, &PingerConfig{
		LocalDevicePK:   cfg.LocalDevicePK,
		Interval:        cfg.ProbeInterval,
		ProbeTimeout:    cfg.TWAMPSenderTimeout,
		Peers:           cfg.PeerDiscovery,
		Buffer:          buffer,
		GetSender:       c.getOrCreateSender,
		GetCurrentEpoch: cfg.GetCurrentEpochFunc,
	})

	return c, nil
}

// Run launches all telemetry components (reflector, peer discovery, pinger, submitter)
// and blocks until shutdown or an unrecoverable error occurs.
// Each component is started in its own goroutine with coordinated lifecycle management.
func (c *Collector) Run(ctx context.Context) error {
	c.log.Info("Starting telemetry collector",
		"twampReflector", c.reflector.LocalAddr(),
		"localDevicePK", c.cfg.LocalDevicePK,
		"probeInterval", c.cfg.ProbeInterval,
		"submissionInterval", c.cfg.SubmissionInterval,
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Manual errCh + WaitGroup instead of errgroup.Group:
	// better per-component logging, full shutdown coordination, and error classification.
	errCh := make(chan error, 8)
	var wg sync.WaitGroup

	// Start the TWAMP reflector in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.reflector.Run(runCtx); err != nil {
			errCh <- fmt.Errorf("failed to run TWAMP reflector: %w", err)
		}
	}()

	// Start the peer discovery component in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.peers.Run(runCtx); err != nil {
			errCh <- fmt.Errorf("failed to run peer discovery: %w", err)
		}
	}()

	// Start the pinger in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.pinger.Run(runCtx); err != nil {
			errCh <- fmt.Errorf("failed to run probe loop: %w", err)
		}
	}()

	// Start the submitter in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := c.submitter.Run(runCtx); err != nil {
			errCh <- fmt.Errorf("failed to run submission loop: %w", err)
		}
	}()

	// Start the sender cleanup loop in the background.
	go func() {
		t := time.NewTicker(1 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-t.C:
				c.cleanupIdleSenders(time.Minute * 5)
			}
		}
	}()

	// Wait for the context to be done or an error to be returned.
	var err error
	select {
	case <-ctx.Done():
	case e := <-errCh:
		c.log.Error("Telemetry collector shutting down due to error", "error", e)
		err = e
		cancel()
	}

	wg.Wait()

	if cerr := c.Close(runCtx); cerr != nil {
		c.log.Warn("Failed to close telemetry collector", "error", cerr)
	}

	return err
}

// Close gracefully shuts down the Collector by submitting remaining samples,
// stopping the TWAMP reflector, and closing all active TWAMP senders.
func (c *Collector) Close(ctx context.Context) error {
	c.log.Info("Closing telemetry collector")

	// Submit any buffered samples.
	for accountKey, samples := range c.buffer.FlushWithoutReset() {
		if len(samples) > 0 {
			c.log.Debug("Submitting remaining samples", "account", accountKey, "count", len(samples))
			for attempt := 1; attempt <= 2; attempt++ {
				err := c.submitter.SubmitSamples(ctx, accountKey, samples)
				if err == nil {
					break
				}
				metrics.Errors.WithLabelValues(metrics.ErrorTypeCollectorSubmitSamplesOnClose).Inc()
				c.log.Warn("Final sample submission failed", "attempt", attempt, "samples", len(samples), "error", err)
				sleepOrDone(ctx, time.Duration(attempt)*500*time.Millisecond)
			}
		}
	}

	// Close the TWAMP reflector.
	if err := c.reflector.Close(); err != nil {
		c.log.Warn("Failed to close TWAMP reflector", "error", err)
	}

	// Close the TWAMP senders.
	for _, entry := range c.senders {
		if err := entry.sender.Close(); err != nil {
			c.log.Warn("Failed to close TWAMP sender", "error", err)
		}
	}

	return nil
}

type senderEntry struct {
	sender   twamplight.Sender
	lastUsed time.Time
}

func (c *Collector) getOrCreateSender(ctx context.Context, peer *Peer) twamplight.Sender {
	c.sendersMu.Lock()
	defer c.sendersMu.Unlock()

	entry, ok := c.senders[peer.String()]
	if ok {
		entry.lastUsed = time.Now()
		return entry.sender
	}

	sourceAddr := &net.UDPAddr{IP: peer.Tunnel.SourceIP, Port: 0}
	targetAddr := &net.UDPAddr{IP: peer.Tunnel.TargetIP, Port: int(peer.TWAMPPort)}
	sender, err := twamplight.NewSender(ctx, c.log, peer.Tunnel.Interface, sourceAddr, targetAddr)
	if err != nil {
		c.log.Error("Failed to create sender", "error", err)
		return nil
	}
	c.senders[peer.String()] = &senderEntry{sender: sender, lastUsed: time.Now()}
	return sender
}

func (c *Collector) cleanupIdleSenders(maxIdle time.Duration) {
	now := time.Now()

	c.sendersMu.Lock()
	defer c.sendersMu.Unlock()

	for key, entry := range c.senders {
		if now.Sub(entry.lastUsed) > maxIdle {
			c.log.Debug("Evicting idle sender", "peer", key)
			_ = entry.sender.Close()
			delete(c.senders, key)
		}
	}
}
