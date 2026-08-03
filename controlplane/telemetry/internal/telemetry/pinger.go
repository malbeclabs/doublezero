package telemetry

import (
	"context"
	"fmt"
	"log/slog"
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
	// the ledger RPC stops answering. Samples recorded with a stale epoch are written to that
	// epoch's account, so once we are far enough behind that a rollover is likely they would be
	// misattributed to the previous epoch.
	DefaultMaxEpochStaleness = 24 * time.Hour

	// defaultEpochRefreshInterval is used when neither EpochRefreshInterval nor Interval is set.
	defaultEpochRefreshInterval = 10 * time.Second
)

type PingerConfig struct {
	LocalDevicePK     solana.PublicKey
	Interval          time.Duration
	ProbeTimeout      time.Duration
	Peers             PeerDiscovery
	Buffer            buffer.PartitionedBuffer[PartitionKey, Sample]
	GetSender         func(ctx context.Context, peer *Peer) twamplight.Sender
	GetCurrentEpoch   func(ctx context.Context) (uint64, error)
	RecordProbeResult func(peer *Peer, success bool)

	// EpochRefreshInterval is how often the cached epoch is refreshed in the background.
	// Defaults to Interval, which keeps the epoch RPC rate the same as when the fetch was inline.
	EpochRefreshInterval time.Duration

	// MaxEpochStaleness is how long a cached epoch is trusted after the last successful fetch.
	// Defaults to DefaultMaxEpochStaleness.
	MaxEpochStaleness time.Duration

	// NowFunc is the function used to measure the age of the cached epoch.
	// Defaults to time.Now().UTC.
	NowFunc func() time.Time
}

// Pinger is responsible for periodically probing remote peers using TWAMP.
// It gathers round-trip time (RTT) and loss measurements and records them
// into the shared sample buffer.
type Pinger struct {
	log *slog.Logger
	cfg *PingerConfig

	// The epoch is only used to build the sample buffer's partition key, so probing itself needs
	// no ledger access. It is refreshed on its own loop and cached here, and the probe path reads
	// the cache rather than the ledger: a total RPC outage costs us epoch precision, not
	// measurements.
	mu             sync.Mutex
	epoch          uint64
	haveEpoch      bool
	epochAt        time.Time
	servingStale   bool
	warnedNoEpoch  bool
	warnedTooStale bool
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
		cfg.NowFunc = func() time.Time { return time.Now().UTC() }
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
	epoch, ok := p.epochForTick(ctx)
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
// It reads the cached epoch and does not contact the ledger, except on the first call before the
// refresh loop has landed a value.
//
// Probing is refused only when no epoch has ever been fetched, or when the cached one is older
// than MaxEpochStaleness. Both cases log once rather than per tick.
func (p *Pinger) epochForTick(ctx context.Context) (uint64, bool) {
	p.mu.Lock()
	epoch, have, at := p.epoch, p.haveEpoch, p.epochAt
	p.mu.Unlock()

	if !have {
		// Nothing cached: either the agent just started and the refresh loop has not produced a
		// value yet, or Tick is being driven directly. Fetch inline so the tick is not wasted.
		epoch, err := p.getCurrentEpoch(ctx)
		if err != nil {
			metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochUnavailable).Inc()

			p.mu.Lock()
			first := !p.warnedNoEpoch
			p.warnedNoEpoch = true
			p.mu.Unlock()

			if first {
				p.log.Error("No epoch available and none cached, skipping probes until the ledger answers", "error", err)
			} else {
				p.log.Debug("No epoch available and none cached, skipping probe tick", "error", err)
			}
			return 0, false
		}
		p.storeEpoch(epoch)
		return epoch, true
	}

	age := p.cfg.NowFunc().Sub(at)
	if age > p.cfg.MaxEpochStaleness {
		metrics.Errors.WithLabelValues(metrics.ErrorTypePingerEpochUnavailable).Inc()

		p.mu.Lock()
		first := !p.warnedTooStale
		p.warnedTooStale = true
		p.mu.Unlock()

		if first {
			p.log.Warn("Cached epoch is too stale to probe with, skipping probes until the ledger answers", "epoch", epoch, "age", age, "maxStaleness", p.cfg.MaxEpochStaleness)
		} else {
			p.log.Debug("Cached epoch is too stale to probe with, skipping probe tick", "epoch", epoch, "age", age)
		}
		return 0, false
	}

	return epoch, true
}

// refreshEpochLoop keeps the cached epoch warm, independently of the probe loop.
func (p *Pinger) refreshEpochLoop(ctx context.Context) {
	p.refreshEpoch(ctx)

	ticker := time.NewTicker(p.cfg.EpochRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Debug("Epoch refresh loop done")
			return
		case <-ticker.C:
			p.refreshEpoch(ctx)
		}
	}
}

func (p *Pinger) refreshEpoch(ctx context.Context) {
	epoch, err := p.getCurrentEpoch(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.markEpochStale(err)
		return
	}
	p.storeEpoch(epoch)
}

// storeEpoch caches a freshly fetched epoch, and reports the recovery if we had been falling back
// to a cached value.
func (p *Pinger) storeEpoch(epoch uint64) {
	now := p.cfg.NowFunc()

	p.mu.Lock()
	wasStale, hadEpoch := p.servingStale, p.haveEpoch
	cached, staleSince := p.epoch, p.epochAt
	p.epoch = epoch
	p.haveEpoch = true
	p.epochAt = now
	p.servingStale = false
	p.warnedNoEpoch = false
	p.warnedTooStale = false
	p.mu.Unlock()

	metrics.EpochCacheStaleAge.Set(0)

	if wasStale {
		if hadEpoch {
			p.log.Info("Epoch fetch recovered", "epoch", epoch, "cachedEpoch", cached, "staleFor", now.Sub(staleSince))
		} else {
			// Started while the ledger was unreachable, so there was nothing to fall back to.
			p.log.Info("Epoch fetch recovered, starting to probe", "epoch", epoch)
		}
	}
}

// markEpochStale records that the epoch fetch is failing. Repeated failures are collapsed into the
// fresh->stale transition so that a multi-hour outage produces a handful of log lines rather than
// one per attempt.
func (p *Pinger) markEpochStale(err error) {
	p.mu.Lock()
	epoch, have, at := p.epoch, p.haveEpoch, p.epochAt
	first := !p.servingStale
	p.servingStale = true
	p.mu.Unlock()

	if !have {
		// epochForTick reports this case; there is no cached epoch to fall back to.
		p.log.Debug("Failed to get current epoch, none cached", "error", err)
		return
	}

	age := p.cfg.NowFunc().Sub(at)
	metrics.EpochCacheStaleAge.Set(age.Seconds())

	if first {
		p.log.Warn("Failed to get current epoch, probing with the last known epoch", "epoch", epoch, "age", age, "maxStaleness", p.cfg.MaxEpochStaleness, "error", err)
	} else {
		p.log.Debug("Failed to get current epoch, still probing with the last known epoch", "epoch", epoch, "age", age, "error", err)
	}
}

// getCurrentEpoch gets the current epoch, with a few retries to mitigate any transient network
// issues. Callers do not rely on this to succeed: the refresh loop tries again on its next tick,
// and probing continues against the cached epoch in the meantime.
func (p *Pinger) getCurrentEpoch(ctx context.Context) (uint64, error) {
	attempt := 0
	epoch, err := backoff.Retry(ctx, func() (uint64, error) {
		if attempt > 1 {
			// Debug, not Warn: markEpochStale carries the operator-facing signal, collapsed into
			// the fresh->stale transition. A per-attempt warning here means thousands of lines
			// across a multi-hour outage.
			p.log.Debug("Failed to get current epoch, retrying", "attempt", attempt)
		}
		attempt++
		epoch, err := p.cfg.GetCurrentEpoch(ctx)
		if err != nil {
			return 0, err
		}
		return epoch, nil
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxTries(3))
	if err != nil {
		return 0, fmt.Errorf("failed to get current epoch: %w", err)
	}
	return epoch, nil
}
