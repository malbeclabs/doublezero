package state

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	stateingest "github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/client"
)

type CollectorConfig struct {
	Logger *slog.Logger

	EAPI        aristapb.EapiMgrServiceClient
	StateIngest *stateingest.Client

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
	if c.StateIngest == nil {
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
	command := "show snmp mib ifmib ifindex"
	if err := c.collectStateSnapshot(ctx, command); err != nil {
		c.log.Error("state: failed to collect snapshot", "command", command, "error", err)
	}
	return nil
}

func (c *Collector) collectStateSnapshot(ctx context.Context, command string) error {
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
	c.log.Info("state: uploading snapshot", "kind", kind, "command", command, "response", string(data), "dataSize", len(data))

	if _, err := c.cfg.StateIngest.UploadSnapshot(ctx, kind, time.Now().UTC(), data); err != nil {
		return fmt.Errorf("failed to upload state snapshot for command %q: %w", command, err)
	}

	return nil
}

func sanitizeCommandAsKind(command string) string {
	command = strings.TrimPrefix(command, "show ")
	return strings.ReplaceAll(strings.ToLower(command), " ", "-")
}
