package geoprobe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type ServiceabilityClient interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type RPCClientInterface interface {
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
}

type CoordinatorConfig struct {
	Logger               *slog.Logger
	InitialProbes        []ProbeAddress
	ProbeUpdateCh        chan []ProbeAddress
	Interval             time.Duration
	ProbeTimeout         time.Duration
	Keypair              solana.PrivateKey
	LocalDevicePK        solana.PublicKey
	ServiceabilityClient ServiceabilityClient
	RPCClient            RPCClientInterface
	ManagementNamespace  string
}

type Coordinator struct {
	log       *slog.Logger
	cfg       *CoordinatorConfig
	pinger    *Pinger
	publisher *Publisher
	probes    map[string]ProbeAddress
	probesMu  sync.RWMutex
}

func NewCoordinator(cfg *CoordinatorConfig) (*Coordinator, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("logger is required")
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("interval must be greater than 0")
	}
	if cfg.ProbeTimeout <= 0 {
		return nil, errors.New("probe timeout must be greater than 0")
	}
	if cfg.Keypair == nil {
		return nil, errors.New("keypair is required")
	}
	if cfg.LocalDevicePK.IsZero() {
		return nil, errors.New("local device pubkey is required")
	}
	if cfg.ServiceabilityClient == nil {
		return nil, errors.New("serviceability client is required")
	}
	if cfg.RPCClient == nil {
		return nil, errors.New("rpc client is required")
	}

	c := &Coordinator{
		log:    cfg.Logger,
		cfg:    cfg,
		probes: make(map[string]ProbeAddress),
	}

	pingerCfg := &PingerConfig{
		Logger:              cfg.Logger,
		ProbeTimeout:        cfg.ProbeTimeout,
		Interval:            cfg.Interval,
		ManagementNamespace: cfg.ManagementNamespace,
	}
	c.pinger = NewPinger(pingerCfg)

	publisherCfg := &PublisherConfig{
		Logger:               cfg.Logger,
		Keypair:              cfg.Keypair,
		LocalDevicePK:        cfg.LocalDevicePK,
		ServiceabilityClient: cfg.ServiceabilityClient,
		RPCClient:            cfg.RPCClient,
		ManagementNamespace:  cfg.ManagementNamespace,
	}
	var err error
	c.publisher, err = NewPublisher(publisherCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	ctx := context.Background()
	for _, addr := range cfg.InitialProbes {
		c.probes[addr.String()] = addr
		if err := c.pinger.AddProbe(ctx, addr); err != nil {
			delete(c.probes, addr.String())
			c.log.Warn("Failed to add initial probe to pinger", "addr", addr, "error", err)
			continue
		}
		if err := c.publisher.AddProbe(ctx, addr); err != nil {
			c.log.Warn("Failed to add initial probe to publisher", "addr", addr, "error", err)
			continue
		}
		c.log.Info("Added initial geoprobe", "addr", addr)
	}

	return c, nil
}

func (c *Coordinator) Run(ctx context.Context) error {
	c.log.Info("Starting geoprobe coordinator",
		"interval", c.cfg.Interval,
		"probeCount", len(c.probes),
	)

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Geoprobe coordinator shutting down")
			return nil

		case newProbes, ok := <-c.cfg.ProbeUpdateCh:
			if !ok {
				c.log.Warn("Probe update channel closed")
				c.cfg.ProbeUpdateCh = nil
				continue
			}
			c.handleProbeUpdate(ctx, newProbes)

		case <-ticker.C:
			c.runMeasurementCycle(ctx)
		}
	}
}

func (c *Coordinator) Close() error {
	c.log.Info("Closing geoprobe coordinator")

	if err := c.pinger.Close(); err != nil {
		c.log.Warn("Failed to close pinger", "error", err)
	}

	if err := c.publisher.Close(); err != nil {
		c.log.Warn("Failed to close publisher", "error", err)
	}

	c.log.Info("Geoprobe coordinator closed")
	return nil
}

func (c *Coordinator) handleProbeUpdate(ctx context.Context, newProbes []ProbeAddress) {
	c.probesMu.Lock()
	defer c.probesMu.Unlock()

	newProbesMap := make(map[string]ProbeAddress)
	for _, addr := range newProbes {
		newProbesMap[addr.String()] = addr
	}

	for key, addr := range newProbesMap {
		if _, exists := c.probes[key]; !exists {
			c.probes[key] = addr
			if err := c.pinger.AddProbe(ctx, addr); err != nil {
				delete(c.probes, key)
				c.log.Warn("Failed to add probe to pinger", "addr", addr, "error", err)
				continue
			}
			if err := c.publisher.AddProbe(ctx, addr); err != nil {
				delete(c.probes, key)
				if removeErr := c.pinger.RemoveProbe(addr); removeErr != nil {
					c.log.Warn("Failed to remove probe from pinger during cleanup", "addr", addr, "error", removeErr)
				}
				c.log.Warn("Failed to add probe to publisher", "addr", addr, "error", err)
				continue
			}
			c.log.Info("Added geoprobe", "addr", addr)
		}
	}

	for key, addr := range c.probes {
		if _, exists := newProbesMap[key]; !exists {
			delete(c.probes, key)
			if err := c.pinger.RemoveProbe(addr); err != nil {
				c.log.Warn("Failed to remove probe from pinger", "addr", addr, "error", err)
			}
			if err := c.publisher.RemoveProbe(addr); err != nil {
				c.log.Warn("Failed to remove probe from publisher", "addr", addr, "error", err)
			}
			c.log.Info("Removed geoprobe", "addr", addr)
		}
	}
}

func (c *Coordinator) runMeasurementCycle(ctx context.Context) {
	c.probesMu.RLock()
	probeCount := len(c.probes)
	c.probesMu.RUnlock()

	c.log.Debug("Starting geoprobe measurement cycle", "probeCount", probeCount)

	if probeCount == 0 {
		c.log.Debug("No probes to measure, skipping cycle")
		return
	}

	rttData, err := c.pinger.MeasureAll(ctx)
	if err != nil {
		c.log.Error("Failed to measure probes", "error", err)
		return
	}

	if len(rttData) == 0 {
		c.log.Warn("No successful measurements in cycle")
		return
	}

	if err := c.publisher.Publish(ctx, rttData); err != nil {
		c.log.Error("Failed to publish offsets", "error", err)
		return
	}

	c.log.Debug("Completed geoprobe measurement cycle",
		"probesCount", len(rttData),
		"totalProbes", probeCount,
	)
}
