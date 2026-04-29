//go:build linux

package bgpstatus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
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

// DefaultCollector returns a NamespaceCollector that collects BGP socket stats
// via netlink and local interfaces via Linux namespace switching.
func DefaultCollector(localNet netutil.LocalNet) NamespaceCollector {
	return func(ctx context.Context, namespace string) (map[string]struct{}, []netutil.Interface, error) {
		rawSockets, err := state.GetBGPSocketStatsInNamespace(ctx, namespace)
		if err != nil {
			return nil, nil, fmt.Errorf("bgp sockets in %s: %w", namespace, err)
		}
		socks := make([]bgpSocket, len(rawSockets))
		for i, rs := range rawSockets {
			socks[i] = bgpSocket{RemoteIP: rs.RemoteIP, State: rs.State}
		}
		established := buildEstablishedIPSet(socks)

		ifaces, err := netns.RunInNamespace(namespace, func() ([]netutil.Interface, error) {
			return localNet.Interfaces()
		})
		if err != nil {
			return nil, nil, fmt.Errorf("interfaces in %s: %w", namespace, err)
		}
		return established, ifaces, nil
	}
}

// tick collects BGP socket state, fetches activated users for this device,
// maps each user to their tunnel peer IP, determines Up/Down status (with
// grace period), and enqueues submission tasks for users whose status needs
// updating.
func (s *Submitter) tick(ctx context.Context) {
	programData, err := s.cfg.ServiceabilityClient.GetProgramData(ctx)
	if err != nil {
		s.log.Error("bgpstatus: failed to fetch program data", "error", err)
		return
	}

	// Pre-collect activated users for this device. This is needed both to
	// derive the full namespace set (multicast users require the root namespace) and to
	// drive the per-user status loop below.
	var deviceUsers []serviceability.User
	for _, u := range programData.Users {
		if u.Status == serviceability.UserStatusActivated &&
			solana.PublicKeyFromBytes(u.DevicePubKey[:]) == s.cfg.LocalDevicePK {
			deviceUsers = append(deviceUsers, u)
		}
	}

	// User tunnel interfaces live in a per-tenant VRF namespace (e.g. ns-vrf1,
	// ns-vrf2). Multicast users are an exception: their tunnels live in the
	// global VRF (root network namespace). Collect state from all relevant namespaces so that
	// all user types are handled correctly.
	// Tunnel IPs are globally unique (onchain-allocated), so merging is safe.
	namespaces := vrfNamespaces(s.cfg.BGPNamespace, programData.Tenants, deviceUsers)

	establishedIPs := make(map[string]struct{})
	var interfaces []netutil.Interface
	successCount := 0

	for _, ns := range namespaces {
		established, ifaces, err := s.cfg.Collector(ctx, ns)
		if err != nil {
			s.log.Warn("bgpstatus: failed to collect namespace state", "namespace", ns, "error", err)
			continue
		}
		for ip := range established {
			establishedIPs[ip] = struct{}{}
		}
		interfaces = append(interfaces, ifaces...)
		successCount++
	}

	if successCount == 0 {
		s.log.Error("bgpstatus: failed to collect state from all namespaces", "namespaces", namespaces)
		return
	}

	now := s.cfg.Clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	activeUserKeys := make(map[string]struct{})

	for _, user := range deviceUsers {
		userPK := solana.PublicKeyFromBytes(user.PubKey[:]).String()
		activeUserKeys[userPK] = struct{}{}

		// Seed lastOnchainStatus from the ledger on first observation (e.g. after
		// a daemon restart) so a disappeared tunnel correctly transitions to Down
		// rather than being skipped because Unknown != Up.
		us := s.userStateFor(userPK, serviceability.BGPStatus(user.BgpStatus))

		// Resolve the BGP peer IP for this user's /31 tunnel net.
		tunnelNet := tunnelNetToIPNet(user.TunnelNet)
		var observedUp bool
		tunnel, err := netutil.FindLocalTunnel(interfaces, tunnelNet)
		if err != nil {
			if !errors.Is(err, netutil.ErrLocalTunnelNotFound) {
				s.log.Warn("bgpstatus: unexpected error finding tunnel", "user", userPK, "error", err)
				continue
			}
			s.log.Debug("bgpstatus: tunnel not found for user", "user", userPK)
			// Without a tunnel, the BGP session cannot be established.
			// If the last known onchain status was already Down (or never written),
			// there is nothing to update — skip this user.
			if us.lastOnchainStatus != serviceability.BGPStatusUp {
				continue
			}
			// The tunnel is gone but the last known onchain status is Up.
			// Fall through with observedUp=false so we submit Down.
		} else {
			_, observedUp = establishedIPs[tunnel.TargetIP.String()]
			if observedUp {
				us.lastUpObservedAt = now
			}
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

	// Prune userState entries for users no longer activated on this device to
	// prevent unbounded memory growth as users come and go.
	// Also clear pending flags so a reactivated user is not permanently blocked.
	for pk := range s.userState {
		if _, active := activeUserKeys[pk]; !active {
			delete(s.userState, pk)
			delete(s.pending, pk)
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

			statusLabel := task.status.String()
			s.mu.Lock()
			delete(s.pending, userPK)
			if err == nil {
				metricSubmissionsTotal.WithLabelValues(statusLabel, "success").Inc()
				us := s.userStateFor(userPK, serviceability.BGPStatusUnknown)
				us.lastOnchainStatus = task.status
				us.lastWriteTime = s.cfg.Clock.Now()
				s.log.Info("bgpstatus: submitted BGP status",
					"user", userPK, "status", task.status, "sig", sig)
			} else {
				metricSubmissionsTotal.WithLabelValues(statusLabel, "error").Inc()
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
		start := time.Now()
		sig, err := s.cfg.Executor.SetUserBGPStatus(ctx, update)
		if err == nil {
			metricSubmissionDuration.Observe(time.Since(start).Seconds())
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
