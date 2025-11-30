package pubmon

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
)

type ValidatorTargetSourceConfig struct {
	Logger     *slog.Logger
	Clock      clockwork.Clock
	Validators *solmon.ValidatorsView

	// Network / QUIC settings
	Interface            string
	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	KeepAlivePeriod      time.Duration

	// How long we allow "stats not ready" and dial failures to be treated as warmup
	// before we start counting it as a failure (if we've still never seen a success).
	WarmupPeriod time.Duration

	// Health / window settings
	WindowSlots      int
	WindowResolution time.Duration
	HealthEWMAAlpha  float64

	// How often to resync with ValidatorsView and emit add/remove events.
	SyncInterval time.Duration
}

func (c *ValidatorTargetSourceConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Clock == nil {
		return errors.New("clock is required")
	}
	if c.Validators == nil {
		return errors.New("validators view is required")
	}
	if c.Interface == "" {
		return errors.New("interface is required")
	}
	if c.MaxIdleTimeout <= 0 {
		return errors.New("max idle timeout must be greater than 0")
	}
	if c.HandshakeIdleTimeout <= 0 {
		return errors.New("handshake idle timeout must be greater than 0")
	}
	if c.KeepAlivePeriod <= 0 {
		return errors.New("keep alive period must be greater than 0")
	}
	if c.SyncInterval <= 0 {
		c.SyncInterval = 15 * time.Second
	}
	if c.WarmupPeriod <= 0 {
		return errors.New("warmup period must be greater than 0")
	}
	return nil
}

type ValidatorTargetSource struct {
	cfg *ValidatorTargetSourceConfig

	mu          sync.Mutex
	targets     map[gmon.TargetID]gmon.Target
	initialized bool

	// If non-nil, restrict monitoring to this set of IDs.
	allowed map[gmon.TargetID]struct{}

	addedCh   chan gmon.Target
	removedCh chan gmon.TargetID
}

func NewValidatorTargetSource(cfg *ValidatorTargetSourceConfig) (*ValidatorTargetSource, error) {
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	s := &ValidatorTargetSource{
		cfg:       cfg,
		targets:   make(map[gmon.TargetID]gmon.Target),
		addedCh:   make(chan gmon.Target, 128),
		removedCh: make(chan gmon.TargetID, 128),
	}

	return s, nil
}

// Start runs the periodic sync loop that diffs ValidatorsView against our
// current target set and emits add/remove events.
func (s *ValidatorTargetSource) Start(ctx context.Context, cancel context.CancelFunc) {
	s.cfg.Logger.Info("public internet solana validator target source starting", "interface", s.cfg.Interface)

	s.syncOnce()

	go func() {
		ticker := time.NewTicker(s.cfg.SyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.syncOnce()
			}
		}
	}()
}

// All returns a snapshot of the current targets.
func (s *ValidatorTargetSource) All() map[gmon.TargetID]gmon.Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.targets)
}

func (s *ValidatorTargetSource) Added() <-chan gmon.Target {
	return s.addedCh
}

func (s *ValidatorTargetSource) Removed() <-chan gmon.TargetID {
	return s.removedCh
}

// syncOnce diffs the current ValidatorsView against our target set,
// creating new targets for newly seen validators and removing ones that
// have disappeared. It emits events on addedCh/removedCh for *subsequent*
// syncs; the first sync only seeds internal state.

func (s *ValidatorTargetSource) syncOnce() {
	validators := s.cfg.Validators.All()

	s.mu.Lock()

	wasInitialized := s.initialized
	if !s.initialized {
		s.initialized = true
	}

	present := make(map[gmon.TargetID]struct{}, len(validators))
	added := make([]gmon.Target, 0)
	removed := make([]gmon.TargetID, 0)

	// Additions
	for pk, vv := range validators {
		baseID := gmon.TargetID(pk.String())          // bare pubkey
		fullID := gmon.TargetID("pub/" + pk.String()) // canonical TargetID

		// Sampling check uses bare pubkey
		if s.allowed != nil {
			if _, ok := s.allowed[baseID]; !ok {
				continue
			}
		}

		present[fullID] = struct{}{}
		if _, exists := s.targets[fullID]; exists {
			continue
		}

		target, err := solmon.NewValidatorTarget(&solmon.ValidatorTargetConfig{
			Logger: s.cfg.Logger,
			Clock:  s.cfg.Clock,
			Dial: &tpuquic.DialConfig{
				Interface:            s.cfg.Interface,
				MaxIdleTimeout:       s.cfg.MaxIdleTimeout,
				HandshakeIdleTimeout: s.cfg.HandshakeIdleTimeout,
				KeepAlivePeriod:      s.cfg.KeepAlivePeriod,
			},
			Validator:        vv,
			IDPrefix:         "pub/",
			WindowSlots:      s.cfg.WindowSlots,
			WindowResolution: s.cfg.WindowResolution,
			HealthEWMAAlpha:  s.cfg.HealthEWMAAlpha,
			WarmupPeriod:     s.cfg.WarmupPeriod,
		})
		if err != nil {
			s.cfg.Logger.Error("failed to create public internet solana validator target",
				"error", err,
				"pubkey", vv.Pubkey.String(),
			)
			continue
		}
		if target == nil {
			continue
		}

		s.targets[fullID] = target
		added = append(added, target)
	}

	// Removals
	for id := range s.targets {
		if _, ok := present[id]; !ok {
			removed = append(removed, id)
			delete(s.targets, id)
		}
	}

	targetsTotal := len(s.targets)
	addedCount := len(added)
	removedCount := len(removed)
	validatorsTotal := len(validators)

	s.mu.Unlock()

	if !wasInitialized {
		s.cfg.Logger.Info("public internet solana validator target source initial sync",
			"validatorsTotal", validatorsTotal,
			"targetsTotal", targetsTotal,
			"targetsAdded", addedCount,
			"targetsRemoved", removedCount,
		)
		return
	}

	if addedCount > 0 || removedCount > 0 {
		s.cfg.Logger.Info("public internet solana validator target source sync",
			"validatorsTotal", validatorsTotal,
			"targetsTotal", targetsTotal,
			"targetsAdded", addedCount,
			"targetsRemoved", removedCount,
		)
	}

	for _, t := range added {
		select {
		case s.addedCh <- t:
		default:
			s.cfg.Logger.Warn("public internet solana validator target source added channel full, dropping target",
				"target", t.ID().String(),
			)
		}
	}

	for _, id := range removed {
		select {
		case s.removedCh <- id:
		default:
			s.cfg.Logger.Warn("public internet solana validator target source removed channel full, dropping id",
				"target", id.String(),
			)
		}
	}
}
