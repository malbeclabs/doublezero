package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	partitionBufferCapacity = 4096
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

	// Geoprobe coordinator for measuring RTT to child probes
	geoprobeCoordinator *geoprobe.Coordinator

	senders   map[string]*senderEntry
	sendersMu sync.Mutex

	buffer buffer.PartitionedBuffer[PartitionKey, Sample]
}

func New(log *slog.Logger, cfg Config) (*Collector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	buffer := buffer.NewMemoryPartitionedBuffer[PartitionKey, Sample](partitionBufferCapacity)

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
		MaxConcurrency:     cfg.SubmitterMaxConcurrency,
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

	// Initialize geoprobe coordinator if child probes are configured
	if len(cfg.InitialChildGeoProbes) > 0 {
		probeUpdateCh := make(chan []geoprobe.ProbeAddress, 1)
		c.geoprobeCoordinator, err = geoprobe.NewCoordinator(&geoprobe.CoordinatorConfig{
			Logger:               log,
			InitialProbes:        cfg.InitialChildGeoProbes,
			ProbeUpdateCh:        probeUpdateCh,
			Interval:             cfg.ProbeInterval,
			ProbeTimeout:         cfg.TWAMPSenderTimeout,
			Keypair:              cfg.Keypair,
			LocalDevicePK:        cfg.LocalDevicePK,
			ServiceabilityClient: cfg.ServiceabilityProgramClient,
			RPCClient:            cfg.RPCClient,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create geoprobe coordinator: %w", err)
		}
		log.Info("Initialized geoprobe coordinator", "probeCount", len(cfg.InitialChildGeoProbes))
	}

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
	wg.Add(1)
	go func() {
		defer wg.Done()
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

	// Start the geoprobe coordinator if configured.
	if c.geoprobeCoordinator != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.geoprobeCoordinator.Run(runCtx); err != nil {
				errCh <- fmt.Errorf("failed to run geoprobe coordinator: %w", err)
			}
		}()
	}

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

	if cerr := c.Close(); cerr != nil {
		c.log.Warn("Failed to close telemetry collector", "error", cerr)
	}

	return err
}

// Close gracefully shuts down the Collector by submitting remaining samples,
// stopping the TWAMP reflector, and closing all active TWAMP senders.
func (c *Collector) Close() error {
	c.log.Info("Closing telemetry collector")

	// Close the TWAMP reflector.
	if err := c.reflector.Close(); err != nil {
		c.log.Warn("Failed to close TWAMP reflector", "error", err)
	}

	// Close the geoprobe coordinator if initialized.
	if c.geoprobeCoordinator != nil {
		if err := c.geoprobeCoordinator.Close(); err != nil {
			c.log.Warn("Failed to close geoprobe coordinator", "error", err)
		}
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
	sender    twamplight.Sender
	lastUsed  time.Time
	createdAt time.Time
}

func (c *Collector) getOrCreateSender(ctx context.Context, peer *Peer) twamplight.Sender {
	key := peer.String()
	now := c.cfg.NowFunc()

	c.sendersMu.Lock()
	entry, ok := c.senders[key]
	if ok {
		entry.lastUsed = now
		ttl := c.cfg.SenderTTL
		if ttl > 0 && now.Sub(entry.createdAt) >= ttl {
			_ = entry.sender.Close()
			delete(c.senders, key)
		} else {
			s := entry.sender
			c.sendersMu.Unlock()
			return s
		}
	}
	c.sendersMu.Unlock()

	sourceAddr := &net.UDPAddr{IP: peer.Tunnel.SourceIP, Port: 0}
	targetAddr := &net.UDPAddr{IP: peer.Tunnel.TargetIP, Port: int(peer.TWAMPPort)}
	sender, err := twamplight.NewSender(ctx, c.log, peer.Tunnel.Interface, sourceAddr, targetAddr)
	if err != nil {
		c.log.Error("Failed to create sender", "error", err)
		return nil
	}

	c.sendersMu.Lock()
	c.senders[peer.String()] = &senderEntry{
		sender:    sender,
		lastUsed:  c.cfg.NowFunc(),
		createdAt: c.cfg.NowFunc(),
	}
	c.sendersMu.Unlock()

	return sender
}

func (c *Collector) cleanupIdleSenders(maxIdle time.Duration) {
	now := c.cfg.NowFunc()

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
