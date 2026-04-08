//go:build linux

package bgpstatus

import (
	"context"
	"errors"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/state"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// run starts the background worker goroutine, then drives the tick loop,
// running an immediate first tick before waiting for the ticker.
func (s *Submitter) run(ctx context.Context) error {
	go s.worker(ctx)

	ticker := s.cfg.Clock.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
			s.tick(ctx)
		}
	}
}

// tick collects BGP socket state, fetches activated users for this device,
// maps each user to their tunnel peer IP, determines Up/Down status (with
// grace period), and enqueues submission tasks for users whose status needs
// updating.
func (s *Submitter) tick(ctx context.Context) {
	rawSockets, err := state.GetBGPSocketStatsInNamespace(ctx, s.cfg.BGPNamespace)
	if err != nil {
		s.log.Error("bgpstatus: failed to collect BGP sockets", "error", err)
		return
	}
	sockets := make([]bgpSocket, len(rawSockets))
	for i, rs := range rawSockets {
		sockets[i] = bgpSocket{RemoteIP: rs.RemoteIP, State: rs.State}
	}
	establishedIPs := buildEstablishedIPSet(sockets)

	programData, err := s.cfg.ServiceabilityClient.GetProgramData(ctx)
	if err != nil {
		s.log.Error("bgpstatus: failed to fetch program data", "error", err)
		return
	}

	interfaces, err := s.cfg.LocalNet.Interfaces()
	if err != nil {
		s.log.Error("bgpstatus: failed to get local interfaces", "error", err)
		return
	}

	now := s.cfg.Clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range programData.Users {
		if user.Status != serviceability.UserStatusActivated {
			continue
		}
		if solana.PublicKeyFromBytes(user.DevicePubKey[:]) != s.cfg.LocalDevicePK {
			continue
		}

		userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()
		us := s.userStateFor(userPK)

		// Resolve the BGP peer IP for this user's /31 tunnel net.
		tunnelNet := tunnelNetToIPNet(user.TunnelNet)
		tunnel, err := netutil.FindLocalTunnel(interfaces, tunnelNet)
		if err != nil {
			if !errors.Is(err, netutil.ErrLocalTunnelNotFound) {
				s.log.Warn("bgpstatus: unexpected error finding tunnel", "user", userPK, "error", err)
			}
			// Tunnel not up — user cannot be Up.
			s.log.Debug("bgpstatus: tunnel not found for user", "user", userPK)
			continue
		}

		_, observedUp := establishedIPs[tunnel.TargetIP.String()]
		if observedUp {
			us.lastUpObservedAt = now
		}

		effectiveStatus := computeEffectiveStatus(observedUp, us, now, s.cfg.DownGracePeriod)

		if !shouldSubmit(us, effectiveStatus, now, s.cfg.PeriodicRefreshInterval) {
			continue
		}

		// Skip if a submission for this user is already in-flight.
		if s.pending[userPK] {
			s.log.Debug("bgpstatus: submission already in-flight, skipping", "user", userPK)
			continue
		}

		task := submitTask{user: user, status: effectiveStatus}
		select {
		case s.taskCh <- task:
			s.pending[userPK] = true
		default:
			s.log.Warn("bgpstatus: task channel full, dropping update", "user", userPK)
		}
	}
}

// worker drains the task channel and submits each update onchain with retry.
// It updates the per-user tracking state on success and always clears the
// pending flag so the next tick can re-evaluate.
func (s *Submitter) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.taskCh:
			userPK := solana.PublicKeyFromBytes(task.user.PubKey[:]).String()

			sig, err := s.submitWithRetry(ctx, task)

			s.mu.Lock()
			delete(s.pending, userPK)
			if err == nil {
				us := s.userStateFor(userPK)
				us.lastOnchainStatus = task.status
				us.lastWriteTime = s.cfg.Clock.Now()
				s.log.Info("bgpstatus: submitted BGP status",
					"user", userPK, "status", task.status, "sig", sig)
			} else {
				s.log.Error("bgpstatus: failed to submit after retries",
					"user", userPK, "status", task.status, "error", err)
			}
			s.mu.Unlock()
		}
	}
}

// submitWithRetry attempts the onchain write up to submitMaxRetries times with
// exponential backoff.  It returns early if the context is cancelled.
func (s *Submitter) submitWithRetry(ctx context.Context, task submitTask) (solana.Signature, error) {
	update := serviceability.UserBGPStatusUpdate{
		UserPubkey:   solana.PublicKeyFromBytes(task.user.PubKey[:]),
		DevicePubkey: s.cfg.LocalDevicePK,
		Status:       task.status,
	}

	var lastErr error
	for attempt := range submitMaxRetries {
		sig, err := s.cfg.Executor.SetUserBGPStatus(ctx, update)
		if err == nil {
			return sig, nil
		}
		lastErr = err
		delay := submitBaseBackoff * time.Duration(1<<attempt)
		s.log.Warn("bgpstatus: submission attempt failed",
			"user", update.UserPubkey, "attempt", attempt+1, "delay", delay, "error", err)
		select {
		case <-ctx.Done():
			return solana.Signature{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	return solana.Signature{}, lastErr
}
