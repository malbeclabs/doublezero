package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/tools/global-monitor/internal/solana"
)

type Config struct {
	Logger *slog.Logger
	Solana *solana.MonitorConfig
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Solana == nil {
		return errors.New("solana monitor config is required")
	}
	return nil
}

type Agent struct {
	log *slog.Logger
	cfg *Config

	solana *solana.Monitor
}

func NewAgent(cfg *Config) (*Agent, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	solanaMonitor, err := solana.NewMonitor(cfg.Solana)
	if err != nil {
		return nil, fmt.Errorf("failed to create solana monitor: %w", err)
	}

	return &Agent{
		log:    cfg.Logger,
		cfg:    cfg,
		solana: solanaMonitor,
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	go a.solana.Run(ctx)

	<-ctx.Done()
	a.log.Debug("agent context done, stopping")

	return nil
}
