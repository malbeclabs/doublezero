package server

import (
	"errors"
	"time"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer"
)

type Config struct {
	ListenAddr        string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	IndexerConfig     indexer.Config
}

func (cfg *Config) Validate() error {
	if cfg.ListenAddr == "" {
		return errors.New("listen addr is required")
	}
	if err := cfg.IndexerConfig.Validate(); err != nil {
		return err
	}
	return nil
}
