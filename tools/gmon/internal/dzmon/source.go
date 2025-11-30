// filename: dz_source.go
package dzmon

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
)

type DoubleZeroTargetSourceConfig struct {
	Logger         *slog.Logger
	Clock          clockwork.Clock
	Validators     *solmon.ValidatorsView
	Serviceability *ServiceabilityView
	Daemon         DaemonClient

	Interface            string
	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	KeepAlivePeriod      time.Duration

	// How long we allow "stats not ready" and dial failures to be treated as warmup
	// before we start counting it as a failure (if we've still never seen a success).
	WarmupPeriod time.Duration

	WindowSlots      int
	WindowResolution time.Duration
	HealthEWMAAlpha  float64

	SyncInterval time.Duration
}

func (c *DoubleZeroTargetSourceConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Validators == nil {
		return errors.New("validators view is required")
	}
	if c.Daemon == nil {
		return errors.New("daemon client is required")
	}
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
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
	if c.WindowSlots <= 0 {
		c.WindowSlots = defaultWindowSlots
	}
	if c.WindowResolution <= 0 {
		c.WindowResolution = defaultWindowResolution
	}
	if c.HealthEWMAAlpha <= 0 || c.HealthEWMAAlpha > 1 {
		c.HealthEWMAAlpha = defaultHealthEWMAAlpha
	}
	if c.WarmupPeriod <= 0 {
		c.WarmupPeriod = defaultWarmupPeriod
	}
	return nil
}

type DoubleZeroTargetSource struct {
	log *slog.Logger
	cfg *DoubleZeroTargetSourceConfig

	mu      sync.Mutex
	targets map[gmon.TargetID]gmon.Target

	initialized bool

	addedCh   chan gmon.Target
	removedCh chan gmon.TargetID
}

func NewDoubleZeroTargetSource(cfg *DoubleZeroTargetSourceConfig) (*DoubleZeroTargetSource, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &DoubleZeroTargetSource{
		log:       cfg.Logger,
		cfg:       cfg,
		targets:   make(map[gmon.TargetID]gmon.Target),
		addedCh:   make(chan gmon.Target, 128),
		removedCh: make(chan gmon.TargetID, 128),
	}, nil
}

func (s *DoubleZeroTargetSource) Start(ctx context.Context, cancel context.CancelFunc) {
	s.log.Info("doublezero solana validator target source starting", "interface", s.cfg.Interface)

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

func (s *DoubleZeroTargetSource) All() map[gmon.TargetID]gmon.Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.targets)
}

func (s *DoubleZeroTargetSource) Added() <-chan gmon.Target {
	return s.addedCh
}

func (s *DoubleZeroTargetSource) Removed() <-chan gmon.TargetID {
	return s.removedCh
}

func (s *DoubleZeroTargetSource) syncOnce() {
	validators := s.cfg.Validators.All()
	users := s.cfg.Serviceability.Users()

	routes, err := s.cfg.Daemon.GetRoutes(context.Background())
	if err != nil {
		s.log.Error("failed to get doublezero routes", "error", err)
		return
	}
	routesByPeerIP := make(map[string]Route, len(routes))
	for _, route := range routes {
		if route.KernelState != KernelStatePresent {
			continue
		}
		if route.UserType != UserTypeIBRL && route.UserType != UserTypeIBRLWithAllocatedIP {
			continue
		}
		routesByPeerIP[route.PeerIP] = route
	}

	s.mu.Lock()

	wasInitialized := s.initialized
	if !s.initialized {
		s.initialized = true
	}

	present := make(map[gmon.TargetID]struct{}, len(users))
	added := make([]*DoubleZeroTarget, 0)
	removed := make([]gmon.TargetID, 0)

	var notFoundRoutesCount uint32

	validatorsByIP := make(map[string]*solmon.ValidatorView, len(validators))
	for _, val := range validators {
		if val.Node == nil || val.Node.TPUQUIC == nil {
			continue
		}
		host, _, err := net.SplitHostPort(*val.Node.TPUQUIC)
		if err != nil {
			continue
		}
		validatorsByIP[host] = val
	}

	for _, user := range users {
		ip := user.DZIP.String()

		val, ok := validatorsByIP[ip]
		if !ok {
			continue
		}

		// TODO(snormore): We should track some metric for validators that are supposed to be
		// connected to DZ but are not in the routing table, not being advertised by BGP.

		_, ok = routesByPeerIP[ip]
		if !ok {
			notFoundRoutesCount++
			continue
		}

		id := gmon.TargetID("dz/" + val.Pubkey.String())
		present[id] = struct{}{}

		if _, exists := s.targets[id]; exists {
			continue
		}

		target, err := NewDoubleZeroTarget(&DoubleZeroTargetConfig{
			Logger: s.cfg.Logger,
			Clock:  s.cfg.Clock,
			Dial: &tpuquic.DialConfig{
				Interface:            s.cfg.Interface,
				MaxIdleTimeout:       s.cfg.MaxIdleTimeout,
				HandshakeIdleTimeout: s.cfg.HandshakeIdleTimeout,
				KeepAlivePeriod:      s.cfg.KeepAlivePeriod,
			},
			Validator:        val,
			IDPrefix:         "dz/",
			WindowSlots:      s.cfg.WindowSlots,
			WindowResolution: s.cfg.WindowResolution,
			HealthEWMAAlpha:  s.cfg.HealthEWMAAlpha,
			WarmupPeriod:     s.cfg.WarmupPeriod,
		})
		if err != nil {
			s.log.Error("failed to create doublezero solana target",
				"error", err,
				"pubkey", val.Pubkey.String(),
			)
			continue
		}
		if target == nil {
			continue
		}

		s.targets[id] = target
		added = append(added, target)
	}

	for id := range s.targets {
		if _, ok := present[id]; !ok {
			removed = append(removed, id)
			delete(s.targets, id)
		}
	}

	// capture values while we still hold the lock
	targetsTotal := len(s.targets)
	validatorTargetsActive := len(present)
	addedCount := len(added)
	removedCount := len(removed)
	usersTotal := len(users)
	validatorsTotal := len(validators)

	s.mu.Unlock()

	if !wasInitialized {
		s.log.Info("doublezero solana validator target source initial sync",
			"usersTotal", usersTotal,
			"validatorsTotal", validatorsTotal,
			"validatorTargetsActive", validatorTargetsActive,
			"targetsTotal", targetsTotal,
			"targetsAdded", addedCount,
			"targetsRemoved", removedCount,
			"notFoundRoutes", notFoundRoutesCount,
		)
		return
	}

	for _, t := range added {
		select {
		case s.addedCh <- t:
		default:
			s.log.Warn("doublezero solana validator target source added channel full, dropping target",
				"target", t.ID().String(),
			)
		}
	}

	for _, id := range removed {
		select {
		case s.removedCh <- id:
		default:
			s.log.Warn("doublezero solana validator target source removed channel full, dropping id",
				"target", id.String(),
			)
		}
	}

	if addedCount > 0 || removedCount > 0 {
		s.log.Info("doublezero solana validator target source sync",
			"usersTotal", usersTotal,
			"validatorsTotal", validatorsTotal,
			"validatorTargetsActive", validatorTargetsActive,
			"targetsTotal", targetsTotal,
			"targetsAdded", addedCount,
			"targetsRemoved", removedCount,
			"notFoundRoutes", notFoundRoutesCount,
		)
	}
}
