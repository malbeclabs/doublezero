// isis-syncer periodically syncs ISIS topology data from S3 to memvid memory.
//
// It runs as a sidecar service that:
//  1. On startup: immediately syncs ISIS data from S3 to memvid
//  2. Then every SYNC_INTERVAL (default: 5h) via ticker
//
// Environment variables:
//   - MEMVID_BRAIN_PATH: path to .mv2 file (required)
//   - SYNC_INTERVAL: duration string like "5h" (default: 5h)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/tools"
	"github.com/malbeclabs/doublezero/lake/pkg/isis"
	"github.com/malbeclabs/doublezero/lake/pkg/logger"
)

const (
	defaultSyncInterval = 5 * time.Hour
	defaultMemvidBinary = "memvid"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Config holds the syncer configuration from environment variables.
type Config struct {
	MemvidBrainPath string
	SyncInterval    time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	log := logger.New(true) // verbose logging for sidecar

	log.Info("isis-syncer starting",
		"version", version,
		"commit", commit,
		"built", date,
	)

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	log.Info("configuration loaded",
		"brain_path", cfg.MemvidBrainPath,
		"sync_interval", cfg.SyncInterval.String(),
	)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info("received signal, shutting down", "signal", sig.String())
		cancel()
	}()

	// Create syncer
	syncer := NewSyncer(cfg, log)

	// Run sync loop
	return syncer.Run(ctx)
}

func loadConfig() (*Config, error) {
	brainPath := os.Getenv("MEMVID_BRAIN_PATH")
	if brainPath == "" {
		return nil, fmt.Errorf("MEMVID_BRAIN_PATH environment variable is required")
	}

	syncInterval := defaultSyncInterval
	if intervalStr := os.Getenv("SYNC_INTERVAL"); intervalStr != "" {
		var err error
		syncInterval, err = time.ParseDuration(intervalStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SYNC_INTERVAL %q: %w", intervalStr, err)
		}
		if syncInterval <= 0 {
			return nil, fmt.Errorf("SYNC_INTERVAL must be positive, got %s", syncInterval)
		}
	}

	return &Config{
		MemvidBrainPath: brainPath,
		SyncInterval:    syncInterval,
	}, nil
}

// Syncer handles periodic ISIS data sync to memvid.
type Syncer struct {
	config *Config
	log    *slog.Logger

	fetcher  *isis.S3Fetcher
	enricher *isis.Enricher
	memvid   *tools.MemvidToolClient
}

// NewSyncer creates a new Syncer with the given configuration.
func NewSyncer(cfg *Config, log *slog.Logger) *Syncer {
	fetcher := isis.NewS3Fetcher(isis.S3FetcherConfig{})

	enricher, err := isis.NewEnricher(isis.EnricherConfig{Level: 2})
	if err != nil {
		// This should not happen with default config, but log and continue
		log.Error("failed to create enricher, syncs will fail", "error", err)
	}

	memvid := tools.NewMemvidToolClient(tools.MemvidConfig{
		BinaryPath: defaultMemvidBinary,
		BrainPath:  cfg.MemvidBrainPath,
		Timeout:    2 * time.Minute, // longer timeout for large saves
	})

	return &Syncer{
		config:   cfg,
		log:      log,
		fetcher:  fetcher,
		enricher: enricher,
		memvid:   memvid,
	}
}

// Run starts the sync loop, performing an immediate sync then periodic syncs.
func (s *Syncer) Run(ctx context.Context) error {
	// Immediate sync on startup
	s.log.Info("performing initial sync")
	if err := s.sync(ctx); err != nil {
		s.log.Error("initial sync failed", "error", err)
		// Continue to ticker - don't crash
	} else {
		s.log.Info("initial sync completed successfully")
	}

	// Periodic sync
	ticker := time.NewTicker(s.config.SyncInterval)
	defer ticker.Stop()

	s.log.Info("starting periodic sync loop", "interval", s.config.SyncInterval.String())

	for {
		select {
		case <-ctx.Done():
			s.log.Info("sync loop stopped")
			return nil
		case <-ticker.C:
			s.log.Info("performing scheduled sync")
			if err := s.sync(ctx); err != nil {
				s.log.Error("scheduled sync failed", "error", err)
				// Continue to next interval - don't crash
			} else {
				s.log.Info("scheduled sync completed successfully")
			}
		}
	}
}

// sync fetches ISIS data from S3, enriches it, and saves to memvid.
func (s *Syncer) sync(ctx context.Context) error {
	if s.enricher == nil {
		return fmt.Errorf("enricher not initialized")
	}

	// Fetch latest from S3
	s.log.Debug("fetching latest ISIS data from S3")
	fetchResult, err := s.fetcher.FetchLatest(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch from S3: %w", err)
	}
	defer fetchResult.Body.Close()

	s.log.Debug("fetched ISIS data", "key", fetchResult.Key, "timestamp", fetchResult.Timestamp)

	// Enrich the data
	s.log.Debug("enriching ISIS data")
	result, err := s.enricher.EnrichFromReader(ctx, fetchResult.Body, fetchResult.Timestamp)
	if err != nil {
		return fmt.Errorf("failed to enrich: %w", err)
	}

	s.log.Debug("enrichment complete",
		"routers", result.Stats.TotalRouters,
		"links", result.Stats.TotalLinks,
		"sr_enabled", result.Stats.SREnabledRouters,
	)

	// Save to memvid
	now := time.Now().UTC()
	title := fmt.Sprintf("ISIS Network Topology %s", now.Format(time.RFC3339))
	snapshotTag := fmt.Sprintf("snapshot:%s", now.Format("2006-01-02"))

	s.log.Debug("saving to memvid",
		"title", title,
		"brain_path", s.config.MemvidBrainPath,
	)

	output, isErr, err := s.memvid.CallToolText(ctx, "memory_save", map[string]any{
		"content": result.Markdown,
		"title":   title,
		"tags":    []any{"isis", "topology", snapshotTag},
	})
	if err != nil {
		return fmt.Errorf("memvid call failed: %w", err)
	}
	if isErr {
		return fmt.Errorf("memvid returned error: %s", output)
	}

	s.log.Info("sync completed",
		"timestamp", now.Format(time.RFC3339),
		"routers", result.Stats.TotalRouters,
		"links", result.Stats.TotalLinks,
		"title", title,
		"output", truncateForLog(output, 200),
	)

	return nil
}

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
