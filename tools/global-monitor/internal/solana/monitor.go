package solana

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
)

const (
	defaultParallelism = 32
)

type MonitorConfig struct {
	Logger *slog.Logger
	Clock  clockwork.Clock

	GetValidatorsFunc func(ctx context.Context) (map[solana.PublicKey]*ValidatorView, error)

	Interface string

	Parallelism          uint32
	StatsInterval        time.Duration
	KeepAlivePeriod      time.Duration
	HandshakeIdleTimeout time.Duration

	// Health / window settings
	WindowSlots      int           // number of buckets in the window, e.g. 60
	WindowResolution time.Duration // per-bucket duration, e.g. 10 * time.Second
	EWMAAlpha        float64       // 0 < alpha <= 1, e.g. 0.2
}

func (c *MonitorConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.GetValidatorsFunc == nil {
		return errors.New("get validators func is required")
	}
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	if c.Parallelism == 0 {
		c.Parallelism = defaultParallelism
	}
	if c.Parallelism <= 0 {
		return errors.New("parallelism must be greater than 0")
	}
	if c.StatsInterval <= 0 {
		return errors.New("stats interval must be greater than 0")
	}
	if c.KeepAlivePeriod <= 0 {
		return errors.New("keep alive period must be greater than 0")
	}
	if c.HandshakeIdleTimeout <= 0 {
		return errors.New("handshake idle timeout must be greater than 0")
	}
	if c.WindowSlots <= 0 {
		c.WindowSlots = 60
	}
	if c.WindowResolution <= 0 {
		c.WindowResolution = 10 * time.Second
	}
	if c.EWMAAlpha <= 0 || c.EWMAAlpha > 1 {
		c.EWMAAlpha = 0.2
	}
	return nil
}

type Validator struct {
	Pubkey solana.PublicKey
	View   *ValidatorView

	FirstSeenAt time.Time
	LastSeenAt  time.Time

	conn *tpuquic.Conn
}

func (v *Validator) Dial(ctx context.Context, cfg *tpuquic.DialConfig) error {
	if v.View == nil || v.View.Node == nil || v.View.Node.TPUQUIC == nil {
		return fmt.Errorf("validator %s has no tpu quic address", v.Pubkey.String())
	}
	if v.conn != nil {
		_ = v.conn.Close()
		v.conn = nil
	}
	conn, err := tpuquic.Dial(ctx, *v.View.Node.TPUQUIC, cfg)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		v.conn = nil
		return err
	}
	v.conn = conn
	return nil
}

func (v *Validator) ConnectionStats() *quic.ConnectionStats {
	if v.conn == nil {
		return nil
	}
	stats := v.conn.ConnectionStats()
	return &stats
}

func (v *Validator) Close() error {
	if v.conn == nil {
		return nil
	}
	return v.conn.Close()
}

func (v *Validator) TPUQUICAddr() string {
	if v.View == nil || v.View.Node == nil || v.View.Node.TPUQUIC == nil {
		return ""
	}
	return *v.View.Node.TPUQUIC
}

func (v *Validator) IsConnected() bool {
	return v.conn != nil && !v.conn.IsClosed()
}

type Monitor struct {
	log *slog.Logger
	cfg *MonitorConfig

	mu sync.Mutex

	validators  map[solana.PublicKey]*Validator
	successes   map[solana.PublicKey]uint64
	failures    map[solana.PublicKey]uint64
	latestStats map[solana.PublicKey]quic.ConnectionStats
	lastSeenAt  map[solana.PublicKey]time.Time

	// Track a recent summary of each validator's health and stats.
	health map[solana.PublicKey]*ValidatorHealth
	window map[solana.PublicKey]*rollingWindow
}

func NewMonitor(cfg *MonitorConfig) (*Monitor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Monitor{
		log: cfg.Logger,
		cfg: cfg,

		validators:  make(map[solana.PublicKey]*Validator),
		successes:   make(map[solana.PublicKey]uint64),
		failures:    make(map[solana.PublicKey]uint64),
		latestStats: make(map[solana.PublicKey]quic.ConnectionStats),
		lastSeenAt:  make(map[solana.PublicKey]time.Time),
		health:      make(map[solana.PublicKey]*ValidatorHealth),
		window:      make(map[solana.PublicKey]*rollingWindow),
	}, nil
}

func (m *Monitor) Run(ctx context.Context) error {
	m.log.Info("solana monitor starting", "statsInterval", m.cfg.StatsInterval, "parallelism", m.cfg.Parallelism)

	ticker := m.cfg.Clock.NewTicker(m.cfg.StatsInterval)
	defer ticker.Stop()

	err := m.initialize(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize monitor: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			m.log.Debug("solana monitor context done, stopping")
			for _, validator := range m.validators {
				if validator.conn != nil {
					_ = validator.conn.Close()
				}
			}
			return nil
		case <-ticker.Chan():
			err := m.tick(ctx)
			if err != nil {
				m.log.Error("solana monitor failed to tick", "error", err)
			}
		}
	}
}

func (m *Monitor) initialize(ctx context.Context) error {
	validators, err := m.cfg.GetValidatorsFunc(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch validators: %w", err)
	}

	m.mu.Lock()
	m.validators = make(map[solana.PublicKey]*Validator)
	m.mu.Unlock()

	var dialCountNew, dialFailuresNew uint32

	var wg sync.WaitGroup
	sem := make(chan struct{}, m.cfg.Parallelism)
	defer close(sem)
	for _, view := range validators {
		wg.Add(1)
		sem <- struct{}{}
		go func(view *ValidatorView) {
			defer wg.Done()
			defer func() { <-sem }()

			if view.Node == nil || view.Node.TPUQUIC == nil {
				return
			}
			val := &Validator{
				Pubkey:      view.Pubkey,
				View:        view,
				FirstSeenAt: m.cfg.Clock.Now(),
			}

			m.log.Debug("dialing tpu quic for new validator", "pubkey", val.Pubkey.String(), "address", val.TPUQUICAddr())
			err := val.Dial(ctx, &tpuquic.DialConfig{
				Interface:            m.cfg.Interface,
				KeepAlivePeriod:      m.cfg.KeepAlivePeriod,
				HandshakeIdleTimeout: m.cfg.HandshakeIdleTimeout,
			})

			m.mu.Lock()
			defer m.mu.Unlock()

			dialCountNew++
			m.validators[view.Pubkey] = val

			if err != nil {
				m.log.Debug("failed to dial tpu quic", "error", err, "pubkey", view.Pubkey.String(), "address", val.TPUQUICAddr())
				dialFailuresNew++
			}
		}(view)
	}
	wg.Wait()

	m.log.Info("solana monitor initialized",
		"validators", len(m.validators),
		"dialCountNew", dialCountNew,
		"dialFailuresNew", dialFailuresNew,
	)

	return nil
}

func (m *Monitor) tick(ctx context.Context) error {
	m.log.Debug("solana monitor tick", "statsInterval", m.cfg.StatsInterval, "parallelism", m.cfg.Parallelism)

	err := m.updateValidators(ctx)
	if err != nil {
		return fmt.Errorf("failed to update validators: %w", err)
	}

	var successes, failures uint32
	var dialCountNew, dialFailuresNew uint32

	var wg sync.WaitGroup
	sem := make(chan struct{}, m.cfg.Parallelism)
	defer close(sem)
	now := m.cfg.Clock.Now()
	m.mu.Lock()
	validators := m.validators
	m.mu.Unlock()
	for _, val := range validators {
		wg.Add(1)
		sem <- struct{}{}
		go func(val *Validator) {
			defer wg.Done()
			defer func() { <-sem }()

			// If the validator is not connected, try to dial.
			if !val.IsConnected() {
				m.log.Debug("dialing tpu quic for new validator", "pubkey", val.Pubkey.String(), "address", val.TPUQUICAddr())
				err := val.Dial(ctx, &tpuquic.DialConfig{
					Interface:            m.cfg.Interface,
					KeepAlivePeriod:      m.cfg.KeepAlivePeriod,
					HandshakeIdleTimeout: m.cfg.HandshakeIdleTimeout,
				})
				m.mu.Lock()
				dialCountNew++
				if err != nil {
					m.log.Debug("failed to dial tpu quic", "error", err, "pubkey", val.Pubkey.String(), "address", val.TPUQUICAddr())
					dialFailuresNew++
					m.mu.Unlock()
					return
				}
				m.mu.Unlock()
			}

			val.LastSeenAt = now

			m.mu.Lock()
			h := m.health[val.Pubkey]
			if h == nil {
				h = &ValidatorHealth{}
				m.health[val.Pubkey] = h
			}
			w := m.window[val.Pubkey]
			if w == nil {
				w = newRollingWindow(m.cfg.WindowSlots, m.cfg.WindowResolution)
				m.window[val.Pubkey] = w
			}
			m.mu.Unlock()

			ok := false
			var rtt time.Duration

			// If the validator is connected, update stats.
			stats := val.ConnectionStats()
			if stats == nil {
				// Validator disconnected already.
				m.mu.Lock()
				failures++
				m.failures[val.Pubkey]++
				m.mu.Unlock()
				return
			}
			if shouldIgnoreStats(*stats) {
				// TODO(snormore): We should only count these as failures after we've seen been connected for at least one ACK.
				// m.mu.Lock()
				// failures++
				// m.failures[val.Pubkey]++
				// m.mu.Unlock()
				// m.log.Info("tpu quic stats ignored", "pubkey", val.Pubkey.String(), "address", val.TPUQUICAddr(), "stats", statsString(*stats))
			} else {
				ok = true
				rtt = stats.SmoothedRTT
				m.mu.Lock()
				successes++
				m.successes[val.Pubkey]++
				m.latestStats[val.Pubkey] = *stats
				m.lastSeenAt[val.Pubkey] = now

				m.log.Debug("tpu quic stats",
					"pubkey", val.Pubkey.String(),
					"address", val.TPUQUICAddr(),
					"rttMin", stats.MinRTT,
					"rttLatest", stats.LatestRTT,
					"rttSmoothed", stats.SmoothedRTT,
					"rttDev", stats.MeanDeviation,
					"sentBytes", stats.BytesSent,
					"sentPackets", stats.PacketsSent,
					"recvBytes", stats.BytesReceived,
					"recvPackets", stats.PacketsReceived,
					"lostBytes", stats.BytesLost,
					"lostPackets", stats.PacketsLost,
				)

				h.Update(now, ok, m.cfg.EWMAAlpha)
				w.addSample(now, ok, rtt)
				m.mu.Unlock()
			}
		}(val)
	}
	wg.Wait()

	var unhealthy, neverAvailable, belowThreshold uint32
	availabilityThreshold := 0.95
	snap := m.Snapshot()
	for _, s := range snap {
		if s.WindowAvail == 0 {
			if s.Health.LastSuccess.IsZero() {
				unhealthy++
				neverAvailable++
			}
		}
		if s.WindowAvail < availabilityThreshold {
			unhealthy++
			belowThreshold++
		}
	}

	m.log.Info("solana monitor tick complete",
		"validators", len(m.validators),
		"successes", successes,
		"failures", failures,
		"dialCountNew", dialCountNew,
		"dialFailuresNew", dialFailuresNew,
		"unhealthy", unhealthy,
		"neverAvailable", neverAvailable,
		"belowThreshold", belowThreshold,
	)

	return nil
}

func (m *Monitor) updateValidators(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fetch validators.
	validators, err := m.cfg.GetValidatorsFunc(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch validators: %w", err)
	}

	// Find validators that were removed.
	removedAccounts := make(map[solana.PublicKey]struct{})
	for pk := range m.validators {
		if _, ok := validators[pk]; !ok {
			removedAccounts[pk] = struct{}{}
		}
	}
	m.log.Debug("removed validators", "count", len(removedAccounts))

	// Find validators that were added.
	newAccounts := make(map[solana.PublicKey]struct{})
	for pk, val := range validators {
		if val.Node == nil {
			continue
		}
		if _, ok := m.validators[pk]; !ok {
			newAccounts[pk] = struct{}{}
		}
	}
	m.log.Debug("new validators", "count", len(newAccounts))

	// Delete removed validators.
	for pk := range removedAccounts {
		validator, ok := m.validators[pk]
		if !ok {
			continue
		}
		if validator.conn != nil {
			_ = validator.conn.Close()
		}
		delete(m.validators, pk)
		delete(m.health, pk)
		delete(m.window, pk)
	}

	// Add new validators.
	for pk := range newAccounts {
		val := validators[pk]
		if val.Node == nil || val.Node.TPUQUIC == nil {
			// TODO(snormore): We should track this too, validators without a TPU QUIC address.
			continue
		}

		m.validators[pk] = &Validator{
			Pubkey:      pk,
			View:        val,
			FirstSeenAt: m.cfg.Clock.Now(),
		}
		m.health[pk] = &ValidatorHealth{}
		m.window[pk] = newRollingWindow(m.cfg.WindowSlots, m.cfg.WindowResolution)
	}

	// Log validator changes.
	if len(removedAccounts) > 0 || len(newAccounts) > 0 {
		m.log.Info("solana monitor validators changed", "removed", len(removedAccounts), "added", len(newAccounts))
		for pk := range removedAccounts {
			m.log.Debug("solana monitor validator removed", "pubkey", pk.String())
		}
		for pk := range newAccounts {
			m.log.Debug("solana monitor validator added", "pubkey", pk.String())
		}
	}

	return nil
}

func (m *Monitor) ValidatorHealth(pk solana.PublicKey) (ValidatorHealth, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.health[pk]
	if !ok || h == nil {
		return ValidatorHealth{}, false
	}
	return *h, true
}

func (m *Monitor) WindowStats(pk solana.PublicKey) (availability float64, meanRTT time.Duration, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.window[pk]
	if !ok || w == nil {
		return 0, 0, false
	}
	return w.Availability(), w.MeanRTT(), true
}

func shouldIgnoreStats(stats quic.ConnectionStats) bool {
	// Ignore initialization stats; we haven't seen any real data yet.
	if stats.MeanDeviation == 0 || stats.LatestRTT == 100*time.Millisecond {
		return true
	}

	// Ignore stats that don't have any data.
	if stats.BytesSent == 0 && stats.BytesReceived == 0 {
		return true
	}

	return false
}

// func statsString(s quic.ConnectionStats) string {
// 	return fmt.Sprintf(
// 		"RTT{min=%v latest=%v smoothed=%v dev=%v} Sent{bytes=%d pkts=%d} Recv{bytes=%d pkts=%d} Lost{bytes=%d pkts=%d}",
// 		s.MinRTT, s.LatestRTT, s.SmoothedRTT, s.MeanDeviation,
// 		s.BytesSent, s.PacketsSent,
// 		s.BytesReceived, s.PacketsReceived,
// 		s.BytesLost, s.PacketsLost,
// 	)
// }

type ValidatorSummary struct {
	Pubkey         solana.PublicKey
	Health         ValidatorHealth
	WindowAvail    float64
	WindowMeanRTT  time.Duration
	LatestStats    quic.ConnectionStats
	HasLatestStats bool
}

func (m *Monitor) Snapshot() []ValidatorSummary {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []ValidatorSummary
	for pk := range m.validators {
		h := m.health[pk]
		w := m.window[pk]

		var stats quic.ConnectionStats
		latest, hasLatest := m.latestStats[pk]
		if hasLatest {
			stats = latest
		}

		var health ValidatorHealth
		if h != nil {
			health = *h
		}

		out = append(out, ValidatorSummary{
			Pubkey:         pk,
			Health:         health,
			WindowAvail:    w.Availability(),
			WindowMeanRTT:  w.MeanRTT(),
			LatestStats:    stats,
			HasLatestStats: hasLatest,
		})
	}
	return out
}
