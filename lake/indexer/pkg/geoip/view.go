package geoip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type ViewConfig struct {
	Logger              *slog.Logger
	Clock               clockwork.Clock
	GeoIPStore          *Store
	GeoIPResolver       geoip.Resolver
	ServiceabilityStore *dzsvc.Store
	SolanaStore         *sol.Store
	RefreshInterval     time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.GeoIPStore == nil {
		return errors.New("geoip store is required")
	}
	if cfg.GeoIPResolver == nil {
		return errors.New("geoip resolver is required")
	}
	if cfg.ServiceabilityStore == nil {
		return errors.New("serviceability store is required")
	}
	if cfg.SolanaStore == nil {
		return errors.New("solana store is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	refreshMu sync.Mutex // prevents concurrent refreshes

	fetchedAt time.Time
	readyOnce sync.Once
	readyCh   chan struct{}
}

func NewView(cfg ViewConfig) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		readyCh: make(chan struct{}),
	}

	return v, nil
}

// Ready returns true if the view has completed at least one successful refresh
func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

// WaitReady waits for the view to be ready (has completed at least one successful refresh)
// It returns immediately if already ready, or blocks until ready or context is cancelled.
func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for geoip view: %w", ctx.Err())
	}
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("geoip: starting refresh loop", "interval", v.cfg.RefreshInterval)

		v.safeRefresh(ctx)

		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				v.safeRefresh(ctx)
			}
		}
	}()
}

// safeRefresh wraps Refresh with panic recovery to prevent the refresh loop from dying
func (v *View) safeRefresh(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			v.log.Error("geoip: refresh panicked", "panic", r)
			metrics.ViewRefreshTotal.WithLabelValues("geoip", "panic").Inc()
		}
	}()

	if err := v.Refresh(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		v.log.Error("geoip: refresh failed", "error", err)
	}
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Debug("geoip: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("geoip: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("geoip").Observe(duration.Seconds())
	}()

	v.log.Debug("geoip: querying IPs from serviceability and solana stores")

	// Collect unique IPs from both sources
	ipSet := make(map[string]net.IP)

	// Get IPs from serviceability users
	users, err := dzsvc.QueryCurrentUsers(ctx, v.log, v.cfg.ServiceabilityStore.GetClickHouse())
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("geoip", "error").Inc()
		return fmt.Errorf("failed to get users: %w", err)
	}
	for _, user := range users {
		if user.ClientIP != nil {
			ipSet[user.ClientIP.String()] = user.ClientIP
		}
	}

	// Get IPs from solana gossip nodes
	gossipIPs, err := v.cfg.SolanaStore.GetGossipIPs(ctx)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("geoip", "error").Inc()
		return fmt.Errorf("failed to get gossip IPs: %w", err)
	}
	for _, ip := range gossipIPs {
		if ip != nil {
			ipSet[ip.String()] = ip
		}
	}

	v.log.Debug("geoip: found unique IPs", "count", len(ipSet))

	// Resolve IPs and collect records
	geoipRecords := make([]*geoip.Record, 0, len(ipSet))
	for _, ip := range ipSet {
		record := v.cfg.GeoIPResolver.Resolve(ip)
		if record == nil {
			continue
		}
		geoipRecords = append(geoipRecords, record)
	}

	v.log.Debug("geoip: resolved records", "count", len(geoipRecords))

	// Upsert records
	if err := v.cfg.GeoIPStore.UpsertRecords(ctx, geoipRecords); err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("geoip", "error").Inc()
		return fmt.Errorf("failed to update geoip records: %w", err)
	}

	v.fetchedAt = time.Now().UTC()
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("geoip: view is now ready")
	})

	v.log.Debug("geoip: refresh completed", "fetched_at", v.fetchedAt)
	metrics.ViewRefreshTotal.WithLabelValues("geoip", "success").Inc()
	return nil
}
