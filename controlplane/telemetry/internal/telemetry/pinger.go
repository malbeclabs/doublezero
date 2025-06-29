package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

type PingerConfig struct {
	LocalDevicePK solana.PublicKey
	Interval      time.Duration
	Peers         PeerDiscovery
	Buffer        *AccountsBuffer
	GetSender     func(peerKey string, peer *Peer) twamplight.Sender
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
	var wg sync.WaitGroup
	for peerKey, peer := range peers {
		wg.Add(1)
		go func(peerKey string, peer *Peer) {
			defer wg.Done()

			if !sleepOrDone(ctx, time.Millisecond) {
				p.log.Debug("==> Probe loop cancelled during iteration")
				return
			}

			ts := time.Now().UTC()

			accountKey := AccountKey{
				OriginDevicePK: p.cfg.LocalDevicePK,
				TargetDevicePK: peer.DevicePK,
				LinkPK:         peer.LinkPK,
				Epoch:          DeriveEpoch(ts),
			}

			sender := p.cfg.GetSender(peerKey, peer)
			if sender == nil {
				p.log.Debug("==> Failed to create sender, recording loss", "peer", peerKey)
				p.cfg.Buffer.Add(accountKey, Sample{
					Timestamp: ts,
					RTT:       0,
					Loss:      true,
				})
				return
			}

			rtt, err := sender.Probe(ctx)
			if err != nil {
				p.log.Debug("==> Probe failed, recording loss", "peer", peerKey, "error", err)
				p.cfg.Buffer.Add(accountKey, Sample{
					Timestamp: ts,
					RTT:       0,
					Loss:      true,
				})
				return
			}

			p.cfg.Buffer.Add(accountKey, Sample{
				Timestamp: ts,
				RTT:       rtt,
				Loss:      false,
			})
		}(peerKey, peer)
	}
	wg.Wait()
}
