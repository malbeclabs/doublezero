package server

import (
	"errors"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/indexer"
)

// VersionInfo contains build-time version information.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type Config struct {
	ListenAddr        string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	VersionInfo       VersionInfo
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
