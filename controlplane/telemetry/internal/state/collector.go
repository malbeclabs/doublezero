package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
)

var (
	defaultCommands = []string{
		"show snmp mib ifmib ifindex",
		"show isis database detail",
	}
	defaultConcurrency = 16
)

type StateIngestClient interface {
	UploadSnapshot(ctx context.Context, kind string, timestamp time.Time, data []byte) (string, error)
}

type CollectorConfig struct {
	Logger *slog.Logger
	Clock  clockwork.Clock

	EAPI        aristapb.EapiMgrServiceClient
	StateIngest StateIngestClient

	Interval time.Duration
	DevicePK solana.PublicKey

	Commands    []string
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
	if len(c.Commands) == 0 {
		c.Commands = defaultCommands
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
		"commands", c.cfg.Commands,
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
	var wg sync.WaitGroup
	sem := make(chan struct{}, c.cfg.Concurrency)

	for _, command := range c.cfg.Commands {
		wg.Add(1)
		go func(command string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := c.collectStateSnapshot(ctx, command); err != nil {
				c.log.Error("state: failed to collect snapshot", "command", command, "error", err)
			}
		}(command)
	}

	wg.Wait()

	return nil
}

func (c *Collector) collectStateSnapshot(ctx context.Context, command string) error {
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
	kind := sanitizeCommandAsKind(command)

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

func sanitizeCommandAsKind(command string) string {
	command = strings.TrimPrefix(command, "show ")
	return strings.ReplaceAll(strings.ToLower(command), " ", "-")
}
