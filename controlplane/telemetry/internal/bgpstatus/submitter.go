package bgpstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// GNMIClient is a minimal interface satisfied by gpb.GNMIClient (the generated
// gNMI gRPC client). Using the interface keeps GNMICollector testable without a
// live gRPC server.
type GNMIClient interface {
	Get(ctx context.Context, req *gpb.GetRequest, opts ...grpc.CallOption) (*gpb.GetResponse, error)
}

// bgpNeighborsGetRequest fetches all BGP neighbor state across all network instances.
var bgpNeighborsGetRequest = &gpb.GetRequest{
	Path: []*gpb.Path{
		{
			Elem: []*gpb.PathElem{
				{Name: "network-instances"},
				{Name: "network-instance", Key: map[string]string{"name": "*"}},
				{Name: "protocols"},
				{Name: "protocol", Key: map[string]string{"identifier": "BGP", "name": "BGP"}},
				{Name: "bgp"},
				{Name: "neighbors"},
				{Name: "neighbor", Key: map[string]string{"neighbor-address": "*"}},
				{Name: "state"},
			},
		},
	},
	Type:     gpb.GetRequest_STATE,
	Encoding: gpb.Encoding_JSON_IETF,
}

// GNMICollector returns a BGPCollector that reads BGP neighbor session state via
// a gNMI Get to the Arista device's local gNMI server. It reports all neighbors
// whose session-state is ESTABLISHED across every network instance.
func GNMICollector(client GNMIClient) BGPCollector {
	return func(ctx context.Context) (map[string]struct{}, error) {
		resp, err := client.Get(ctx, bgpNeighborsGetRequest)
		if err != nil {
			return nil, fmt.Errorf("gNMI Get BGP neighbors: %w", err)
		}
		return parseEstablished(resp), nil
	}
}

// bgpStateJSON extracts session-state from a gNMI JSON IETF update value.
type bgpStateJSON struct {
	SessionState string `json:"openconfig-network-instance:session-state"`
}

// parseEstablished returns the set of neighbor-address strings whose
// session-state is ESTABLISHED in the gNMI GetResponse.
func parseEstablished(resp *gpb.GetResponse) map[string]struct{} {
	established := make(map[string]struct{})
	for _, notif := range resp.GetNotification() {
		prefix := notif.GetPrefix()
		for _, update := range notif.GetUpdate() {
			addr := neighborAddress(prefix, update.GetPath())
			if addr == "" {
				continue
			}
			jsonVal := update.GetVal().GetJsonIetfVal()
			if len(jsonVal) == 0 {
				continue
			}
			var state bgpStateJSON
			if err := json.Unmarshal(jsonVal, &state); err != nil {
				continue
			}
			if state.SessionState == "ESTABLISHED" {
				established[addr] = struct{}{}
			}
		}
	}
	return established
}

// neighborAddress extracts the neighbor-address key from the gNMI path elements,
// checking both the notification prefix and the update path.
func neighborAddress(prefix, path *gpb.Path) string {
	for _, elem := range prefix.GetElem() {
		if elem.GetName() == "neighbor" {
			return elem.GetKey()["neighbor-address"]
		}
	}
	for _, elem := range path.GetElem() {
		if elem.GetName() == "neighbor" {
			return elem.GetKey()["neighbor-address"]
		}
	}
	return ""
}

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

// tick fetches activated users for this device, calls the BGPCollector once to
// get all ESTABLISHED sessions, maps each user's /31 tunnel net to their peer
// IP, and enqueues submission tasks for users whose status needs updating.
func (s *Submitter) tick(ctx context.Context) {
	programData, err := s.cfg.ServiceabilityClient.GetProgramData(ctx)
	if err != nil {
		s.log.Error("bgpstatus: failed to fetch program data", "error", err)
		return
	}

	var deviceUsers []serviceability.User
	for _, u := range programData.Users {
		if u.Status == serviceability.UserStatusActivated &&
			solana.PublicKeyFromBytes(u.DevicePubKey[:]) == s.cfg.LocalDevicePK {
			deviceUsers = append(deviceUsers, u)
		}
	}

	establishedIPs, err := s.cfg.Collector(ctx)
	if err != nil {
		s.log.Error("bgpstatus: failed to collect BGP state", "error", err)
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
		// a daemon restart) so a disappeared session correctly transitions to Down
		// rather than being skipped because Unknown != Up.
		us := s.userStateFor(userPK, serviceability.BGPStatus(user.BgpStatus))

		// A /31 tunnel net has two host IPs: the device-side and the peer-side.
		// We check both because we don't know which end the device holds; only
		// one can appear as a BGP neighbor on this device.
		tunnelNet := tunnelNetToIPNet(user.TunnelNet)
		ip0, ip1 := peerIPsFor31(tunnelNet)
		_, up0 := establishedIPs[ip0.String()]
		_, up1 := establishedIPs[ip1.String()]
		observedUp := up0 || up1

		if observedUp {
			us.lastUpObservedAt = now
		}

		effectiveStatus := computeEffectiveStatus(observedUp, us, now, s.cfg.DownGracePeriod)

		if !shouldSubmit(us, effectiveStatus, now, s.cfg.PeriodicRefreshInterval) {
			continue
		}

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
// exponential backoff. It returns early if the context is cancelled.
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
