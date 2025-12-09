package state

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/agent/pkg/arista"
	"github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/ingest"
)

type CollectorConfig struct {
	Logger *slog.Logger

	EAPI   *arista.EAPIClient
	Ingest *ingest.Client

	Interval time.Duration
	DevicePK solana.PublicKey
}

func (c *CollectorConfig) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.EAPI == nil {
		return fmt.Errorf("eapi is required")
	}
	if c.Ingest == nil {
		return fmt.Errorf("ingest is required")
	}
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	if c.DevicePK.IsZero() {
		return fmt.Errorf("device pk is required")
	}
	return nil
}

type Collector struct {
	log *slog.Logger
	cfg CollectorConfig
}

func NewCollector(cfg CollectorConfig) (*Collector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Collector{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

func (c *Collector) Start(ctx context.Context, cancel context.CancelFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer cancel()
		if err := c.Run(ctx); err != nil {
			c.log.Error("state: collector failed", "error", err)
			errCh <- err
			cancel()
		}
	}()
	return errCh
}

func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	if err := c.tick(ctx); err != nil {
		c.log.Error("state: collector failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.tick(ctx); err != nil {
				c.log.Error("state: collector failed", "error", err)
			}
		}
	}
}

func (c *Collector) tick(ctx context.Context) error {
	resp, command, err := c.cfg.EAPI.ShowSnmpMibIfmibIfindex(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ifindex: %w", err)
	}

	kind := "snmp-mib-ifmib-ifindex"
	c.log.Info("state: running command", "kind", kind, "command", string(command), "ifindex", resp.IfIndex)
	for ifName, ifIndex := range resp.IfIndex {
		c.log.Info("state: ifindex", "ifname", ifName, "ifindex", ifIndex)
	}
	err = c.cfg.Ingest.Push(ctx, kind, ingest.PushRequest{
		Metadata: ingest.Metadata{
			SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
			Command:           string(command),
			DevicePubkey:      c.cfg.DevicePK,
		},
		Data: resp.IfIndex,
	})
	if err != nil {
		return fmt.Errorf("failed to push to ingest: %w", err)
	}

	return nil
}
