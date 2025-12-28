package server

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/indexer"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultShutdownTimeout   = 5 * time.Second
)

type Config struct {
	Logger *slog.Logger

	Indexer *indexer.Indexer
	Querier *querier.Querier

	DeviceUsageEnabled bool
	Version            string
	ListenAddr         string
	ReadHeaderTimeout  time.Duration
	ShutdownTimeout    time.Duration
	AllowedTokens      []string // Bearer tokens allowed for MCP endpoint authentication
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.Querier == nil {
		return fmt.Errorf("querier is required")
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	return nil
}
