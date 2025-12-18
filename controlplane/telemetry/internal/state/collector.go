package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
)

var (
	defaultConcurrency = 16
)

type ShowCommand struct {
	Kind    string
	Command string
}

type StateIngestClient interface {
	UploadSnapshot(ctx context.Context, kind string, timestamp time.Time, data []byte) (string, error)
	GetStateToCollect(ctx context.Context) ([]ShowCommand, error)
}

type CollectorConfig struct {
	Logger *slog.Logger
	Clock  clockwork.Clock

	EAPI        aristapb.EapiMgrServiceClient
	StateIngest StateIngestClient

	Interval time.Duration
	DevicePK solana.PublicKey

	Concurrency int
}

func (c *CollectorConfig) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	if c.EAPI == nil {
		return fmt.Errorf("eapi is required")
	}
	if c.StateIngest == nil {
		return fmt.Errorf("ingest is required")
	}
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	if c.DevicePK.IsZero() {
		return fmt.Errorf("device pk is required")
	}
	if c.Concurrency <= 0 {
		c.Concurrency = defaultConcurrency
	}
	return nil
}

type Collector struct {
	log *slog.Logger
	cfg *CollectorConfig
}

func NewCollector(cfg *CollectorConfig) (*Collector, error) {
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
	c.log.Info("state: collector started",
		"concurrency", c.cfg.Concurrency,
		"interval", c.cfg.Interval,
		"device", c.cfg.DevicePK.String(),
	)

	ticker := c.cfg.Clock.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	if err := c.tick(ctx); err != nil {
		c.log.Error("state: collector failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
			if err := c.tick(ctx); err != nil {
				c.log.Error("state: collector failed", "error", err)
			}
		}
	}
}

func (c *Collector) tick(ctx context.Context) error {
	showCommands, err := c.cfg.StateIngest.GetStateToCollect(ctx)
	if err != nil {
		c.log.Warn("state: failed to fetch commands from server, skipping collection", "error", err)
		return nil
	}

	if len(showCommands) == 0 {
		c.log.Debug("state: no commands to collect from server")
		return nil
	}

	c.log.Debug("state: fetched commands from server", "count", len(showCommands))

	var wg sync.WaitGroup
	sem := make(chan struct{}, c.cfg.Concurrency)

	for _, sc := range showCommands {
		wg.Add(1)
		go func(kind, command string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := c.collectStateSnapshot(ctx, kind, command); err != nil {
				c.log.Error("state: failed to collect snapshot", "kind", kind, "command", command, "error", err)
			}
		}(sc.Kind, sc.Command)
	}

	wg.Wait()

	return nil
}

func (c *Collector) collectStateSnapshot(ctx context.Context, kind, command string) error {
	now := c.cfg.Clock.Now().UTC()
	response, err := c.cfg.EAPI.RunShowCmd(ctx, &aristapb.RunShowCmdRequest{
		Command: command,
	})
	if err != nil {
		return fmt.Errorf("failed to execute command %q: %w", command, err)
	}
	if response.Response == nil {
		return fmt.Errorf("no response from arista eapi for command %q", command)
	}
	if !response.Response.Success {
		return fmt.Errorf("error from arista eapi for command %q: code=%d, message=%s", command, response.Response.ErrorCode, response.Response.ErrorMessage)
	}
	if len(response.Response.Responses) == 0 {
		return fmt.Errorf("no responses from arista eapi for command %q", command)
	}
	data := []byte(response.Response.Responses[0])

	snap := StateSnapshot{
		Metadata: StateSnapshotMetadata{
			Kind:      kind,
			Timestamp: now.Format(time.RFC3339),
			Device:    c.cfg.DevicePK.String(),
		},
		Data: json.RawMessage(data),
	}

	c.log.Debug("state: uploading snapshot", "kind", kind, "command", command, "dataSize", len(data))

	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("failed to marshal state snapshot: %w", err)
	}

	if _, err := c.cfg.StateIngest.UploadSnapshot(ctx, kind, now, snapJSON); err != nil {
		return fmt.Errorf("failed to upload state snapshot for command %q: %w", command, err)
	}

	return nil
}
