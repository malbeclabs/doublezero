package solmon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
)

const (
	defaultWindowSlots      = 60
	defaultWindowResolution = 10 * time.Second
	defaultHealthEWMAAlpha  = 0.2
	defaultWarmupPeriod     = 30 * time.Second
)

var (
	ErrNotConnected  = errors.New("validator not connected")
	ErrStatsNotReady = errors.New("stats not ready")
)

// ValidatorProbeResult is the concrete result type for ValidatorTarget.
// A gmon.Scheduler[ValidatorProbeResult] will stream these.
type ValidatorProbeResult struct {
	targetID gmon.TargetID
	Pubkey   solana.PublicKey

	OK    bool
	Stats *quic.ConnectionStats
	Error error

	Health          ValidatorHealth
	WindowAvail     float64
	WindowMeanRTT   time.Duration
	WindowSuccesses uint64
	WindowFailures  uint64

	Warmup         bool   // whether the target was considered in warmup for this result
	WarmupFailures uint64 // total failures observed while in warmup
}

func (r ValidatorProbeResult) TargetID() gmon.TargetID {
	return r.targetID
}

type ValidatorTargetConfig struct {
	Logger    *slog.Logger
	Clock     clockwork.Clock
	Dial      *tpuquic.DialConfig
	Validator *ValidatorView

	// Optional: prefix for TargetID, so we can distinguish different
	// "kinds" of ValidatorTarget in the same scheduler, e.g. "pub/" or "dz/".
	IDPrefix string

	WindowSlots      int
	WindowResolution time.Duration
	HealthEWMAAlpha  float64

	// How long we allow "stats not ready" and dial failures to be treated as warmup
	// before we start counting it as a failure (if we've still never seen a success).
	WarmupPeriod time.Duration
}

func (c *ValidatorTargetConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Clock == nil {
		return errors.New("clock is required")
	}
	if c.Dial == nil {
		return errors.New("dial config is required")
	}
	if c.Validator == nil {
		return errors.New("validator view is required")
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

// ValidatorTarget implements gmon.Target[ValidatorProbeResult].
type ValidatorTarget struct {
	log *slog.Logger
	cfg *ValidatorTargetConfig
	val *ValidatorView

	mu sync.Mutex

	firstSeenAt    time.Time
	lastSeenAt     time.Time
	conn           *tpuquic.Conn
	health         ValidatorHealth
	window         *rollingWindow
	warmupFailures uint64
}

func NewValidatorTarget(cfg *ValidatorTargetConfig) (*ValidatorTarget, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid validator target config: %w", err)
	}
	return &ValidatorTarget{
		log: cfg.Logger,
		cfg: cfg,
		val: cfg.Validator,
	}, nil
}

func (t *ValidatorTarget) ID() gmon.TargetID {
	prefix := t.cfg.IDPrefix
	return gmon.TargetID(prefix + t.val.Pubkey.String())
}

func (t *ValidatorTarget) Probe(ctx context.Context) (gmon.ProbeResult, error) {
	now := t.cfg.Clock.Now()

	t.mu.Lock()
	if t.firstSeenAt.IsZero() {
		t.firstSeenAt = now
		t.health = ValidatorHealth{}
		t.window = newRollingWindow(t.cfg.WindowSlots, t.cfg.WindowResolution)
	}
	t.lastSeenAt = now
	conn := t.conn
	inWarmup := t.health.LastSuccess.IsZero() &&
		now.Sub(t.firstSeenAt) < t.cfg.WarmupPeriod
	t.mu.Unlock()

	var tpuQUICAddr string
	if t.val.Node != nil && t.val.Node.TPUQUIC != nil {
		tpuQUICAddr = *t.val.Node.TPUQUIC
	}

	/* -------------------------
	   1. DIAL IF NEEDED
	   ------------------------- */

	if conn == nil || conn.IsClosed() {
		if err := t.dial(ctx); err != nil {
			t.mu.Lock()
			defer t.mu.Unlock()
			return t.recordSample(now, false, nil, errors.Join(ErrNotConnected, err), inWarmup), nil
		}

		t.log.Debug("validator probe dial succeeded",
			"pubkey", t.val.Pubkey.String(),
			"address", tpuQUICAddr,
			"interface", t.cfg.Dial.Interface,
		)

		time.Sleep(500 * time.Millisecond)
	}

	/* -------------------------
	   2. CONNECTION STATS
	   ------------------------- */

	t.mu.Lock()
	// conn may have been replaced by dial() above
	conn = t.conn
	stats := conn.ConnectionStats()
	// recompute warmup in case time moved a lot
	inWarmup = t.health.LastSuccess.IsZero() &&
		now.Sub(t.firstSeenAt) < t.cfg.WarmupPeriod
	t.mu.Unlock()

	/* -------------------------
	   3. STATS NOT READY
	   ------------------------- */

	if statsNotReady(&stats) {
		t.log.Debug("validator stats not ready",
			"pubkey", t.val.Pubkey.String(),
			"address", tpuQUICAddr,
			"interface", t.cfg.Dial.Interface,
		)

		t.mu.Lock()
		defer t.mu.Unlock()
		return t.recordSample(now, false, &stats, ErrStatsNotReady, inWarmup), nil
	}

	/* -------------------------
	   4. SUCCESS PATH
	   ------------------------- */

	t.log.Debug("tpu quic stats",
		"pubkey", t.val.Pubkey.String(),
		"address", tpuQUICAddr,
		"interface", t.cfg.Dial.Interface,
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

	// First success should *end* warmup and be counted as a real success sample.
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.recordSample(now, true, &stats, nil, false), nil
}

func (t *ValidatorTarget) dial(ctx context.Context) error {
	if t.val == nil || t.val.Node == nil || t.val.Node.TPUQUIC == nil {
		return fmt.Errorf("validator %s has no TPU QUIC address", t.val.Pubkey.String())
	}

	tpuQUICAddr := *t.val.Node.TPUQUIC

	t.mu.Lock()
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
	t.mu.Unlock()

	t.log.Debug("dialing validator tpu quic",
		"pubkey", t.val.Pubkey.String(),
		"address", tpuQUICAddr,
		"interface", t.cfg.Dial.Interface,
	)

	conn, err := tpuquic.Dial(ctx, tpuQUICAddr, t.cfg.Dial)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		t.log.Debug("failed to dial validator tpu quic",
			"error", err,
			"pubkey", t.val.Pubkey.String(),
			"address", tpuQUICAddr,
			"interface", t.cfg.Dial.Interface,
		)
		return err
	}

	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()

	return nil
}

func statsNotReady(stats *quic.ConnectionStats) bool {
	if stats == nil {
		return true
	}

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

func (t *ValidatorTarget) recordSample(
	now time.Time,
	ok bool,
	stats *quic.ConnectionStats,
	err error,
	warmup bool,
) ValidatorProbeResult {
	var rtt time.Duration
	if ok && stats != nil {
		rtt = stats.SmoothedRTT
	}

	if warmup {
		if !ok {
			t.warmupFailures++
		}
		// No EWMA / window update during warmup.
	} else {
		t.health.Update(now, ok, t.cfg.HealthEWMAAlpha)
		t.window.addSample(now, ok, rtt)
	}

	avail := t.window.Availability()
	meanRTT := t.window.MeanRTT()
	succ, fail := t.window.Counts()

	return ValidatorProbeResult{
		targetID:        t.ID(),
		Pubkey:          t.val.Pubkey,
		OK:              ok,
		Error:           err,
		Stats:           stats,
		Health:          t.health,
		WindowAvail:     avail,
		WindowMeanRTT:   meanRTT,
		WindowSuccesses: succ,
		WindowFailures:  fail,
		Warmup:          warmup,
		WarmupFailures:  t.warmupFailures,
	}
}
