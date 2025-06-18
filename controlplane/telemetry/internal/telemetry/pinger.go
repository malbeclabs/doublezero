package telemetry

import (
	"context"
	"log/slog"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

type PingerConfig struct {
	Interval  time.Duration
	Peers     PeerDiscovery
	Buffer    *SampleBuffer
	GetSender func(peerKey string, peer *Peer) twamplight.Sender
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
	p.log.Info("==> Starting probe loop")

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Debug("==> Probe loop done")
			return nil
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

func (p *Pinger) Tick(ctx context.Context) {
	peers := p.cfg.Peers.GetPeers()
	for peerKey, peer := range peers {
		if !sleepOrDone(ctx, time.Millisecond) {
			p.log.Debug("==> Probe loop cancelled during iteration")
			return
		}

		ts := time.Now().UTC()

		sender := p.cfg.GetSender(peerKey, peer)
		if sender == nil {
			p.log.Debug("==> Failed to create sender, recording loss", "peer", peerKey)
			p.cfg.Buffer.Add(Sample{
				Timestamp: ts,
				Link:      peer.LinkPubkey,
				Device:    peer.DevicePubkey,
				RTT:       0,
				Loss:      true,
			})
			continue
		}

		rtt, err := sender.Probe(ctx)
		if err != nil {
			p.log.Debug("==> Probe failed, recording loss", "peer", peerKey, "error", err)
			p.cfg.Buffer.Add(Sample{
				Timestamp: ts,
				Link:      peer.LinkPubkey,
				Device:    peer.DevicePubkey,
				RTT:       0,
				Loss:      true,
			})
			continue
		}

		p.cfg.Buffer.Add(Sample{
			Timestamp: ts,
			Link:      peer.LinkPubkey,
			Device:    peer.DevicePubkey,
			RTT:       rtt,
			Loss:      false,
		})
	}
}
