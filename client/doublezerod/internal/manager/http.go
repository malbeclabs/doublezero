package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

// MulticastGroups contains the group codes a user publishes to and subscribes to.
type MulticastGroups struct {
	Publisher  []string `json:"publisher"`
	Subscriber []string `json:"subscriber"`
}

// Subscription describes one multicast group the user participates in, with the
// group's onchain details and the user's role(s) in it. A user that is both
// publisher and subscriber of a group appears once with both booleans set.
type Subscription struct {
	Pubkey       string `json:"pubkey"`
	Code         string `json:"code"`
	MulticastIp  string `json:"multicast_ip"`
	MaxBandwidth uint64 `json:"max_bandwidth"`
	Publisher    bool   `json:"publisher"`
	Subscriber   bool   `json:"subscriber"`
}

// V2ServiceStatus wraps a StatusResponse with enriched fields.
type V2ServiceStatus struct {
	*api.StatusResponse
	CurrentDevice               string          `json:"current_device"`
	CurrentDeviceRttNanoseconds int64           `json:"current_device_rtt_nanoseconds,omitempty"`
	CurrentDeviceLossPercentage float64         `json:"current_device_loss_percentage,omitempty"`
	LowestLatencyDevice         string          `json:"lowest_latency_device"`
	Metro                       string          `json:"metro"`
	Tenant                      string          `json:"tenant"`
	MulticastGroups             MulticastGroups `json:"multicast_groups"`
	Subscriptions               []Subscription  `json:"subscriptions"`
}

// V2StatusResponse is the response for the /v2/status endpoint.
type V2StatusResponse struct {
	ReconcilerEnabled bool              `json:"reconciler_enabled"`
	ClientIP          string            `json:"client_ip"`
	Network           string            `json:"network"`
	Services          []V2ServiceStatus `json:"services"`
}

/*
ServeProvision handles local provisioning of a double zero tunnel. The following is an example payload:

	`{
		"user_type": "IBRL"							[required]
		"tunnel_src": "1.1.1.1", 					[optional]
		"tunnel_dst": "2.2.2.2", 					[required]
		"tunnel_net": "10.1.1.0/31",				[required]
		"doublezero_ip": "10.0.0.0",				[required]
		"doublezero_prefixes": ["10.0.0.0/24"], 	[required]
		"bgp_local_asn": 65000,						[optional]
		"bgp_remote_asn": 65001						[optional]
	}`,
*/
func (n *NetlinkManager) ServeProvision(w http.ResponseWriter, r *http.Request) {
	var p api.ProvisionRequest
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed provision request: %v"}`, err)))
		return
	}

	if err = p.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "invalid request: %v"}`, err)))
		return
	}

	err = n.Provision(p)
	if err != nil {
		slog.Error("error during tunnel provisioning", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed stuff: %v"}`, err)))
		return
	}

	n.updateConnectionInfoMetric()
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (n *NetlinkManager) ServeRemove(w http.ResponseWriter, r *http.Request) {
	rr := &api.RemoveRequest{}
	err := json.NewDecoder(r.Body).Decode(rr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "malformed provision request: %v"}`, err)))
		return
	}

	// TODO: this is a hack until the client is updated to send user type
	if rr.UserType == api.UserTypeUnknown {
		rr.UserType = api.UserTypeIBRL
	}
	if err = rr.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "invalid request: %v"}`, err)))
		return
	}

	err = n.Remove(rr.UserType)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error during tunnel removal: %v"}`, err)))
		return
	}

	n.updateConnectionInfoMetric()
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (n *NetlinkManager) ServeStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status, err := n.Status()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while getting status: %v"}`, err)))
		return
	}
	if len(status) == 0 {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"doublezero_status": {"session_status": "disconnected"}}]`))
		return
	}
	if err = json.NewEncoder(w).Encode(status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status": "error", "description": "error while encoding status: %v"}`, err)))
		return
	}
}

// EnableRequest is the optional body of POST /enable.
type EnableRequest struct {
	// ClientIP pins the address the reconciler matches onchain users against and uses as the
	// IBRL tunnel source, overriding the one discovered at startup. An empty value leaves the
	// pin in effect alone — both the address in use and the one persisted — which is what a
	// body-less request (every pre-pin client, and every `connect` without the flag) does.
	ClientIP string `json:"client_ip,omitempty"`
}

// ServeEnable handles POST /enable requests.
func (n *NetlinkManager) ServeEnable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req EnableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("malformed enable request: %v", err)}) //nolint:errcheck
		return
	}

	// Parsed before anything is written, so a bad address leaves the daemon untouched.
	var clientIP net.IP
	if req.ClientIP != "" {
		parsed := net.ParseIP(req.ClientIP)
		if parsed == nil || parsed.To4() == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("invalid client_ip %q: not an IPv4 address", req.ClientIP)}) //nolint:errcheck
			return
		}
		clientIP = parsed.To4()

		// A pin the next restart would overrule is refused rather than accepted and quietly
		// reverted. The -client-ip flag outranks a pin at startup (it is the operator's standing
		// configuration for this host, in the unit file), but SetReconcilerState adopts a pin
		// immediately, so without this check the two disagree: the pin works, is persisted and is
		// reported by /v2/status, until a restart puts the flag's address back and the host
		// matches no onchain user. Naming the flag is the whole value of the message — the
		// operator has to edit the unit file, and nothing else would say so.
		if n.flagClientIP != nil && !n.flagClientIP.Equal(clientIP) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("client_ip %s conflicts with the daemon's -client-ip %s, which takes precedence at startup; remove -client-ip from the doublezerod unit to pin a different address", clientIP, n.flagClientIP)}) //nolint:errcheck
			return
		}

		// The daemon is the last gate before local configuration, so it repeats both of the
		// CLI's checks rather than trusting them. `create_user` is not the backstop for either:
		// it rejects the onchain *user*, while what is written here is the daemon's *pin*, and a
		// pin the program would never accept a user for is the worst shape to hold — it matches
		// no Activated user, tears down the services the host had, persists, and survives every
		// restart, with no un-pin path short of editing the state file.
		if !IsPublicIPv4(clientIP) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("client_ip %s is not a globally routable address", clientIP)}) //nolint:errcheck
			return
		}

		// For plain IBRL this address becomes the GRE tunnel source verbatim, so one the kernel
		// does not hold cannot carry a tunnel. An enumeration failure is fatal here — unlike in
		// the CLI, there is nothing downstream left to catch it.
		assigned, err := isLocallyAssigned(clientIP)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("could not verify client_ip %s: %v", clientIP, err)}) //nolint:errcheck
			return
		}
		if !assigned {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": fmt.Sprintf("client_ip %s is not assigned to any interface that is up on this host", clientIP)}) //nolint:errcheck
			return
		}
	}

	// What gets persisted is the pin that will be in effect once this request is served: the
	// new one, or the existing one when none was supplied. A body-less enable must leave it
	// alone rather than blank it — this request does not touch the address the daemon is
	// using, and a state file that disagreed with the running daemon would keep the tunnel up
	// until the next restart and then tear it down, discovery having quietly taken over.
	pinned := n.PinnedClientIP()
	if clientIP != nil {
		pinned = clientIP.String()
	}
	if err := WriteState(n.stateDir, State{ReconcilerEnabled: true, ClientIP: pinned}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	n.SetReconcilerState(true, clientIP)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// ServeDisable handles POST /disable requests.
func (n *NetlinkManager) ServeDisable(w http.ResponseWriter, _ *http.Request) {
	// The pin outlives a disable, because it describes which address this host presents to
	// DoubleZero rather than anything about one session. Dropping it here would only drop it
	// from disk — the daemon has no discovered address to fall back to without probing for
	// one again — and that split is what makes a later restart surprising.
	if err := WriteState(n.stateDir, State{ClientIP: n.PinnedClientIP()}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	n.SetEnabled(false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// ServeV2Status handles GET /v2/status requests.
func (n *NetlinkManager) ServeV2Status(w http.ResponseWriter, _ *http.Request) {
	statuses, err := n.Status()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}

	enriched := n.enrichStatuses(statuses)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(V2StatusResponse{ //nolint:errcheck
		ReconcilerEnabled: n.enabled.Load(),
		ClientIP:          n.ClientIP().String(),
		Network:           n.network,
		Services:          enriched,
	})
}

// updateConnectionInfoMetric resets and repopulates the doublezero_connection_info,
// doublezero_connection_rtt_nanoseconds, and doublezero_connection_loss_percentage
// gauges with current service metadata.
func (n *NetlinkManager) updateConnectionInfoMetric() {
	metricConnectionInfo.Reset()
	metricConnectionRttNanoseconds.Reset()
	metricConnectionLossPercentage.Reset()

	statuses, err := n.Status()
	if err != nil || len(statuses) == 0 {
		return
	}

	enriched := n.enrichStatuses(statuses)
	for _, svc := range enriched {
		metricConnectionInfo.WithLabelValues(
			svc.UserType.String(),
			n.network,
			svc.CurrentDevice,
			svc.Metro,
			svc.TunnelName,
			ipString(svc.TunnelSrc),
			ipString(svc.TunnelDst),
		).Set(1)
		if svc.CurrentDeviceRttNanoseconds > 0 {
			metricConnectionRttNanoseconds.WithLabelValues(
				svc.UserType.String(),
				n.network,
				svc.CurrentDevice,
				svc.Metro,
			).Set(float64(svc.CurrentDeviceRttNanoseconds))
			metricConnectionLossPercentage.WithLabelValues(
				svc.UserType.String(),
				n.network,
				svc.CurrentDevice,
				svc.Metro,
			).Set(svc.CurrentDeviceLossPercentage)
		}
	}
}

// ipString returns the string representation of an IP, or empty string if nil.
func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

// latencyToleranceNS matches the CLI's LATENCY_TOLERANCE_NS (5ms).
const latencyToleranceNS int64 = 5_000_000

// enrichStatuses adds onchain + latency context to each service status.
func (n *NetlinkManager) enrichStatuses(statuses []*api.StatusResponse) []V2ServiceStatus {
	// Fetch onchain data (best effort — empty strings on failure).
	var data *serviceability.ProgramData
	if n.fetcher != nil {
		ctx, cancel := context.WithTimeout(context.Background(), n.fetchTimeout)
		defer cancel()
		var err error
		data, err = n.fetcher.GetProgramData(ctx)
		if err != nil {
			slog.Warn("v2/status: failed to fetch program data for enrichment", "error", err)
		}
	}

	// Fetch latency results (best effort).
	var latencyResults []latency.LatencyResult
	if n.latencyProvider != nil {
		latencyResults = n.latencyProvider.GetResultsCache()
	}

	// Build lookup maps from program data.
	var (
		devicesByPK     map[[32]byte]serviceability.Device
		exchangesByPK   map[[32]byte]serviceability.Exchange
		tenantsByPK     map[[32]byte]serviceability.Tenant
		mcastGroupsByPK map[[32]byte]serviceability.MulticastGroup
		users           []serviceability.User
	)
	if data != nil {
		devicesByPK = make(map[[32]byte]serviceability.Device, len(data.Devices))
		for _, d := range data.Devices {
			devicesByPK[d.PubKey] = d
		}
		exchangesByPK = make(map[[32]byte]serviceability.Exchange, len(data.Exchanges))
		for _, e := range data.Exchanges {
			exchangesByPK[e.PubKey] = e
		}
		tenantsByPK = make(map[[32]byte]serviceability.Tenant, len(data.Tenants))
		for _, t := range data.Tenants {
			tenantsByPK[t.PubKey] = t
		}
		mcastGroupsByPK = make(map[[32]byte]serviceability.MulticastGroup, len(data.MulticastGroups))
		for _, mg := range data.MulticastGroups {
			mcastGroupsByPK[mg.PubKey] = mg
		}
		users = data.Users
	}

	// Build device IP lookup for tunnel_dst fallback matching.
	deviceByPublicIP := make(map[[4]byte]*serviceability.Device)
	if data != nil {
		for i := range data.Devices {
			d := &data.Devices[i]
			deviceByPublicIP[d.PublicIp] = d
		}
	}

	enriched := make([]V2ServiceStatus, 0, len(statuses))
	for _, svc := range statuses {
		es := V2ServiceStatus{
			StatusResponse: svc,
			MulticastGroups: MulticastGroups{
				Publisher:  []string{},
				Subscriber: []string{},
			},
			Subscriptions: []Subscription{},
		}

		if data == nil {
			enriched = append(enriched, es)
			continue
		}

		// Match service to onchain user by dz_ip + user_type.
		var matchedDevice *serviceability.Device
		var matchedUser *serviceability.User
		dzIP := svc.DoubleZeroIP.To4()
		if dzIP != nil && !dzIP.IsUnspecified() {
			for i := range users {
				u := &users[i]
				if net.IP(u.DzIp[:]).Equal(dzIP) && mapUserType(u.UserType) == svc.UserType {
					matchedUser = u
					devPK := [32]byte(u.DevicePubKey)
					if d, ok := devicesByPK[devPK]; ok {
						matchedDevice = &d
					}
					break
				}
			}
		}

		// Fallback: match by tunnel_dst to device public_ip.
		if matchedDevice == nil && svc.TunnelDst != nil {
			tunnelDst := svc.TunnelDst.To4()
			if tunnelDst != nil {
				var key [4]byte
				copy(key[:], tunnelDst)
				if d, ok := deviceByPublicIP[key]; ok {
					matchedDevice = d
				}
			}
		}

		// Fallback: match by client_ip + user_type (e.g. multicast subscribers
		// whose tunnel endpoint differs from the device public IP).
		if matchedUser == nil {
			clientIP4 := n.ClientIP().To4()
			for i := range users {
				u := &users[i]
				if net.IP(u.ClientIp[:]).Equal(clientIP4) && mapUserType(u.UserType) == svc.UserType {
					matchedUser = u
					devPK := [32]byte(u.DevicePubKey)
					if d, ok := devicesByPK[devPK]; ok {
						matchedDevice = &d
					}
					break
				}
			}
		}

		if matchedDevice != nil {
			es.CurrentDevice = matchedDevice.Code
			exchPK := [32]byte(matchedDevice.ExchangePubKey)
			if exch, ok := exchangesByPK[exchPK]; ok {
				es.Metro = exch.Name
			}
			for _, r := range latencyResults {
				if r.Device.PubKey == matchedDevice.PubKey && r.Reachable {
					es.CurrentDeviceRttNanoseconds = r.Avg
					es.CurrentDeviceLossPercentage = r.Loss
					break
				}
			}
		}

		if matchedUser != nil {
			tenantPK := [32]byte(matchedUser.TenantPubKey)
			if tenantPK != [32]byte{} {
				if t, ok := tenantsByPK[tenantPK]; ok {
					es.Tenant = t.Code
				}
			}
			// Build the structured subscriptions list (one entry per group,
			// deduplicated) alongside the flat code lists. Iterate publishers
			// first, then subscribers, to keep a publishers-first ordering.
			subIdxByPK := make(map[[32]byte]int)
			addRole := func(pk [32]byte, pub, sub bool) {
				if idx, ok := subIdxByPK[pk]; ok {
					es.Subscriptions[idx].Publisher = es.Subscriptions[idx].Publisher || pub
					es.Subscriptions[idx].Subscriber = es.Subscriptions[idx].Subscriber || sub
					return
				}
				mg, ok := mcastGroupsByPK[pk]
				if !ok {
					return
				}
				subIdxByPK[pk] = len(es.Subscriptions)
				es.Subscriptions = append(es.Subscriptions, Subscription{
					Pubkey:       base58.Encode(mg.PubKey[:]),
					Code:         mg.Code,
					MulticastIp:  net.IP(mg.MulticastIp[:]).String(),
					MaxBandwidth: mg.MaxBandwidth,
					Publisher:    pub,
					Subscriber:   sub,
				})
			}
			for _, pk := range matchedUser.Publishers {
				if mg, ok := mcastGroupsByPK[pk]; ok {
					es.MulticastGroups.Publisher = append(es.MulticastGroups.Publisher, mg.Code)
				}
				addRole(pk, true, false)
			}
			for _, pk := range matchedUser.Subscribers {
				if mg, ok := mcastGroupsByPK[pk]; ok {
					es.MulticastGroups.Subscriber = append(es.MulticastGroups.Subscriber, mg.Code)
				}
				addRole(pk, false, true)
			}
		}

		// Compute lowest latency device.
		es.LowestLatencyDevice = computeLowestLatencyDevice(
			latencyResults, devicesByPK, matchedDevice,
		)

		enriched = append(enriched, es)
	}

	return enriched
}

// computeLowestLatencyDevice finds the best device by latency, preferring the
// current device within tolerance. Returns the device code or empty string.
func computeLowestLatencyDevice(
	latencyResults []latency.LatencyResult,
	devicesByPK map[[32]byte]serviceability.Device,
	currentDevice *serviceability.Device,
) string {
	if len(latencyResults) == 0 || len(devicesByPK) == 0 {
		return ""
	}

	// Filter to reachable results with activated devices.
	type candidate struct {
		result latency.LatencyResult
		device serviceability.Device
	}
	var candidates []candidate
	for _, r := range latencyResults {
		if !r.Reachable {
			continue
		}
		d, ok := devicesByPK[r.Device.PubKey]
		if !ok || d.Status != serviceability.DeviceStatusActivated {
			continue
		}
		candidates = append(candidates, candidate{result: r, device: d})
	}

	if len(candidates) == 0 {
		return ""
	}

	// If there's a current device in the candidates, start with it as best.
	var bestIdx int
	var bestAvg int64 = math.MaxInt64

	if currentDevice != nil {
		for i, c := range candidates {
			if c.device.PubKey == currentDevice.PubKey {
				bestIdx = i
				bestAvg = c.result.Avg
				break
			}
		}
	}

	// If we didn't find the current device, start with no candidate selected.
	if bestAvg == math.MaxInt64 {
		// Find the lowest avg latency overall.
		for i, c := range candidates {
			if c.result.Avg < bestAvg {
				bestAvg = c.result.Avg
				bestIdx = i
			}
		}
		return candidates[bestIdx].device.Code
	}

	// Current device found — switch only if another device has the lowest
	// latency overall AND beats the current device by more than the tolerance.
	var lowestIdx int
	var lowestAvg int64 = math.MaxInt64
	for i, c := range candidates {
		if c.result.Avg < lowestAvg {
			lowestAvg = c.result.Avg
			lowestIdx = i
		}
	}
	if bestAvg-lowestAvg > latencyToleranceNS {
		bestIdx = lowestIdx
	}

	return candidates[bestIdx].device.Code
}
