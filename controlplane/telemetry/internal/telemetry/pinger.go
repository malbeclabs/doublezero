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
	LocalDevicePK   solana.PublicKey
	Interval        time.Duration
	ProbeTimeout    time.Duration
	Peers           PeerDiscovery
	Buffer          *AccountsBuffer
	GetSender       func(ctx context.Context, peer *Peer) twamplight.Sender
	GetCurrentEpoch func(ctx context.Context) (uint64, error)
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
	epoch, err := p.cfg.GetCurrentEpoch(ctx)
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

			accountKey := AccountKey{
				OriginDevicePK: p.cfg.LocalDevicePK,
				TargetDevicePK: peer.DevicePK,
				LinkPK:         peer.LinkPK,
				Epoch:          epoch,
			}

			ts := time.Now().UTC()

			if peer.Tunnel == nil {
				p.log.Debug("Tunnel not found, recording loss", "device", peer.DevicePK.String(), "link", peer.LinkPK.String())
				p.cfg.Buffer.Add(accountKey, Sample{
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
				p.cfg.Buffer.Add(accountKey, Sample{
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
		}(peer)
	}
	wg.Wait()
}
