package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	// DefaultMaxEpochStaleness bounds how long the pinger keeps probing with a cached epoch after
	// the ledger RPC stops answering. The binding constraint is the sample buffer underneath it,
	// not the likelihood of a rollover: a partition holds partitionBufferCapacity samples, and the
	// submitter cannot drain during the same outage, so once it finds the partition over capacity
	// it discards the whole backlog in one tick. Probing past that point trades a visible gap for
	// silently backdated samples, since the account carries one start timestamp and a fixed
	// interval rather than a timestamp per sample. The collector clamps this to what the buffer can
	// actually hold at the configured probe interval; the constant is the default at the default
	// interval. A rollover is bounded separately, and much more tightly, by the epoch's projected
	// end.
	DefaultMaxEpochStaleness = 10 * time.Hour

	// defaultEpochRefreshInterval is used when neither EpochRefreshInterval nor Interval is set.
	defaultEpochRefreshInterval = 10 * time.Second

	// epochStaleWarnAfter is how many consecutive failed fetches are needed before the fresh->stale
	// transition is reported at Warn. A flapping endpoint would otherwise emit a Warn and an Info
	// per flap, which is what the fallback is meant to avoid.
	epochStaleWarnAfter = 3
)

type PingerConfig struct {
	LocalDevicePK     solana.PublicKey
	Interval          time.Duration
	ProbeTimeout      time.Duration
	Peers             PeerDiscovery
	Buffer            buffer.PartitionedBuffer[PartitionKey, Sample]
	GetSender         func(ctx context.Context, peer *Peer) twamplight.Sender
	GetEpochInfo      func(ctx context.Context) (EpochInfo, error)
	RecordProbeResult func(peer *Peer, success bool)

	// EpochRefreshInterval is how often the cached epoch is refreshed in the background.
	// Defaults to Interval, which keeps the epoch RPC rate the same as when the fetch was inline.
	EpochRefreshInterval time.Duration

	// MaxEpochStaleness is how long a cached epoch is trusted after the last successful fetch.
	// Defaults to DefaultMaxEpochStaleness.
	MaxEpochStaleness time.Duration

	// NowFunc is the function used to measure the age of the cached epoch. Defaults to time.Now,
	// whose monotonic reading is what keeps an NTP step from looking like hours of staleness.
	NowFunc func() time.Time
}

// Pinger is responsible for periodically probing remote peers using TWAMP.
// It gathers round-trip time (RTT) and loss measurements and records them
// into the shared sample buffer.
type Pinger struct {
	log *slog.Logger
	cfg *PingerConfig

	// The epoch is only used to build the sample buffer's partition key, so probing itself needs
	// no ledger access. refreshEpochLoop is the sole writer of this state and the probe path only
	// reads it: a total RPC outage costs us epoch precision, not measurements.
	mu                  sync.Mutex
	epoch               uint64
	haveEpoch           bool
	epochAt             time.Time
	epochEndsAt         time.Time
	slotRate            slotRateEstimator
	servingStale        bool
	consecutiveFailures int
	warnedStale         bool
	warnedNoEpoch       bool
	warnedTooStale      bool
	warnedEpochEnded    bool
}

func NewPinger(log *slog.Logger, cfg *PingerConfig) *Pinger {
	if cfg.EpochRefreshInterval <= 0 {
		cfg.EpochRefreshInterval = cfg.Interval
	}
	if cfg.EpochRefreshInterval <= 0 {
		cfg.EpochRefreshInterval = defaultEpochRefreshInterval
	}
	if cfg.MaxEpochStaleness <= 0 {
		cfg.MaxEpochStaleness = DefaultMaxEpochStaleness
	}
	if cfg.NowFunc == nil {
		// time.Now, not time.Now().UTC(): .UTC() strips the monotonic reading, which would make the
		// staleness bound pure wall-clock arithmetic. A device booting with a bad RTC and then
		// stepping forward over NTP would trip the bound instantly and stop probing.
		cfg.NowFunc = time.Now
	}
	return &Pinger{log: log, cfg: cfg}
}

func (p *Pinger) Run(ctx context.Context) error {
	p.log.Info("Starting probe loop")

	// Refresh the epoch on its own loop so an unreachable ledger RPC cannot stall probing. A
	// failing fetch burns ~130s across its retries and the probe ticker only buffers one tick, so
	// fetching inline dropped roughly a dozen probe opportunities per failure.
	go p.refreshEpochLoop(ctx)

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Debug("Probe loop done")
			return nil
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

func (p *Pinger) Tick(ctx context.Context) {
	epoch, ok := p.epochForTick()
	if !ok {
		return
	}

	peers := p.cfg.Peers.GetPeers()
	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(peer *Peer) {
			defer wg.Done()

			if !sleepOrDone(ctx, time.Millisecond) {
				p.log.Debug("Probe loop cancelled during iteration")
				return
			}

			partitionKey := PartitionKey{
				OriginDevicePK: p.cfg.LocalDevicePK,
				TargetDevicePK: peer.DevicePK,
				LinkPK:         peer.LinkPK,
				Epoch:          epoch,
			}

			ts := time.Now().UTC()

			if peer.Tunnel == nil {
				p.log.Debug("Tunnel not found, recording loss", "device", peer.DevicePK.String(), "link", peer.LinkPK.String())
				p.cfg.Buffer.Add(partitionKey, Sample{
					Timestamp: ts,
					RTT:       0,
					Loss:      true,
				})
				return
			}

			log := p.log.With("device", peer.DevicePK.String(), "link", peer.LinkPK.String(), "addr", peer.Tunnel.TargetIP.String())

			sender := p.cfg.GetSender(ctx, peer)
			if sender == nil {
				log.Debug("Failed to create sender, recording loss")
				p.cfg.Buffer.Add(partitionKey, Sample{
					Timestamp: ts,
					RTT:       0,
					Loss:      true,
				})
				return
			}

			var probeCtx context.Context
			var probeCancel context.CancelFunc
			if p.cfg.ProbeTimeout > 0 {
				probeCtx, probeCancel = context.WithTimeout(ctx, p.cfg.ProbeTimeout)
			} else {
				probeCtx = ctx
			}

			log.Debug("Probing", "source", peer.Tunnel.SourceIP, "interface", peer.Tunnel.Interface, "remote", peer.Tunnel.TargetIP, "timeout", p.cfg.ProbeTimeout)
			rtt, err := sender.Probe(probeCtx)
			if probeCancel != nil {
				probeCancel()
			}
			if err != nil {
				log.Debug("Probe failed, recording loss", "error", err)
				p.cfg.Buffer.Add(partitionKey, Sample{
					Timestamp: ts,
					RTT:       0,
					Loss:      true,
				})
				if p.cfg.RecordProbeResult != nil {
					p.cfg.RecordProbeResult(peer, false)
				}
				return
			}

			p.cfg.Buffer.Add(partitionKey, Sample{
				Timestamp: ts,
				RTT:       rtt,
				Loss:      false,
			})
			if p.cfg.RecordProbeResult != nil {
				p.cfg.RecordProbeResult(peer, true)
			}
		}(peer)
	}
	wg.Wait()
}

// epochForTick returns the epoch to stamp this tick's samples with, and whether to probe at all.
// It only reads the cache; refreshEpochLoop is the sole writer, so the probe path never contacts
// the ledger and never acts on a value another goroutine is about to replace.
//
// Probing is refused when no epoch has ever been fetched, when the cached one is older than
// MaxEpochStaleness, or when it has likely rolled over. Each case logs once rather than per tick,
// and each is counted separately: a bad ledger URL and a multi-hour outage want different alerts.
func (p *Pinger) epochForTick() (uint64, bool) {
	now := p.cfg.NowFunc()

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.haveEpoch {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochNeverFetched).Inc()
		if !p.warnedNoEpoch {
			p.warnedNoEpoch = true
			p.log.Error("No epoch available and none cached, skipping probes until the ledger answers")
		} else {
			p.log.Debug("No epoch available and none cached, skipping probe tick")
		}
		return 0, false
	}

	if age := now.Sub(p.epochAt); age > p.cfg.MaxEpochStaleness {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochTooStale).Inc()
		if !p.warnedTooStale {
			p.warnedTooStale = true
			p.log.Warn("Cached epoch is too stale to probe with, skipping probes until the ledger answers", "epoch", p.epoch, "age", age, "maxStaleness", p.cfg.MaxEpochStaleness)
		} else {
			p.log.Debug("Cached epoch is too stale to probe with, skipping probe tick", "epoch", p.epoch, "age", age)
		}
		return 0, false
	}

	// Samples are written to the account of the epoch in their partition key, and the reader
	// synthesizes their timestamps from that account's start time. Probing past the projected
	// rollover would file them under the previous epoch with timestamps in the next epoch's window,
	// where a time-range query scoped to the new epoch never looks.
	if !p.epochEndsAt.IsZero() && now.After(p.epochEndsAt) {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochEnded).Inc()
		if !p.warnedEpochEnded {
			p.warnedEpochEnded = true
			p.log.Warn("Cached epoch has likely rolled over, skipping probes until the ledger answers", "epoch", p.epoch, "projectedEnd", p.epochEndsAt.UTC(), "overBy", now.Sub(p.epochEndsAt))
		} else {
			p.log.Debug("Cached epoch has likely rolled over, skipping probe tick", "epoch", p.epoch, "projectedEnd", p.epochEndsAt.UTC())
		}
		return 0, false
	}

	return p.epoch, true
}

// refreshEpochLoop keeps the cached epoch warm, independently of the probe loop.
func (p *Pinger) refreshEpochLoop(ctx context.Context) {
	p.RefreshEpoch(ctx)

	ticker := time.NewTicker(p.cfg.EpochRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Debug("Epoch refresh loop done")
			return
		case <-ticker.C:
			p.RefreshEpoch(ctx)
		}
	}
}

// RefreshEpoch fetches the current epoch and updates the cache the probe path reads. It is the one
// step of the refresh loop, exposed the same way Tick exposes one step of the probe loop, and it is
// the only writer of the epoch cache — Tick never contacts the ledger.
func (p *Pinger) RefreshEpoch(ctx context.Context) {
	info, err := p.getEpochInfo(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.markEpochStale(err)
		return
	}
	p.storeEpoch(info)
}

// storeEpoch caches a freshly fetched epoch and projects when it ends, and reports the recovery if
// we had been falling back to a cached value.
//
// Everything here happens under the lock, including the log line and the gauge: emitting them after
// the unlock let a recovery be reported before the failure it recovered from, and let the gauge be
// left large right after a successful fetch.
func (p *Pinger) storeEpoch(info EpochInfo) {
	now := p.cfg.NowFunc()

	p.mu.Lock()
	defer p.mu.Unlock()

	wasStale, warned, hadEpoch := p.servingStale, p.warnedStale, p.haveEpoch
	cached, staleSince := p.epoch, p.epochAt

	p.epoch = info.Epoch
	p.haveEpoch = true
	p.epochAt = now
	p.epochEndsAt = projectEpochEnd(now, info, p.slotRate.observe(info.AbsoluteSlot, now))
	p.servingStale = false
	p.consecutiveFailures = 0
	p.warnedStale = false
	p.warnedNoEpoch = false
	p.warnedTooStale = false
	p.warnedEpochEnded = false

	metrics.EpochCacheStaleAge.Set(0)

	switch {
	case !wasStale:
	case !hadEpoch:
		// Started while the ledger was unreachable, so there was nothing to fall back to. Reported
		// unconditionally: epochForTick logged the refusal at Error, so the resolution belongs above
		// Debug too.
		p.log.Info("Epoch fetch recovered, starting to probe", "epoch", info.Epoch)
	case warned:
		p.log.Info("Epoch fetch recovered", "epoch", info.Epoch, "cachedEpoch", cached, "staleFor", now.Sub(staleSince))
	}
}

// markEpochStale records that the epoch fetch is failing. Repeated failures are collapsed into the
// fresh->stale transition so that a multi-hour outage produces a handful of log lines rather than
// one per attempt, and the transition itself waits for epochStaleWarnAfter consecutive failures so
// that a flapping endpoint does not produce a Warn/Info pair per flap.
func (p *Pinger) markEpochStale(err error) {
	now := p.cfg.NowFunc()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.servingStale = true
	p.consecutiveFailures++

	if !p.haveEpoch {
		// Nothing to fall back to, and epochForTick reports that at Error. The gauge gets a sentinel
		// rather than being left at 0, so "restarted mid-outage, never probed" — the worst state
		// there is — does not read the same as healthy.
		metrics.EpochCacheStaleAge.Set(math.Inf(1))
		p.log.Debug("Failed to get current epoch, none cached", "error", err)
		return
	}

	age := now.Sub(p.epochAt)
	metrics.EpochCacheStaleAge.Set(age.Seconds())

	switch {
	case p.consecutiveFailures < epochStaleWarnAfter:
		p.log.Debug("Failed to get current epoch, probing with the last known epoch", "epoch", p.epoch, "age", age, "consecutiveFailures", p.consecutiveFailures, "error", err)
	case !p.warnedStale:
		p.warnedStale = true
		p.log.Warn("Failed to get current epoch, probing with the last known epoch", "epoch", p.epoch, "age", age, "consecutiveFailures", p.consecutiveFailures, "maxStaleness", p.cfg.MaxEpochStaleness, "error", err)
	default:
		p.log.Debug("Failed to get current epoch, still probing with the last known epoch", "epoch", p.epoch, "age", age, "error", err)
	}
}

// getEpochInfo gets the current epoch and its slot position, with a few retries to mitigate any
// transient network issues. Callers do not rely on this to succeed: the refresh loop tries again on
// its next tick, and probing continues against the cached epoch in the meantime.
func (p *Pinger) getEpochInfo(ctx context.Context) (EpochInfo, error) {
	attempt := 0
	info, err := backoff.Retry(ctx, func() (EpochInfo, error) {
		attempt++
		info, err := p.cfg.GetEpochInfo(ctx)
		if err != nil {
			// Counted per attempt, so a partially degraded endpoint whose retries eventually succeed
			// still leaves a signal — markEpochStale never runs in that mode. Logged at Debug, not
			// Warn: the operator-facing line is markEpochStale's, collapsed into the fresh->stale
			// transition, and a per-attempt warning here meant thousands of lines across the outage.
			metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochFetchFailed).Inc()
			p.log.Debug("Failed to get current epoch, retrying", "attempt", attempt, "error", err)
			return EpochInfo{}, err
		}
		return info, nil
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxTries(3))
	if err != nil {
		return EpochInfo{}, fmt.Errorf("failed to get current epoch: %w", err)
	}
	return info, nil
}
