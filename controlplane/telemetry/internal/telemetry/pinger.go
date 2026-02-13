package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
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
}

// Pinger is responsible for periodically probing remote peers using TWAMP.
// It gathers round-trip time (RTT) and loss measurements and records them
// into the shared sample buffer.
type Pinger struct {
	log *slog.Logger
	cfg *PingerConfig
}

func NewPinger(log *slog.Logger, cfg *PingerConfig) *Pinger {
	return &Pinger{log: log, cfg: cfg}
}

func (p *Pinger) Run(ctx context.Context) error {
	p.log.Info("Starting probe loop")

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
	epoch, err := p.getCurrentEpoch(ctx)
	if err != nil {
		p.log.Error("failed to get current epoch", "error", err)
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

// getCurrentEpoch gets the current epoch, with a few retries to mitigate any transient network
// issues. The pinger does not rely on this to succeed, and will just try again on the next tick
// if it fails all retries.
func (p *Pinger) getCurrentEpoch(ctx context.Context) (uint64, error) {
	attempt := 0
	epoch, err := backoff.Retry(ctx, func() (uint64, error) {
		if attempt > 1 {
			p.log.Warn("Failed to get current epoch, retrying", "attempt", attempt)
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
