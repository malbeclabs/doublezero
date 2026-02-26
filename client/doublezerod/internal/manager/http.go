package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// V2ServiceStatus wraps a StatusResponse with enriched fields.
type V2ServiceStatus struct {
	*api.StatusResponse
	CurrentDevice       string `json:"current_device"`
	LowestLatencyDevice string `json:"lowest_latency_device"`
	Metro               string `json:"metro"`
	Tenant              string `json:"tenant"`
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

// ServeEnable handles POST /enable requests.
func (n *NetlinkManager) ServeEnable(w http.ResponseWriter, _ *http.Request) {
	if err := WriteState(n.stateDir, true); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	n.SetEnabled(true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// ServeDisable handles POST /disable requests.
func (n *NetlinkManager) ServeDisable(w http.ResponseWriter, _ *http.Request) {
	if err := WriteState(n.stateDir, false); err != nil {
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
		ClientIP:          n.clientIP.String(),
		Network:           n.network,
		Services:          enriched,
	})
}

// latencyToleranceNS matches the CLI's LATENCY_TOLERANCE_NS (5ms).
const latencyToleranceNS int64 = 5_000_000

// enrichStatuses adds onchain + latency context to each service status.
func (n *NetlinkManager) enrichStatuses(statuses []*api.StatusResponse) []V2ServiceStatus {
	// Fetch onchain data (best effort — empty strings on failure).
	var data *serviceability.ProgramData
	if n.fetcher != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
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
		devicesByPK   map[[32]byte]serviceability.Device
		exchangesByPK map[[32]byte]serviceability.Exchange
		tenantsByPK   map[[32]byte]serviceability.Tenant
		users         []serviceability.User
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
		es := V2ServiceStatus{StatusResponse: svc}

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

		// Fallback: match by tunnel_dst to device public_ip (for multicast subscribers without dz_ip).
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

		if matchedDevice != nil {
			es.CurrentDevice = matchedDevice.Code
			exchPK := [32]byte(matchedDevice.ExchangePubKey)
			if exch, ok := exchangesByPK[exchPK]; ok {
				es.Metro = exch.Name
			}
		}

		if matchedUser != nil {
			tenantPK := [32]byte(matchedUser.TenantPubKey)
			if tenantPK != [32]byte{} {
				if t, ok := tenantsByPK[tenantPK]; ok {
					es.Tenant = t.Code
				}
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

	// Current device found — check if any other device is significantly better.
	for i, c := range candidates {
		if c.device.PubKey == currentDevice.PubKey {
			continue
		}
		if bestAvg-c.result.Avg > latencyToleranceNS {
			bestIdx = i
			break
		}
	}

	return candidates[bestIdx].device.Code
}
